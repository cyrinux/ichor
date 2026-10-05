package ichorgo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
)

// AppProject sync windows, evaluated the way the application controller does: a window is
// active from each run of its cron schedule for its duration; an active deny window stops
// automated syncs (self-heal included) of the apps it matches, and manual ones unless
// manualSync. Ichor's freezes are one-shot deny windows (see kube_argocd_freeze.go) recorded
// in the argoFreezesAnnotation. See plans/roadmap/devops/09-argocd-freeze.md.

// argoFreezesAnnotation lists the project's windows Ichor created: [{window, reason,
// createdAt, expiresAt}], window being the argoWindowID of the window.
const argoFreezesAnnotation = "ichor.levis.name/freezes"

type argoWindow struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"` // allow|deny
	Schedule     string   `json:"schedule"`
	Duration     string   `json:"duration"`
	TimeZone     string   `json:"timeZone"`
	Applications []string `json:"applications"`
	Namespaces   []string `json:"namespaces"`
	Clusters     []string `json:"clusters"`
	ManualSync   bool     `json:"manualSync"`
	Active       bool     `json:"active"`
	// Start and End are the current occurrence when active, else the next one; 0 when none
	// or the window cannot be read (Error says why).
	Start int64           `json:"start"`
	End   int64           `json:"end"`
	Error string          `json:"error"`
	Apps  int             `json:"apps"`  // apps of the project it matches
	Ichor *argoFreezeInfo `json:"ichor"` // set on a freeze Ichor created
}

type argoFreezeInfo struct {
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"createdAt"`
	ExpiresAt int64  `json:"expiresAt"`
	Expired   bool   `json:"expired"`
}

// argoFreeze is an app's active deny windows summed up.
type argoFreeze struct {
	Project    string   `json:"project"`
	Until      int64    `json:"until"`      // the latest end among them
	ManualSync bool     `json:"manualSync"` // every one of them allows manual syncs
	ByIchor    bool     `json:"byIchor"`    // every one of them is an Ichor freeze
	Windows    []string `json:"windows"`
}

type argoWindowObject struct {
	Kind         string   `json:"kind"`
	Schedule     string   `json:"schedule"`
	Duration     string   `json:"duration"`
	TimeZone     string   `json:"timeZone,omitempty"`
	Applications []string `json:"applications,omitempty"`
	Namespaces   []string `json:"namespaces,omitempty"`
	Clusters     []string `json:"clusters,omitempty"`
	ManualSync   bool     `json:"manualSync,omitempty"`
	AndOperator  bool     `json:"andOperator,omitempty"`
}

type argoFreezeRecord struct {
	Window    string `json:"window"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
}

// argoWindowEntry is one window of a project as read, with its raw JSON (kept verbatim on
// writes: newer fields such as description survive) and its Ichor record.
type argoWindowEntry struct {
	raw     json.RawMessage
	window  argoWindowObject
	id      string
	record  *argoFreezeRecord
	expired bool
}

// argoWindowID identifies a window by its whole content, fields Ichor does not know
// included (windows have no name): its JSON with the keys sorted.
func argoWindowID(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) == nil {
		raw, _ = json.Marshal(v)
	}

	sum := sha256.Sum256(raw)

	return hex.EncodeToString(sum[:6])
}

// argoWindowEntries reads the project's windows; err when the Ichor annotation is unreadable
// (the windows then all look foreign).
func argoWindowEntries(p argoProjectObject, now time.Time) (_ []argoWindowEntry, err error) {
	var records []argoFreezeRecord
	if a := p.Metadata.Annotations[argoFreezesAnnotation]; a != "" {
		if json.Unmarshal([]byte(a), &records) != nil {
			err = errArgoFreezesAnnotation
		}
	}

	out := make([]argoWindowEntry, 0, len(p.Spec.SyncWindows))

	for _, raw := range p.Spec.SyncWindows {
		e := argoWindowEntry{raw: raw}
		_ = json.Unmarshal(raw, &e.window)
		e.id = argoWindowID(raw)

		if i := slices.IndexFunc(records, func(r argoFreezeRecord) bool { return r.Window == e.id }); i >= 0 {
			r := records[i]
			e.record = &r
			e.expired = !now.Before(parseTime(r.ExpiresAt))
		}

		out = append(out, e)
	}

	return out, err
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)

	return t
}

// argoWindowTimes is the current occurrence of w when it is active, else the next one (zero
// when it never runs again), or why the window cannot be read.
func argoWindowTimes(w argoWindowObject, now time.Time) (active bool, start, end time.Time, err error) {
	sched, err := parseCronSchedule(w.Schedule)
	if err != nil {
		return false, start, end, err
	}

	d, err := time.ParseDuration(w.Duration)
	if err != nil {
		return false, start, end, err
	}

	loc := time.UTC
	if w.TimeZone != "" {
		if loc, err = time.LoadLocation(w.TimeZone); err != nil {
			return false, start, end, err
		}
	}

	t := now.In(loc)

	// The first run after now-d: before now it is the current occurrence, else the next one.
	start = sched.next(t.Add(-d))
	if start.IsZero() {
		return false, start, start, nil
	}

	return start.Before(t), start, start.Add(d), nil
}

