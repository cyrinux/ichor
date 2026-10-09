package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Freezes: one-shot deny sync windows on an AppProject, so a hotfix made by hand is not
// reverted by auto-sync or self-heal. A window on the project, unlike pausing auto-sync on the
// Application, also holds for apps an ApplicationSet generates, and Argo CD ends it on its own.
// Argo CD has no one-shot window: the schedule is the start minute (in UTC), which only comes
// back a year later; Ichor drops its expired windows on its next change to the project.

const (
	argoFreezeActionFreeze       = "freeze"
	argoFreezeActionExtend       = "extend"
	argoFreezeActionUnfreeze     = "unfreeze"
	argoFreezeActionClearExpired = "clearExpired"

	argoFreezeMinMinutes = 5
	argoFreezeMaxMinutes = 7 * 24 * 60
	argoFreezeMaxScope   = 20
	argoFreezeMaxReason  = 200
)

var argoFreezeActions = []string{argoFreezeActionFreeze, argoFreezeActionExtend, argoFreezeActionUnfreeze, argoFreezeActionClearExpired}

// argoFreezeOptions are the choices of the freeze sheet. Exactly one of Applications and
// Namespaces is set; ["*"] in Applications freezes the whole project. Window is the
// argoWindow.id to extend or unfreeze; FromGit confirms removing a window Ichor did not create.
type argoFreezeOptions struct {
	Applications []string `json:"applications"`
	Namespaces   []string `json:"namespaces"`
	Minutes      int      `json:"minutes"`
	ManualSync   bool     `json:"manualSync"`
	Reason       string   `json:"reason"`
	Window       string   `json:"window"`
	FromGit      bool     `json:"fromGit"`
}

var (
	errArgoWindowGone = &kubeAPIError{Code: 404, Reason: "NotFound", Message: "that sync window is no longer there: it ended or was removed"}
	errArgoFromGit    = &kubeAPIError{Code: 409, Reason: "FromGit", Message: "this sync window was not created by Ichor: remove it in Git too, or Argo CD may put it back"}
	errArgoNotIchor   = &kubeAPIError{Code: 409, Reason: "FromGit", Message: "only freezes created by Ichor can be extended"}
	errArgoSameFreeze = &kubeAPIError{Code: 409, Reason: "Duplicate", Message: "an identical freeze already exists on this project"}
	// errArgoFreezesAnnotation: writing would lose the record of Ichor's freezes.
	errArgoFreezesAnnotation = &kubeAPIError{Code: 409, Reason: "Unreadable", Message: "the project's " + argoFreezesAnnotation + " annotation is unreadable: fix or remove it first"}
)

// KubeArgoFreeze changes the sync windows of the Argo CD AppProject namespace/project
// (os:admin): freeze (a deny window from now for minutes, on applications or namespaces),
// extend (an Ichor freeze by minutes), unfreeze (remove a window; one Ichor did not create
// needs fromGit) or clearExpired (drop Ichor's ended freezes, which every change also does).
// optionsJSON holds argoFreezeOptions. kubeServer: see KubePods.
func KubeArgoFreeze(configYAML, contextName, kubeServer, namespace, project, action, optionsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, project = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(project))

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "argo-" + action, Namespace: namespace, Object: "AppProject/" + project})

	if !slices.Contains(argoFreezeActions, action) {
		return fmt.Errorf("unsupported Argo CD freeze action %q", action)
	}

	if err := validateKubeName("project", namespace, project); err != nil {
		return err
	}

	var opts argoFreezeOptions
	if strings.TrimSpace(optionsJSON) != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
			return fmt.Errorf("bad freeze options: %w", err)
		}
	}

	for i, s := range opts.Applications {
		opts.Applications[i] = privacy.reveal(strings.TrimSpace(s))
	}

	for i, s := range opts.Namespaces {
		opts.Namespaces[i] = privacy.revealNamespace(strings.TrimSpace(s))
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return argoFreezeAction(ctx, k, namespace, project, action, opts, time.Now())
	})
}

