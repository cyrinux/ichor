package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Actions on an Argo CD Application, each one merge patch on the object, as `argocd --core`
// does: the application controller picks the change up. Every patch carries the
// resourceVersion read just before, so it fails instead of overwriting a change made in
// between (a sync the controller started, a parent rewriting the spec).

const (
	argoActionRefresh     = "refresh"
	argoActionHardRefresh = "hardRefresh"
	argoActionSync        = "sync"
	argoActionTerminate   = "terminate"
	argoActionAutoSyncOn  = "autoSyncOn"
	argoActionAutoSyncOff = "autoSyncOff"
	argoActionRollback    = "rollback"

	// argoPausedAnnotation keeps the prune/selfHeal settings of an auto-sync Ichor paused,
	// so resuming restores them.
	argoPausedAnnotation = "ichor.levis.name/paused-automated"
	argoInitiatorName    = "ichor"
)

var argoActions = []string{argoActionRefresh, argoActionHardRefresh, argoActionSync, argoActionTerminate, argoActionAutoSyncOn, argoActionAutoSyncOff, argoActionRollback}

// argoSyncOptions are the choices of the sync sheet (and the rollback target).
type argoSyncOptions struct {
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
	errArgoBusy     = &kubeAPIError{Code: 409, Reason: "Busy", Message: "a sync is already running: wait for it or terminate it"}
	errArgoNotBusy  = &kubeAPIError{Code: 409, Reason: "Idle", Message: "no sync is running"}
	errArgoAutoSync = &kubeAPIError{Code: 409, Reason: "AutoSync", Message: "pause auto-sync first, or Argo CD syncs straight back to the latest revision"}
	errArgoHistory  = &kubeAPIError{Code: 404, Reason: "NotFound", Message: "that deployment is no longer in the app's history"}
)

// KubeArgoAction runs an action on the Argo CD Application namespace/name (os:admin):
// refresh, hardRefresh, sync, terminate, autoSyncOn, autoSyncOff or rollback. optionsJSON
// holds the sync options (prune, dryRun, force, applyOutOfSyncOnly, serverSideApply, replace,
// resources) and, for a rollback, historyId; "" for none. Spec changes (auto-sync, rollback)
// are refused on an app an ApplicationSet or a parent app owns: the owner would revert them.
// kubeServer: see KubePods.
func KubeArgoAction(configYAML, contextName, kubeServer, namespace, name, action, optionsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "argo-" + action, Namespace: namespace, Object: "Application/" + name, Params: strings.TrimSpace(optionsJSON)})

	if !slices.Contains(argoActions, action) {
		return fmt.Errorf("unsupported Argo CD action %q", action)
	}

	if err := validateKubeName("application", namespace, name); err != nil {
		return err
	}

	var opts argoSyncOptions
	if strings.TrimSpace(optionsJSON) != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
			return fmt.Errorf("bad sync options: %w", err)
		}
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return argoAction(ctx, k, namespace, name, action, opts, time.Now())
	})
}

func argoAction(ctx context.Context, k *kubeClient, namespace, name, action string, opts argoSyncOptions, now time.Time) error {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	path := "/apis/" + groupArgo + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/applications/" + url.PathEscape(name)

	var app argoObject
	if err := k.get(ctx, path, &app); err != nil {
		return err
	}

	base := "/apis/" + groupArgo + "/" + version

	var owner *argoOwner
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

	return k.patch(ctx, path, "application/merge-patch+json", patch, nil)
}

// argoPatch is the merge patch for action on app, or why it is refused.
// owner is who writes the app's spec (nil: nobody); spec changes are refused when set.
func argoPatch(app argoObject, owner *argoOwner, action string, opts argoSyncOptions, now time.Time) (map[string]any, error) {
	switch action {
	case argoActionRefresh, argoActionHardRefresh:
		mode := "normal"
		if action == argoActionHardRefresh {
			mode = "hard"
		}

		return map[string]any{"metadata": map[string]any{"annotations": map[string]any{argoRefreshAnnotation: mode}}}, nil
	case argoActionTerminate:
		if !argoRunning(app) {
			return nil, errArgoNotBusy
		}

		return map[string]any{"status": map[string]any{"operationState": map[string]any{"phase": "Terminating"}}}, nil
	case argoActionSync:
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

func argoRunning(app argoObject) bool {
	op := app.Status.OperationState

	return op != nil && (op.Phase == "Running" || op.Phase == "Terminating")
}

// argoBusy: an operation runs, or one was requested and not picked up yet.
func argoBusy(app argoObject) bool {
	return argoRunning(app) || (len(app.Operation) > 0 && string(app.Operation) != "null")
}

func argoOwnedError(owner *argoOwner) error {
	if owner == nil {
		return nil
	}

	return &kubeAPIError{Code: 409, Reason: "Owned", Message: fmt.Sprintf("managed by %s %s: change it in Git, it would be reverted", owner.Kind, owner.Name)}
}

// argoActionOwner finds who writes app's spec, like the listing does: a parent named by its
// tracking only counts when that Application exists.
func argoActionOwner(ctx context.Context, k *kubeClient, base string, app argoObject) (*argoOwner, error) {
	ref := app.Metadata.Annotations[argoTrackingID]
	if ref != "" {
		ref, _, _ = strings.Cut(ref, ":")
	} else {
		ref = app.Metadata.Labels[argoInstanceLabel]
	}

	existing := map[string]bool{}

	if ref != "" {
		for _, key := range argoAppKeys(ref, app.Metadata.Namespace) {
			ns, name, _ := strings.Cut(key, "/")

			var parent struct{}

			switch err := k.get(ctx, base+"/namespaces/"+url.PathEscape(ns)+"/applications/"+url.PathEscape(name), &parent); {
			case err == nil:
				existing[key] = true
			case !isNotFound(err):
				return nil, err
			}
		}
	}

	return argoOwnerOf(app, existing), nil
}

func argoAutoSyncPatch(app argoObject, owner *argoOwner, on bool) (map[string]any, error) {
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

func argoRollbackPatch(app argoObject, owner *argoOwner, opts argoSyncOptions, now time.Time) (map[string]any, error) {
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
func argoSyncOperation(app argoObject, opts argoSyncOptions, to *argoHistoryObject, now time.Time) map[string]any {
	sync := map[string]any{
		"prune":        opts.Prune,
		"dryRun":       opts.DryRun,
		"syncStrategy": map[string]any{"hook": map[string]any{"force": opts.Force}},
	}

	sources := argoSourcesOf(app.Spec)

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
