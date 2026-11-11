package argocd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

// Actions on an Argo CD Application, each one merge patch on the object, as `argocd --core`
// does: the application controller picks the change up. Every patch carries the
// resourceVersion read just before, so it fails instead of overwriting a change made in
// between (a sync the controller started, a parent rewriting the spec).

const (
	argoActionRefresh     = "refresh"
	argoActionHardRefresh = "hardRefresh"
	ActionSync            = "sync"
	ActionTerminate       = "terminate"
	argoActionAutoSyncOn  = "autoSyncOn"
	argoActionAutoSyncOff = "autoSyncOff"
	argoActionRollback    = "rollback"

	// argoPausedAnnotation keeps the prune/selfHeal settings of an auto-sync Ichor paused,
	// so resuming restores them.
	argoPausedAnnotation = "ichor.levis.name/paused-automated"
	argoInitiatorName    = "ichor"
)

var argoActions = []string{argoActionRefresh, argoActionHardRefresh, ActionSync, ActionTerminate, argoActionAutoSyncOn, argoActionAutoSyncOff, argoActionRollback}

// SyncOptions are the choices of the sync sheet (and the rollback target).
type SyncOptions struct {
	Prune              bool              `json:"prune"`
	DryRun             bool              `json:"dryRun"`
	Force              bool              `json:"force"`
	ApplyOutOfSyncOnly bool              `json:"applyOutOfSyncOnly"`
	ServerSideApply    bool              `json:"serverSideApply"`
	Replace            bool              `json:"replace"`
	Resources          []argoResourceRef `json:"resources"` // empty: every resource
	HistoryID          int64             `json:"historyId"` // rollback only
}

type argoResourceRef struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

var (
	errArgoBusy     = &kube.APIError{Code: 409, Reason: "Busy", Message: "a sync is already running: wait for it or terminate it"}
	errArgoNotBusy  = &kube.APIError{Code: 409, Reason: "Idle", Message: "no sync is running"}
	errArgoAutoSync = &kube.APIError{Code: 409, Reason: "AutoSync", Message: "pause auto-sync first, or Argo CD syncs straight back to the latest revision"}
	errArgoHistory  = &kube.APIError{Code: 404, Reason: "NotFound", Message: "that deployment is no longer in the app's history"}
)

// IsAction tells an action Act runs.
func IsAction(action string) bool { return slices.Contains(argoActions, action) }

// ParseSyncOptions reads the optionsJSON of an action; "" is none.
func ParseSyncOptions(optionsJSON string) (SyncOptions, error) {
	var opts SyncOptions
	if strings.TrimSpace(optionsJSON) != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
			return opts, fmt.Errorf("bad sync options: %w", err)
		}
	}

	return opts, nil
}

