package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"

	sigsyaml "sigs.k8s.io/yaml"
)

// What a rollback needs of a release's rendered manifest: its objects as JSON maps, each
// resolved to its API path through discovery, in the order Helm installs them, and the
// three-way merge that moves a live object from one revision's manifest to another's.

const (
	helmAnnotationPolicy    = "helm.sh/resource-policy" // "keep": never deleted by Helm
	helmAnnotationRelease   = "meta.helm.sh/release-name"
	helmAnnotationReleaseNS = "meta.helm.sh/release-namespace"
	// The labels the Flux helm-controller puts on everything it renders: the HelmRelease.
	fluxHelmLabelName      = "helm.toolkit.fluxcd.io/name"
	fluxHelmLabelNamespace = "helm.toolkit.fluxcd.io/namespace"
)

// helmInstallOrder is Helm's install order by kind (releaseutil.InstallOrder); other kinds
// come after, deletion runs the other way round.
var helmInstallOrder = []string{
	"PriorityClass", "Namespace", "NetworkPolicy", "ResourceQuota", "LimitRange", "PodSecurityPolicy",
	"PodDisruptionBudget", "ServiceAccount", "Secret", "SecretList", "ConfigMap", "StorageClass",
	"PersistentVolume", "PersistentVolumeClaim", "CustomResourceDefinition", "ClusterRole",
	"ClusterRoleList", "ClusterRoleBinding", "ClusterRoleBindingList", "Role", "RoleList", "RoleBinding",
	"RoleBindingList", "Service", "DaemonSet", "Pod", "ReplicationController", "ReplicaSet", "Deployment",
	"HorizontalPodAutoscaler", "StatefulSet", "Job", "CronJob", "IngressClass", "Ingress", "APIService",
	"MutatingWebhookConfiguration", "ValidatingWebhookConfiguration",
}

// helmObject is one object of a manifest, resolved.
type helmObject struct {
	APIVersion string
	Kind       string
	Namespace  string // "" for cluster-scoped objects
	Name       string
	Path       string // the object's API path
	Body       map[string]any
}

// key identifies the object across revisions: the API version may change, its group not.
func (o helmObject) key() string {
	group, _, _ := strings.Cut(o.APIVersion, "/")
	if !strings.Contains(o.APIVersion, "/") {
		group = ""
	}

	return group + "|" + o.Kind + "|" + o.Namespace + "|" + o.Name
}

func (o helmObject) label() string {
	if o.Namespace == "" {
		return o.Kind + " " + o.Name
	}

	return o.Kind + " " + o.Namespace + "/" + o.Name
}

func (o helmObject) annotation(key string) string {
	meta, _ := o.Body["metadata"].(map[string]any)
	annotations, _ := meta["annotations"].(map[string]any)
	value, _ := annotations[key].(string)

	return value
}

func (o helmObject) objectLabel(key string) string {
	meta, _ := o.Body["metadata"].(map[string]any)
	labels, _ := meta["labels"].(map[string]any)
	value, _ := labels[key].(string)

	return value
}

// helmDocSeparator splits a manifest the way Helm does (releaseutil.SplitManifests).
var helmDocSeparator = regexp.MustCompile(`(?:^|\s*\n)---\s*`)

// parseHelmManifest splits a manifest into its objects, in order, read as Helm reads them
// (sigs.k8s.io/yaml: YAML 1.1 booleans, the last of repeated keys winning) and through JSON,
// so they compare the same way as objects read from the API.
func parseHelmManifest(manifest string) ([]map[string]any, error) {
	out := []map[string]any{}

	for _, doc := range helmDocSeparator.Split(manifest, -1) {
		raw, err := sigsyaml.YAMLToJSON([]byte(doc))
		if err != nil {
			return nil, fmt.Errorf("manifest: %w", err)
		}

		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue // not a mapping: a comment-only or empty document
		}

		if obj["kind"] != nil {
			out = append(out, obj)
		}
	}

	return out, nil
}

// kubeKinds resolves kinds to resources through discovery, one group version read once.
type kubeKinds struct {
	k     *kubeClient
	lists map[string]discoveryResourceList
}

func newKubeKinds(k *kubeClient) *kubeKinds {
	return &kubeKinds{k: k, lists: map[string]discoveryResourceList{}}
}

