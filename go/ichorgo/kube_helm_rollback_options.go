package ichorgo

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
)

// The options of a Helm rollback, as `helm rollback --force --history-max N`: force replaces
// each changed object with a PUT of the target rendering instead of a three-way merge patch
// (for an object a patch cannot get back), history max deletes the oldest revisions once
// the rollback is deployed (Helm's storage.removeLeastRecent). Kubernetes access: create,
// update and delete on secrets in the release namespace, update on each replaced kind.

// helmPrunePrefix starts the error of a rollback that is done but whose old revisions could
// not all be deleted.
const helmPrunePrefix = "history not pruned: "

// helmRollbackOptions is the optionsJSON of KubeHelmRollbackPlan and KubeHelmRollback.
type helmRollbackOptions struct {
	Force bool `json:"force"`
	// HistoryMax is how many revisions to keep, the new one included; 0 keeps them all.
	HistoryMax int `json:"historyMax"`
}

// parseHelmRollbackOptions reads optionsJSON; "" is no option.
func parseHelmRollbackOptions(optionsJSON string) (helmRollbackOptions, error) {
	var opts helmRollbackOptions

	if strings.TrimSpace(optionsJSON) == "" {
		return opts, nil
	}

	if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
		return helmRollbackOptions{}, fmt.Errorf("invalid rollback options: %w", err)
	}

	if opts.HistoryMax < 0 {
		return helmRollbackOptions{}, fmt.Errorf("invalid history max %d (0 keeps every revision)", opts.HistoryMax)
	}

	return opts, nil
}

func (o helmRollbackOptions) auditParams(revision int) string {
	return fmt.Sprintf("revision=%d force=%t historyMax=%d", revision, o.Force, o.HistoryMax)
}

// helmPruneRefs are the revisions to delete after a rollback (refs newest first), oldest
// first: after it there are len(refs)+1 revisions, the historyMax newest stay. The new
// revision, the newest, is never one of them.
func helmPruneRefs(refs []helmSecretRef, historyMax int) []helmSecretRef {
	extra := len(refs) + 1 - historyMax
	if historyMax <= 0 || extra <= 0 {
		return nil
	}

	oldest := slices.Clone(refs)
	slices.Reverse(oldest)

	return oldest[:min(extra, len(oldest))]
}

func helmRevisions(refs []helmSecretRef) []int {
	out := make([]int, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.revision)
	}

	return out
}

// pruneHelmHistory deletes the Secrets of refs (by their own name: a copy Helm did not name
// goes too). One already gone is fine; the others refused are named in the error.
func pruneHelmHistory(ctx context.Context, k *kubeClient, refs []helmSecretRef) error {
	errs := []error{}

	for _, ref := range refs {
		path := scopedPath("/api/v1", ref.namespace, "secrets") + "/" + url.PathEscape(ref.secret)

		if err := k.do(ctx, http.MethodDelete, path, "application/json", nil, nil); err != nil && !isNotFound(err) {
			errs = append(errs, fmt.Errorf("revision %d: %w", ref.revision, kubeError(err)))
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf(helmPrunePrefix+"%w", err)
	}

	return nil
}

// helmImmutableFields are the spec fields the API server assigns and then refuses to see
// changed: a replace copies them from the live object when the target does not set them.
var helmImmutableFields = map[string][]string{
	"Service":               {"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy", "healthCheckNodePort"},
	"PersistentVolumeClaim": {"volumeName", "storageClassName"},
}

// helmReplaceBody is what a replace PUTs: the target rendering (ownership already stamped)
// with the live resourceVersion, so a concurrent change makes it fail instead of being
// overwritten, and the live immutable fields. obj.Body is left as it is.
func helmReplaceBody(obj helmObject, live map[string]any) map[string]any {
	body := maps.Clone(obj.Body)

	meta := cloneMap(body["metadata"])
	if rv, ok := mapField(live, "metadata")["resourceVersion"]; ok {
		meta["resourceVersion"] = rv
	}

	body["metadata"] = meta

	fields := helmImmutableFields[obj.Kind]
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

// helmWorkloads are the Deployments, StatefulSets and DaemonSets (apps/v1) among objs.
func helmWorkloads(objs []helmObject) []helmObject {
	out := []helmObject{}

	for _, o := range objs {
		if _, err := findWorkloadKind(o.Kind); err == nil && strings.HasPrefix(o.APIVersion, "apps/") {
			out = append(out, o)
		}
	}

	return out
}