// argoWindowMatches: an app is in a window when one of its selectors matches (all of the
// non-empty ones with andOperator), as the controller's SyncWindows.Matches.
func argoWindowMatches(w argoWindowObject, a argoApp) bool {
	var clusters []string // Argo CD skips an empty server or name, not an empty namespace
	for _, c := range []string{a.Destination.Server, a.Destination.Name} {
		if c != "" {
			clusters = append(clusters, c)
		}
	}

	checks := []struct {
		patterns []string
		values   []string
	}{
		{w.Applications, []string{a.Name}},
		{w.Namespaces, []string{a.Destination.Namespace}},
		{w.Clusters, clusters},
	}

	matched, used := 0, 0

	for _, c := range checks {
		if len(c.patterns) == 0 {
			continue
		}

		used++

		if slices.ContainsFunc(c.patterns, func(p string) bool {
			return slices.ContainsFunc(c.values, func(v string) bool { return argoGlobMatch(p, v) })
		}) {
			matched++
		}
	}

	if w.AndOperator {
		return used > 0 && matched == used
	}

	return matched > 0
}

// argoGlobMatch matches Argo CD's globs (gobwas/glob without separators): * and ? anywhere,
// [abc] [a-z] [!a] classes, {a,b} alternatives, \ escapes; anything else is literal.
func argoGlobMatch(pattern, s string) bool {
	if pattern == s || pattern == "*" {
		return true
	}

	re, err := regexp.Compile("^(?:" + argoGlobRegexp(pattern) + ")$")

	return err == nil && re.MatchString(s)
}

func argoGlobRegexp(pattern string) string {
	var (
		b      strings.Builder
		braces int
	)

	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; {
		case r == '\\' && i+1 < len(runes):
			i++
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		case r == '*':
			b.WriteString(".*")
		case r == '?':
			b.WriteString(".")
		case r == '{':
			braces++
			b.WriteString("(?:")
		case r == '}' && braces > 0:
			braces--
			b.WriteString(")")
		case r == ',' && braces > 0:
			b.WriteString("|")
		case r == '[':
			end := slices.Index(runes[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}

			class := string(runes[i+1 : i+1+end])
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}

			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}

	return b.String()
}

// mapArgoProjects maps the projects with their windows, and marks the apps an active deny
// window freezes.
func mapArgoProjects(projects []argoProjectObject, apps []argoApp, now time.Time) []argoProject {
	out := make([]argoProject, 0, len(projects))

	for _, p := range projects {
		project := argoProject{
			Namespace: p.Metadata.Namespace, Name: p.Metadata.Name, Description: p.Spec.Description,
			SyncWindows: len(p.Spec.SyncWindows), Windows: []argoWindow{},
		}

		if managing := argoProjectManager(p, apps); managing != nil {
			project.ManagedBy = managing.Name
			project.ManagedServerSide = slices.Contains(managing.SyncOptions, "ServerSideApply=true")
		}

		entries, _ := argoWindowEntries(p, now)
		for _, e := range entries {
			w := mapArgoWindow(e, now)

			for i := range apps {
				a := &apps[i]
				if a.Project != p.Metadata.Name || !argoWindowMatches(e.window, *a) {
					continue
				}

				w.Apps++

				if w.Active && w.Kind == "deny" {
					a.Freeze = addArgoFreeze(a.Freeze, p.Metadata.Name, w)
				}
			}

			project.Windows = append(project.Windows, w)
		}

		out = append(out, project)
	}

	slices.SortFunc(out, func(a, b argoProject) int { return strings.Compare(a.Name, b.Name) })

	return out
}

func mapArgoWindow(e argoWindowEntry, now time.Time) argoWindow {
	w := argoWindow{
		ID: e.id, Kind: e.window.Kind, Schedule: e.window.Schedule, Duration: e.window.Duration, TimeZone: e.window.TimeZone,
		Applications: orEmpty(e.window.Applications), Namespaces: orEmpty(e.window.Namespaces), Clusters: orEmpty(e.window.Clusters),
		ManualSync: e.window.ManualSync,
	}

	active, start, end, err := argoWindowTimes(e.window, now)
	if err != nil {
		w.Error = err.Error()
	} else {
		w.Active = active
		if !start.IsZero() {
			w.Start, w.End = start.UnixMilli(), end.UnixMilli()
		}
	}

	if r := e.record; r != nil {
		w.Ichor = &argoFreezeInfo{
			Reason: r.Reason, CreatedAt: unixMilli(r.CreatedAt), ExpiresAt: unixMilli(r.ExpiresAt), Expired: e.expired,
		}
	}

	return w
}

func addArgoFreeze(f *argoFreeze, project string, w argoWindow) *argoFreeze {
	if f == nil {
		f = &argoFreeze{Project: project, ManualSync: true, ByIchor: true, Windows: []string{}}
	}

	f.Until = max(f.Until, w.End)
	f.ManualSync = f.ManualSync && w.ManualSync
	f.ByIchor = f.ByIchor && w.Ichor != nil
	f.Windows = append(f.Windows, w.ID)

	return f
}

// argoProjectManager is the Argo CD app that applies the project from Git (its tracking
// annotation or instance label), nil when none is listed.
func argoProjectManager(p argoProjectObject, apps []argoApp) *argoApp {
	ref := p.Metadata.Annotations[argoTrackingID]
	if ref != "" {
		ref, _, _ = strings.Cut(ref, ":")
	} else {
		ref = p.Metadata.Labels[argoInstanceLabel]
	}

	if ref == "" {
		return nil
	}

	for _, key := range argoAppKeys(ref, p.Metadata.Namespace) {
		for i := range apps {
			if apps[i].Namespace+"/"+apps[i].Name == key {
				return &apps[i]
			}
		}
	}

	return nil
}