func argoFreezeAction(ctx context.Context, k *kubeClient, namespace, project, action string, opts argoFreezeOptions, now time.Time) error {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	base := "/apis/" + groupArgo + "/" + version
	path := base + "/namespaces/" + url.PathEscape(namespace) + "/appprojects/" + url.PathEscape(project)

	resp, data, err := k.send(ctx, http.MethodGet, path, "application/json", "", nil, nil)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return kubeStatusError(resp.StatusCode, data)
	}

	var p argoProjectObject
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("decode Kubernetes API answer: %w", err)
	}

	// The window is timed by the controller's clock: take the API server's, not the phone's.
	if date, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		now = date
	}

	if action == argoFreezeActionFreeze {
		var apps kubeList[argoObject]
		if err := getList(ctx, k, base+"/applications", &apps); err != nil {
			return err
		}

		if err := argoFreezeScopeMatches(opts, project, apps.Items); err != nil {
			return err
		}
	}

	patch, err := argoFreezePatch(p, action, opts, now)
	if err != nil || patch == nil {
		return err
	}

	return k.patch(ctx, path, "application/merge-patch+json", patch, nil)
}

// argoFreezeScopeMatches refuses a freeze naming an app or a namespace no app of the project
// has: Argo CD would accept a window that freezes nothing (a typo, a name still masked).
func argoFreezeScopeMatches(opts argoFreezeOptions, project string, apps []argoObject) error {
	check := func(what string, names []string, of func(argoObject) string) error {
		for _, name := range names {
			if name == "*" {
				continue
			}

			if !slices.ContainsFunc(apps, func(a argoObject) bool { return a.Spec.Project == project && of(a) == name }) {
				return &kubeAPIError{Code: 404, Reason: "NotFound", Message: fmt.Sprintf("no app of project %s %s %s", project, what, name)}
			}
		}

		return nil
	}

	if err := check("is named", opts.Applications, func(a argoObject) string { return a.Metadata.Name }); err != nil {
		return err
	}

	return check("deploys to namespace", opts.Namespaces, func(a argoObject) string { return a.Spec.Destination.Namespace })
}

// argoFreezePatch is the merge patch for action on project p (nil when nothing changes), or
// why it is refused. It rewrites the whole window list (a merge patch replaces lists), keeping
// the other windows verbatim, and carries the resourceVersion read so a concurrent change
// makes it fail instead of being lost.
func argoFreezePatch(p argoProjectObject, action string, opts argoFreezeOptions, now time.Time) (map[string]any, error) {
	entries, err := argoWindowEntries(p, now)
	if err != nil {
		return nil, err
	}

	target := -1
	if action == argoFreezeActionExtend || action == argoFreezeActionUnfreeze {
		target = slices.IndexFunc(entries, func(e argoWindowEntry) bool { return e.id == opts.Window })
		if target < 0 {
			return nil, errArgoWindowGone
		}
	}

	switch action {
	case argoFreezeActionFreeze:
		e, err := newArgoFreeze(opts, now)
		if err != nil {
			return nil, err
		}

		if slices.ContainsFunc(entries, func(x argoWindowEntry) bool { return x.id == e.id }) {
			return nil, nil // the same freeze, asked twice within the minute
		}

		entries = append(entries, e)
	case argoFreezeActionExtend:
		e, err := extendArgoFreeze(entries[target], opts.Minutes, now)
		if err != nil {
			return nil, err
		}

		if slices.ContainsFunc(entries, func(x argoWindowEntry) bool { return x.id == e.id }) {
			return nil, errArgoSameFreeze
		}

		entries[target] = e
	case argoFreezeActionUnfreeze:
		if entries[target].record == nil && !opts.FromGit {
			return nil, errArgoFromGit
		}

		entries = slices.Delete(entries, target, target+1)
	}

	kept := slices.DeleteFunc(slices.Clone(entries), func(e argoWindowEntry) bool { return e.expired })
	if action == argoFreezeActionClearExpired && len(kept) == len(entries) {
		return nil, nil
	}

	return argoWindowsPatch(p, kept), nil
}

func argoWindowsPatch(p argoProjectObject, entries []argoWindowEntry) map[string]any {
	var (
		windows []json.RawMessage
		records []argoFreezeRecord
	)

	for _, e := range entries {
		windows = append(windows, e.raw)
		if e.record != nil && !slices.ContainsFunc(records, func(r argoFreezeRecord) bool { return r.Window == e.id }) {
			records = append(records, *e.record)
		}
	}

	var annotation any // nil removes it
	if len(records) > 0 {
		data, _ := json.Marshal(records)
		annotation = string(data)
	}

	var list any // nil removes the field
	if len(windows) > 0 {
		list = windows
	}

	return map[string]any{
		"metadata": map[string]any{
			"resourceVersion": p.Metadata.ResourceVersion,
			"annotations":     map[string]any{argoFreezesAnnotation: annotation},
		},
		"spec": map[string]any{"syncWindows": list},
	}
}