func Act(ctx context.Context, k kube.Client, namespace, name, action string, opts SyncOptions, now time.Time) error {
	groups, err := kube.APIGroups(ctx, k)
	if err != nil {
		return err
	}

	version, ok := groups[Group]
	if !ok {
		return &kube.APIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	path := "/apis/" + Group + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/applications/" + url.PathEscape(name)

	var app Object
	if err := k.Get(ctx, path, &app); err != nil {
		return err
	}

	base := "/apis/" + Group + "/" + version

	var owner *Owner
	if action == argoActionAutoSyncOn || action == argoActionAutoSyncOff || action == argoActionRollback {
		if owner, err = argoActionOwner(ctx, k, base, app); err != nil {
			return err
		}
	}

	patch, err := argoPatch(app, owner, action, opts, now)
	if err != nil {
		return err
	}

	meta, _ := patch["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		patch["metadata"] = meta
	}

	meta["resourceVersion"] = app.Metadata.ResourceVersion

	return k.Patch(ctx, path, "application/merge-patch+json", patch, nil)
}

// argoPatch is the merge patch for action on app, or why it is refused.
// owner is who writes the app's spec (nil: nobody); spec changes are refused when set.
func argoPatch(app Object, owner *Owner, action string, opts SyncOptions, now time.Time) (map[string]any, error) {
	switch action {
	case argoActionRefresh, argoActionHardRefresh:
		mode := "normal"
		if action == argoActionHardRefresh {
			mode = "hard"
		}

		return map[string]any{"metadata": map[string]any{"annotations": map[string]any{argoRefreshAnnotation: mode}}}, nil
	case ActionTerminate:
		if !argoRunning(app) {
			return nil, errArgoNotBusy
		}

		return map[string]any{"status": map[string]any{"operationState": map[string]any{"phase": "Terminating"}}}, nil
	case ActionSync:
		if argoBusy(app) {
			return nil, errArgoBusy
		}

		return map[string]any{"operation": argoSyncOperation(app, opts, nil, now)}, nil
	case argoActionRollback:
		return argoRollbackPatch(app, owner, opts, now)
	default:
		return argoAutoSyncPatch(app, owner, action == argoActionAutoSyncOn)
	}
}

func argoRunning(app Object) bool {
	op := app.Status.OperationState

	return op != nil && (op.Phase == "Running" || op.Phase == "Terminating")
}

// argoBusy: an operation runs, or one was requested and not picked up yet.
func argoBusy(app Object) bool {
	return argoRunning(app) || (len(app.Operation) > 0 && string(app.Operation) != "null")
}

func argoOwnedError(owner *Owner) error {
	if owner == nil {
		return nil
	}

	return &kube.APIError{Code: 409, Reason: "Owned", Message: fmt.Sprintf("managed by %s %s: change it in Git, it would be reverted", owner.Kind, owner.Name)}
}

// argoActionOwner finds who writes app's spec, like the listing does: a parent named by its
// tracking only counts when that Application exists.
func argoActionOwner(ctx context.Context, k kube.Client, base string, app Object) (*Owner, error) {
	ref := app.Metadata.Annotations[TrackingID]
	if ref != "" {
		ref, _, _ = strings.Cut(ref, ":")
	} else {
		ref = app.Metadata.Labels[InstanceLabel]
	}

	existing := map[string]bool{}

	if ref != "" {
		for _, key := range AppKeys(ref, app.Metadata.Namespace) {
			ns, name, _ := strings.Cut(key, "/")

			var parent struct{}

			switch err := k.Get(ctx, base+"/namespaces/"+url.PathEscape(ns)+"/applications/"+url.PathEscape(name), &parent); {
			case err == nil:
				existing[key] = true
			case !kube.IsNotFound(err):
				return nil, err
			}
		}
	}

	return argoOwnerOf(app, existing), nil
}

func argoAutoSyncPatch(app Object, owner *Owner, on bool) (map[string]any, error) {
	if err := argoOwnedError(owner); err != nil {
		return nil, err
	}

	if !on {
		auto := map[string]bool{}
		if p := app.Spec.SyncPolicy; p != nil && p.Automated != nil {
			auto["prune"], auto["selfHeal"] = p.Automated.Prune, p.Automated.SelfHeal
		}

		saved, _ := json.Marshal(auto)

		return map[string]any{
			"metadata": map[string]any{"annotations": map[string]any{argoPausedAnnotation: string(saved)}},
			"spec":     map[string]any{"syncPolicy": map[string]any{"automated": nil}},
		}, nil
	}

	auto := map[string]any{"prune": false, "selfHeal": false}

	var saved map[string]bool
	if json.Unmarshal([]byte(app.Metadata.Annotations[argoPausedAnnotation]), &saved) == nil {
		auto["prune"], auto["selfHeal"] = saved["prune"], saved["selfHeal"]
	}

	return map[string]any{
		"metadata": map[string]any{"annotations": map[string]any{argoPausedAnnotation: nil}},
		"spec":     map[string]any{"syncPolicy": map[string]any{"automated": auto}},
	}, nil
}

func argoRollbackPatch(app Object, owner *Owner, opts SyncOptions, now time.Time) (map[string]any, error) {
	if err := argoOwnedError(owner); err != nil {
		return nil, err
	}

	if p := app.Spec.SyncPolicy; p != nil && p.Automated != nil && (p.Automated.Enabled == nil || *p.Automated.Enabled) {
		return nil, errArgoAutoSync
	}

	if argoBusy(app) {
		return nil, errArgoBusy
	}

	idx := slices.IndexFunc(app.Status.History, func(h argoHistoryObject) bool { return h.ID == opts.HistoryID })
	if idx < 0 {
		return nil, errArgoHistory
	}

	h := app.Status.History[idx]
	opts.Resources = nil

	return map[string]any{"operation": argoSyncOperation(app, opts, &h, now)}, nil
}

// argoSyncOperation is the operation the controller runs: the app's target revision(s), or a
// history entry's for a rollback, with the app's own sync options plus the chosen ones.
func argoSyncOperation(app Object, opts SyncOptions, to *argoHistoryObject, now time.Time) map[string]any {
	sync := map[string]any{
		"prune":        opts.Prune,
		"dryRun":       opts.DryRun,
		"syncStrategy": map[string]any{"hook": map[string]any{"force": opts.Force}},
	}

	sources := SourcesOf(app.Spec)

	switch {
	case to != nil && len(to.Revisions) > 0:
		sync["revisions"] = to.Revisions
		if len(to.Sources) > 0 {
			sync["sources"] = to.Sources
		}
	case to != nil:
		sync["revision"] = to.Revision
		if len(to.Source) > 0 {
			sync["source"] = to.Source
		}
	case len(app.Spec.Sources) > 0:
		revisions := make([]string, 0, len(sources))
		for _, s := range sources {
			revisions = append(revisions, s.TargetRevision)
		}

		sync["revisions"] = revisions
	case len(sources) == 1:
		sync["revision"] = sources[0].TargetRevision
	}

	var options []string
	if p := app.Spec.SyncPolicy; p != nil {
		options = append(options, p.SyncOptions...)
	}

	for _, o := range []struct {
		on     bool
		option string
	}{
		{opts.ApplyOutOfSyncOnly, "ApplyOutOfSyncOnly=true"},
		{opts.ServerSideApply, "ServerSideApply=true"},
		{opts.Replace, "Replace=true"},
	} {
		if o.on && !slices.Contains(options, o.option) {
			options = append(options, o.option)
		}
	}

	if len(options) > 0 {
		sync["syncOptions"] = options
	}

	if len(opts.Resources) > 0 {
		sync["resources"] = opts.Resources
	}

	return map[string]any{
		"sync":        sync,
		"initiatedBy": map[string]any{"username": argoInitiatorName},
		"info":        []map[string]string{{"name": "Reason", "value": "Started from Ichor at " + now.UTC().Format(time.RFC3339)}},
	}
}
