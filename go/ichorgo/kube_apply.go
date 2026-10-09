package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v4"
)

// Applying manifests the user pasted or shared (a snippet from a chat, a runbook, a Gist),
// like `kubectl apply --server-side`: each document is first sent as a dry run and shown as a
// diff against the object as it is (created, changed, unchanged), then applied for real. The
// field manager is "ichor" and conflicts are not forced: a field another manager owns (a
// GitOps controller, an HPA) is refused with its name, as kubectl does. Refused in the demo.

const (
	applyTimeout     = 90 * time.Second
	applyMaxDocs     = 200
	applyMaxBytes    = 4 << 20
	applyParallel    = 6
	applyFieldManage = "ichor"
)

var (
	errApplyEmpty   = errors.New("nothing to apply: the text holds no Kubernetes object")
	errApplyTooBig  = fmt.Errorf("the text is larger than the app applies (%d MB)", applyMaxBytes>>20)
	errApplyTooMany = fmt.Errorf("more than %d objects: apply them in smaller batches", applyMaxDocs)
)

// kubeApplyResult is what applying the documents did, or would do: one row per object, in
// the order of the text, sorted like a diff (what needs a look first).
type kubeApplyResult struct {
	Resources []kubeDiffResource `json:"resources"`
	Warnings  []string           `json:"warnings"`
	// Applied is set after a real apply: how many objects went through.
	Applied int `json:"applied"`
	Failed  int `json:"failed"`
}

// KubeApplyPreview dry-runs the YAML documents of manifests (os:admin) and returns a JSON
// kubeApplyResult: per object created, changed (with a unified diff), unchanged or error.
// namespace is used for a namespaced object that names none, like kubectl's -n.
func KubeApplyPreview(configYAML, contextName, kubeServer, namespace, manifests string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.reveal(strings.TrimSpace(namespace))

	objs, err := applyDocuments(namespace, manifests)
	if err != nil {
		return "", err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return "", errDemoUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()

	res, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (kubeApplyResult, error) {
		return applyObjects(ctx, k, objs, namespace, true), nil
	})
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

// KubeApply applies the YAML documents of manifests (os:admin) with server-side apply, each
// on its own: one refused object does not stop the others. Returns a JSON kubeApplyResult.
func KubeApply(configYAML, contextName, kubeServer, namespace, manifests string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.reveal(strings.TrimSpace(namespace))

	objs, err := applyDocuments(namespace, manifests)
	if err != nil {
		return "", err
	}

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "apply", Namespace: namespace, Object: applyAuditObject(objs), Params: applyAuditParams(objs)})

	target := kubeTarget{configYAML, contextName, kubeServer}
	if isDemoContext(target.config, target.context) {
		return "", errDemoUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()

	var res kubeApplyResult

	err = kubeDo(ctx, target, func(ctx context.Context, k *kubeClient) error {
		res = applyObjects(ctx, k, objs, namespace, false)

		return nil
	})
	if err != nil {
		return "", kubeMutationError(err)
	}

	if res.Failed > 0 {
		// Recorded as a failure in the audit log, but the result still tells what went through.
		err = fmt.Errorf("%d of %d objects refused", res.Failed, len(res.Resources))
		out, _ = toJSON(res)

		return out, err
	}

	return toJSON(res)
}

// applyDocuments parses the manifests into objects, checking the size and that each has
// apiVersion, kind and a name; namespace fills in a missing one later, once the kind is known
// to be namespaced.
func applyDocuments(namespace, manifests string) ([]map[string]any, error) {
	if len(manifests) > applyMaxBytes {
		return nil, errApplyTooBig
	}

	if err := validateNamespace(namespace); err != nil {
		return nil, err
	}

	if strings.Contains(manifests, hiddenSecretMarker) {
		return nil, errors.New("the text holds hidden Secret values: paste the real values or leave the Secret out")
	}

	objs, err := parseYAMLObjects([]byte(manifests))
	if err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	switch {
	case len(objs) == 0:
		return nil, errApplyEmpty
	case len(objs) > applyMaxDocs:
		return nil, errApplyTooMany
	}

	for i, obj := range objs {
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		meta, _ := obj["metadata"].(map[string]any)
		name, _ := meta["name"].(string)

		switch {
		case apiVersion == "" || kind == "":
			return nil, fmt.Errorf("object %d has no apiVersion or kind", i+1)
		case name == "":
			return nil, fmt.Errorf("%s %d has no metadata.name", kind, i+1)
		case !kubeNamePattern.MatchString(name):
			return nil, fmt.Errorf("%s: invalid name %q", kind, name)
		}

		if ns, _ := meta["namespace"].(string); ns != "" {
			if err := validateNamespace(ns); err != nil {
				return nil, fmt.Errorf("%s %s: %w", kind, name, err)
			}
		}

		// The API server refuses a resourceVersion in an apply patch; a pasted `kubectl get -o
		// yaml` is the common source, so the read-only fields go, as kubectl's apply would ignore them.
		for _, k := range []string{"resourceVersion", "uid", "creationTimestamp", "generation", "managedFields", "selfLink"} {
			delete(meta, k)
		}

		delete(obj, "status")
	}

	return objs, nil
}