// newArgoFreeze is a deny window from the current minute for opts.Minutes (rounded up to
// the next minute so the freeze lasts at least that long).
func newArgoFreeze(opts argoFreezeOptions, now time.Time) (argoWindowEntry, error) {
	if err := validateArgoFreeze(opts); err != nil {
		return argoWindowEntry{}, err
	}

	start := now.UTC().Truncate(time.Minute)

	minutes := opts.Minutes
	if !start.Equal(now.UTC()) {
		minutes++
	}

	end := start.Add(time.Duration(minutes) * time.Minute)

	w := argoWindowObject{
		Kind:         "deny",
		Schedule:     fmt.Sprintf("%d %d %d %d *", start.Minute(), start.Hour(), start.Day(), int(start.Month())),
		Duration:     fmt.Sprintf("%dm", minutes),
		TimeZone:     "UTC",
		Applications: opts.Applications,
		Namespaces:   opts.Namespaces,
		ManualSync:   opts.ManualSync,
	}

	raw, _ := json.Marshal(w)
	id := argoWindowID(raw)

	return argoWindowEntry{raw: raw, window: w, id: id, record: &argoFreezeRecord{
		Window: id, Reason: strings.TrimSpace(opts.Reason),
		CreatedAt: now.UTC().Format(time.RFC3339), ExpiresAt: end.Format(time.RFC3339),
	}}, nil
}

func validateArgoFreeze(opts argoFreezeOptions) error {
	if (len(opts.Applications) == 0) == (len(opts.Namespaces) == 0) {
		return fmt.Errorf("freeze either applications or namespaces")
	}

	for _, s := range append(slices.Clone(opts.Applications), opts.Namespaces...) {
		if s != "*" && !kubeNamePattern.MatchString(s) {
			return fmt.Errorf("invalid Kubernetes name %q", s)
		}
	}

	if len(opts.Applications)+len(opts.Namespaces) > argoFreezeMaxScope {
		return fmt.Errorf("freeze at most %d applications or namespaces at once", argoFreezeMaxScope)
	}

	if err := validateArgoFreezeMinutes(opts.Minutes); err != nil {
		return err
	}

	if utf8.RuneCountInString(opts.Reason) > argoFreezeMaxReason {
		return fmt.Errorf("the reason is longer than %d characters", argoFreezeMaxReason)
	}

	return nil
}

func validateArgoFreezeMinutes(minutes int) error {
	if minutes < argoFreezeMinMinutes || minutes > argoFreezeMaxMinutes {
		return fmt.Errorf("a freeze lasts %d minutes to %d days", argoFreezeMinMinutes, argoFreezeMaxMinutes/(24*60))
	}

	return nil
}

// extendArgoFreeze lengthens an Ichor freeze still running by minutes, up to 7 days from now.
func extendArgoFreeze(e argoWindowEntry, minutes int, now time.Time) (argoWindowEntry, error) {
	switch {
	case e.record == nil:
		return e, errArgoNotIchor
	case e.expired:
		return e, errArgoWindowGone
	}

	if err := validateArgoFreezeMinutes(minutes); err != nil {
		return e, err
	}

	d, err := time.ParseDuration(e.window.Duration)
	if err != nil {
		return e, fmt.Errorf("unreadable freeze duration %q", e.window.Duration)
	}

	end := parseTime(e.record.ExpiresAt).Add(time.Duration(minutes) * time.Minute)
	if end.Sub(now) > argoFreezeMaxMinutes*time.Minute {
		return e, fmt.Errorf("a freeze ends at most %d days from now", argoFreezeMaxMinutes/(24*60))
	}

	var raw map[string]any
	if err := json.Unmarshal(e.raw, &raw); err != nil {
		return e, err
	}

	e.window.Duration = fmt.Sprintf("%dm", int(d.Minutes())+minutes)
	raw["duration"] = e.window.Duration
	e.raw, _ = json.Marshal(raw)
	e.id = argoWindowID(e.raw)

	record := *e.record
	record.Window, record.ExpiresAt = e.id, end.UTC().Format(time.RFC3339)
	e.record = &record

	return e, nil
}
