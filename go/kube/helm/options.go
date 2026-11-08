package helm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/kube"
)

// The options of a Helm rollback, as `helm rollback --force --history-max N`: force replaces
// each changed object with a PUT of the target rendering instead of a three-way merge patch
// (for an object a patch cannot get back), history max deletes the oldest revisions once
// the rollback is deployed (Helm's storage.removeLeastRecent). Kubernetes access: create,
// update and delete on secrets in the release namespace, update on each replaced kind.

// PrunePrefix starts the error of a rollback that is done but whose old revisions could
// not all be deleted.
const PrunePrefix = "history not pruned: "

// Options is the optionsJSON of a rollback and its plan.
type Options struct {
	Force bool `json:"force"`
	// HistoryMax is how many revisions to keep, the new one included; 0 keeps them all.
	HistoryMax int `json:"historyMax"`
}

// ParseRollbackOptions reads optionsJSON; "" is no option.
func ParseRollbackOptions(optionsJSON string) (Options, error) {
	var opts Options

	if strings.TrimSpace(optionsJSON) == "" {
		return opts, nil
	}

	if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
		return Options{}, fmt.Errorf("invalid rollback options: %w", err)
	}

	if opts.HistoryMax < 0 {
		return Options{}, fmt.Errorf("invalid history max %d (0 keeps every revision)", opts.HistoryMax)
	}

	return opts, nil
}

func (o Options) AuditParams(revision int) string {
	return fmt.Sprintf("revision=%d force=%t historyMax=%d", revision, o.Force, o.HistoryMax)
}

// pruneRefs are the revisions to delete after a rollback (refs newest first), oldest
// first: after it there are len(refs)+1 revisions, the historyMax newest stay. The new
// revision, the newest, is never one of them.
func pruneRefs(refs []SecretRef, historyMax int) []SecretRef {
	extra := len(refs) + 1 - historyMax
	if historyMax <= 0 || extra <= 0 {
		return nil
	}

	oldest := slices.Clone(refs)
	slices.Reverse(oldest)

	return oldest[:min(extra, len(oldest))]
}

func revisions(refs []SecretRef) []int {
	out := make([]int, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.Revision)
	}

	return out
}

// pruneHistory deletes the Secrets of refs (by their own name: a copy Helm did not name
// goes too). One already gone is fine; the others refused are named in the error.
func pruneHistory(ctx context.Context, k Env, refs []SecretRef) error {
	errs := []error{}

	for _, ref := range refs {
		path := kube.ScopedPath("/api/v1", ref.Namespace, "secrets") + "/" + url.PathEscape(ref.Secret)

		if err := k.Do(ctx, http.MethodDelete, path, "application/json", nil, nil); err != nil && !kube.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("revision %d: %w", ref.Revision, k.apiError(err)))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf(PrunePrefix+"%w", err)
	}

	return nil
}

// immutableFields are the spec fields the API server assigns and then refuses to see
// changed: a replace copies them from the live object when the target does not set them.
var immutableFields = map[string][]string{
	"Service":               {"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy", "healthCheckNodePort"},
	"PersistentVolumeClaim": {"volumeName", "storageClassName"},
}

// replaceBody is what a replace PUTs: the target rendering (ownership already stamped)
// with the live resourceVersion, so a concurrent change makes it fail instead of being
// overwritten, and the live immutable fields. obj.Body is left as it is.
func replaceBody(obj object, live map[string]any) map[string]any {
	body := maps.Clone(obj.Body)

	meta := cloneMap(body["metadata"])
	if rv, ok := mapField(live, "metadata")["resourceVersion"]; ok {
		meta["resourceVersion"] = rv
	}

	body["metadata"] = meta

	fields := immutableFields[obj.Kind]
	if len(fields) == 0 {
		return body
	}

	spec, liveSpec := cloneMap(body["spec"]), mapField(live, "spec")

	for _, f := range fields {
		if v, ok := liveSpec[f]; ok {
			if _, set := spec[f]; !set {
				spec[f] = v
			}
		}
	}

	body["spec"] = spec

	return body
}

// replaceObject PUTs obj's target rendering to path (with its query) built on a fresh
// read of the live object: the plan's read may be minutes old, and a controller or the
// status writer bumps the resourceVersion meanwhile. A conflict (written between that read
// and the PUT) is retried once on another fresh read.
func replaceObject(ctx context.Context, k Env, obj object, path string) error {
	var err error

	for range 2 {
		var live map[string]any
		if err = k.Get(ctx, obj.Path, &live); err != nil {
			return err
		}

		body, mErr := json.Marshal(replaceBody(obj, live))
		if mErr != nil {
			return mErr
		}

		err = k.Do(ctx, http.MethodPut, path, "application/json", body, nil)
		if kube.Code(err) != http.StatusConflict {
			return err
		}
	}

	return err
}

// cloneMap is a shallow copy of v when it is an object, an empty one otherwise.
func cloneMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}

	return maps.Clone(m)
}

// mapField is obj[key] when it is an object, nil otherwise.
func mapField(obj map[string]any, key string) map[string]any {
	m, _ := obj[key].(map[string]any)

	return m
}

// workloadObjects are the Deployments, StatefulSets and DaemonSets (apps/v1) among objs.
func workloadObjects(objs []object) []object {
	out := []object{}

	for _, o := range objs {
		if isWorkloadKind(o.Kind) && strings.HasPrefix(o.APIVersion, "apps/") {
			out = append(out, o)
		}
	}

	return out
}

// isWorkloadKind tells a kind the follow can wait for.
func isWorkloadKind(kind string) bool {
	return slices.ContainsFunc([]string{"Deployment", "StatefulSet", "DaemonSet"}, func(k string) bool {
		return strings.EqualFold(k, strings.TrimSpace(kind))
	})
}

// PruneRevisions are the revisions of history (newest first) a rollback keeping historyMax
// deletes, oldest first.
func PruneRevisions(history []Revision, historyMax int) []int {
	refs := make([]SecretRef, 0, len(history))
	for _, h := range history {
		refs = append(refs, SecretRef{Revision: h.Revision})
	}

	return revisions(pruneRefs(refs, historyMax))
}