// parseYAMLObjects reads a multi-document YAML stream of Kubernetes objects; a List is
// expanded, empty documents and comments are skipped.
func parseYAMLObjects(data []byte) ([]map[string]any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))

	var out []map[string]any

	for {
		var doc any

		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return out, nil
		}

		if err != nil {
			return nil, err
		}

		obj, ok := yamlToJSON(doc).(map[string]any)
		if !ok || len(obj) == 0 {
			continue
		}

		if kind, _ := obj["kind"].(string); kind == "List" {
			items, _ := obj["items"].([]any)
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					out = append(out, m)
				}
			}

			continue
		}

		out = append(out, obj)
	}
}

// yamlToJSON turns a decoded YAML value into what encoding/json decodes: string keys, times
// as RFC 3339 text.
func yamlToJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = yamlToJSON(val)
		}

		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = yamlToJSON(val)
		}

		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = yamlToJSON(val)
		}

		return out
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return v
	}
}

// applyObjects applies (or dry-runs) each object, applyParallel at a time, and sums up.
func applyObjects(ctx context.Context, k *kubeClient, objs []map[string]any, namespace string, dryRun bool) kubeApplyResult {
	out := kubeApplyResult{Resources: make([]kubeDiffResource, len(objs)), Warnings: []string{}}
	index := newKubeResourceIndex(k)
	masker := newKubeDiffMasker()
	sem := make(chan struct{}, applyParallel)

	var wg sync.WaitGroup

	for i, obj := range objs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			out.Resources[i] = applyObject(ctx, k, index, obj, namespace, dryRun, masker)
		})
	}

	wg.Wait()

	for _, r := range out.Resources {
		if r.Change == diffChangeError {
			out.Failed++
		} else if !dryRun {
			out.Applied++
		}
	}

	sortDiffResources(out.Resources)

	return out
}

func applyObject(ctx context.Context, k *kubeClient, index *kubeResourceIndex, obj map[string]any, namespace string, dryRun bool, masker *kubeDiffMasker) kubeDiffResource {
	apiVersion, _ := obj["apiVersion"].(string)
	kind, _ := obj["kind"].(string)
	meta, _ := obj["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	ns, _ := meta["namespace"].(string)

	group, version := splitAPIVersion(apiVersion)
	r := kubeDiffResource{Group: group, Version: version, Kind: kind, Namespace: ns, Name: name}

	fail := func(err error) kubeDiffResource {
		r.Change, r.Error = diffChangeError, masker.mask(applyError(err).Error())

		return r
	}

	res, err := index.resource(ctx, apiVersion, kind)
	if err != nil {
		return fail(err)
	}

	switch {
	case !res.Namespaced:
		ns = ""
		delete(meta, "namespace")
	case ns == "":
		if namespace == "" {
			return fail(fmt.Errorf("%s is namespaced: give it a namespace, or pick one", kind))
		}

		ns = namespace
		meta["namespace"] = namespace
	}

	r.Namespace = ns

	path, err := index.path(ctx, apiVersion, kind, ns, name)
	if err != nil {
		return fail(err)
	}

	live, err := readLive(ctx, k, path)
	if err != nil {
		return fail(err)
	}

	applied, err := serverSideApply(ctx, k, path, applyFieldManage, obj, dryRun, false)
	if err != nil {
		return fail(err)
	}

	diffObjects(&r, live, applied, masker)

	if live == nil {
		r.Change = diffChangeCreated
	}

	return r
}

// serverSideApply sends obj as an apply patch by fieldManager: a dry run leaves the cluster
// untouched; force takes over fields other managers own instead of failing on them.
func serverSideApply(ctx context.Context, k *kubeClient, path, fieldManager string, obj map[string]any, dryRun, force bool) (map[string]any, error) {
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode object: %w", err)
	}

	q := url.Values{"fieldManager": {fieldManager}}
	if dryRun {
		q.Set("dryRun", "All")
	}

	if force {
		q.Set("force", "true")
	}

	var stored map[string]any
	if err := k.do(ctx, http.MethodPatch, path+"?"+q.Encode(), "application/apply-patch+yaml", body, &stored); err != nil {
		return nil, err
	}

	return stored, nil
}

// applyError explains a refused apply: a field another manager owns, or a kind that needs a
// permission the account lacks.
func applyError(err error) error {
	switch kubeCode(err) {
	case http.StatusConflict:
		return fmt.Errorf("another manager owns a field this would change (%w): change it where it is managed, or remove the field", err)
	case http.StatusForbidden:
		return fmt.Errorf("not allowed for your account: %w", err)
	default:
		return err
	}
}

// applyAuditObject names what an apply touched for the audit log: the one object, or a count.
func applyAuditObject(objs []map[string]any) string {
	if len(objs) == 1 {
		kind, _ := objs[0]["kind"].(string)
		meta, _ := objs[0]["metadata"].(map[string]any)
		name, _ := meta["name"].(string)

		return kind + "/" + name
	}

	return fmt.Sprintf("%d objects", len(objs))
}

// applyAuditParams lists the kinds applied ("Deployment, ConfigMap x2").
func applyAuditParams(objs []map[string]any) string {
	counts := map[string]int{}
	order := []string{}

	for _, o := range objs {
		kind, _ := o["kind"].(string)
		if counts[kind] == 0 {
			order = append(order, kind)
		}

		counts[kind]++
	}

	parts := make([]string, 0, len(order))

	for _, kind := range order {
		if counts[kind] > 1 {
			parts = append(parts, fmt.Sprintf("%s x%d", kind, counts[kind]))
		} else {
			parts = append(parts, kind)
		}
	}

	return strings.Join(parts, ", ")
}