// resolve finds the resource of apiVersion/kind: its plural and whether it is namespaced.
func (kk *kubeKinds) resolve(ctx context.Context, apiVersion, kind string) (resourceRef, bool, error) {
	list, ok := kk.lists[apiVersion]
	if !ok {
		path := "/apis/" + apiVersion
		if !strings.Contains(apiVersion, "/") {
			path = "/api/" + apiVersion
		}

		if err := kk.k.get(ctx, path, &list); err != nil {
			if isNotFound(err) {
				return resourceRef{}, false, fmt.Errorf("%s is not served by this cluster", apiVersion)
			}

			return resourceRef{}, false, err
		}

		kk.lists[apiVersion] = list
	}

	for _, r := range list.Resources {
		if r.Kind == kind && !strings.Contains(r.Name, "/") {
			group, version, found := strings.Cut(apiVersion, "/")
			if !found {
				group, version = "", apiVersion
			}

			ref, err := newResourceRef(group, version, r.Name)

			return ref, r.Namespaced, err
		}
	}

	return resourceRef{}, false, fmt.Errorf("%s %s is not served by this cluster", apiVersion, kind)
}

// helmObjects resolves a manifest's objects; those without a namespace go to the release's.
func helmObjects(ctx context.Context, kk *kubeKinds, manifest, namespace string) ([]helmObject, error) {
	docs, err := parseHelmManifest(manifest)
	if err != nil {
		return nil, err
	}

	out := make([]helmObject, 0, len(docs))

	for _, doc := range docs {
		apiVersion, _ := doc["apiVersion"].(string)
		kind, _ := doc["kind"].(string)
		meta, _ := doc["metadata"].(map[string]any)
		name, _ := meta["name"].(string)
		ns, _ := meta["namespace"].(string)

		if apiVersion == "" || kind == "" || name == "" {
			return nil, fmt.Errorf("manifest: an object without apiVersion, kind or name (%s %s)", kind, name)
		}

		ref, namespaced, err := kk.resolve(ctx, apiVersion, kind)
		if err != nil {
			return nil, err
		}

		if !namespaced {
			ns = ""
		} else if ns == "" {
			ns = namespace
		}

		out = append(out, helmObject{
			APIVersion: apiVersion, Kind: kind, Namespace: ns, Name: name,
			Path: ref.path(ns) + "/" + url.PathEscape(name), Body: doc,
		})
	}

	return out, nil
}

// helmKindRank is a kind's place in Helm's install order.
func helmKindRank(kind string) int {
	if i := slices.Index(helmInstallOrder, kind); i >= 0 {
		return i
	}

	return len(helmInstallOrder)
}

// threeWayMergePatch is the JSON merge patch that moves live from original to modified, the
// way Helm patches custom resources: what modified sets is set (maps merged key by key, lists
// and scalars replaced), what original had and modified dropped is removed, and what neither
// manifest names (defaults, other controllers' fields, status) is left alone. Empty when live
// already matches.
func threeWayMergePatch(original, modified, live map[string]any) map[string]any {
	patch := map[string]any{}

	for key := range original {
		if _, kept := modified[key]; !kept {
			if _, present := live[key]; present {
				patch[key] = nil
			}
		}
	}

	for key, want := range modified {
		have, present := live[key]

		wantMap, wantIsMap := want.(map[string]any)
		haveMap, haveIsMap := have.(map[string]any)

		if wantIsMap && haveIsMap {
			was, _ := original[key].(map[string]any)
			if sub := threeWayMergePatch(was, wantMap, haveMap); len(sub) > 0 {
				patch[key] = sub
			}

			continue
		}

		if !present || !liveMatches(original[key], want, have) {
			patch[key] = want
		}
	}

	return patch
}

// liveMatches tells whether live already is what want says, where the API server only added
// to it: every field want sets has its value (lists element by element, same length), and
// no field original set and want drops is still there. A list whose entries only gained
// defaults (a container's imagePullPolicy, a port's nodePort) is left alone instead of being
// replaced.
func liveMatches(original, want, have any) bool {
	switch w := want.(type) {
	case map[string]any:
		h, ok := have.(map[string]any)
		if !ok {
			return false
		}

		o, _ := original.(map[string]any)

		for key, value := range w {
			if _, present := h[key]; !present || !liveMatches(o[key], value, h[key]) {
				return false
			}
		}

		for key := range o {
			if _, kept := w[key]; !kept {
				if _, present := h[key]; present {
					return false
				}
			}
		}

		return true
	case []any:
		h, ok := have.([]any)
		if !ok || len(h) != len(w) {
			return false
		}

		o, _ := original.([]any)

		for i := range w {
			var was any
			if len(o) == len(w) {
				was = o[i]
			}

			if !liveMatches(was, w[i], h[i]) {
				return false
			}
		}

		return true
	default:
		return reflect.DeepEqual(want, have)
	}
}
