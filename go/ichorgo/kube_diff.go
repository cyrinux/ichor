package ichorgo

import (
	"bytes"
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"go.yaml.in/yaml/v4"
)

// What a GitOps tool would change in the cluster, object by object: the manifests it would
// apply are sent to the API server as a server-side apply dry run (defaults, webhooks and
// field ownership applied, nothing written), and the answer is compared with the live object.
// Shared by the Flux diff, and later Argo CD's.

const (
	diffChangeCreated   = "created"
	diffChangeChanged   = "changed"
	diffChangeDeleted   = "deleted"
	diffChangeUnchanged = "unchanged"
	diffChangeEncrypted = "encrypted" // SOPS: the key is in the cluster, never on the phone
	diffChangeIgnored   = "ignored"   // the tool leaves it alone (reconcile disabled, create only)
	diffChangeError     = "error"     // the dry run was refused: Error says why

	// diffMaxBytes caps the diff of one object; diffContext is the unchanged lines around a change.
	diffMaxBytes = 64 << 10
	diffContext  = 3
	// diffMaxCells bounds the line comparison of one object (rows x columns of the table).
	diffMaxCells = 1 << 20

	// diffSecretMask prefixes what replaces a Secret value: a short keyed digest, so a change
	// still shows.
	diffSecretMask = "redacted hmac:"
	// diffMaskedValue replaces, in every object, a value read from a Secret for substitution.
	diffMaskedValue = "••••••"
	// diffMaskMinLen leaves short Secret values ("true", "80") alone: masking them would hide
	// unrelated text, and they reveal little.
	diffMaskMinLen = 6
)

// kubeDiffResource is one object of a diff.
type kubeDiffResource struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Change    string `json:"change"` // created|changed|deleted|unchanged|encrypted|ignored|error
	// Diff is a unified diff from live to wanted, "" when unchanged or not compared.
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
}

func (r kubeDiffResource) less(o kubeDiffResource) bool {
	return r.Kind+"\x00"+r.Namespace+"\x00"+r.Name < o.Kind+"\x00"+o.Namespace+"\x00"+o.Name
}

// kubeDiffMasker hides what must not leave the cluster in one diff: the values read from
// Secrets for substitution (wherever they land), and Secrets' own values, shown as an HMAC
// under a key of this diff only, so a change still shows but a short value cannot be guessed
// from a screenshot.
type kubeDiffMasker struct {
	values []string // longest first: a value inside another does not leave a tail
	key    []byte
}

func newKubeDiffMasker() *kubeDiffMasker {
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	return &kubeDiffMasker{key: key}
}

func (m *kubeDiffMasker) add(v string) {
	if len(v) < diffMaskMinLen || slices.Contains(m.values, v) {
		return
	}

	m.values = append(m.values, v)
	slices.SortFunc(m.values, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
}

// mask hides the values in a text (an error message).
func (m *kubeDiffMasker) mask(s string) string {
	for _, v := range m.values {
		s = strings.ReplaceAll(s, v, diffMaskedValue)
	}

	return s
}

// maskValue is a copy of v (decoded JSON) with the values hidden in every string, before
// it is rendered: a multi-line or quoted value is still found whole.
func (m *kubeDiffMasker) maskValue(v any) any {
	switch x := v.(type) {
	case string:
		return m.mask(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[m.mask(k)] = m.maskValue(val)
		}

		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = m.maskValue(val)
		}

		return out
	default:
		return v
	}
}

// secretDigest stands for a Secret value: the same bytes give the same digest within a diff.
func (m *kubeDiffMasker) secretDigest(raw []byte) string {
	h := hmac.New(sha256.New, m.key)
	h.Write(raw)

	return diffSecretMask + hex.EncodeToString(h.Sum(nil)[:4])
}

// diffNormalize strips what the API server owns and hides secrets, then renders the object
// as YAML with sorted keys: two renderings differ only where the object does. nil (no
// object) is "".
func diffNormalize(obj map[string]any, masker *kubeDiffMasker) string {
	if obj == nil {
		return ""
	}

	o, _ := masker.maskValue(obj).(map[string]any)
	delete(o, "status")

	if meta, ok := o["metadata"].(map[string]any); ok {
		for _, k := range []string{"managedFields", "resourceVersion", "generation", "uid", "creationTimestamp", "selfLink"} {
			delete(meta, k)
		}

		if ann, ok := meta["annotations"].(map[string]any); ok {
			delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
			delete(ann, "deployment.kubernetes.io/revision")

			if len(ann) == 0 {
				delete(meta, "annotations")
			}
		}
	}

	if obj["kind"] == "Secret" && obj["apiVersion"] == "v1" {
		// From obj, not o: the digest is of the real value.
		if d, ok := obj["data"]; ok {
			o["data"] = masker.redactSecretValues(d, true)
		}

		if sd, ok := obj["stringData"]; ok {
			o["stringData"] = masker.redactSecretValues(sd, false)
		}
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(o); err != nil {
		return fmt.Sprintf("# not shown: %v\n", err)
	}

	return buf.String()
}

// redactSecretValues replaces each value of a Secret's data (base64) or stringData by the
// digest of its decoded bytes: the same value in both forms gives the same digest.
func (m *kubeDiffMasker) redactSecretValues(v any, encoded bool) any {
	values, ok := v.(map[string]any)
	if !ok {
		return v
	}

	out := make(map[string]any, len(values))

	for k, val := range values {
		s, _ := val.(string)
		raw := []byte(s)

		if encoded {
			if d, err := base64.StdEncoding.DecodeString(s); err == nil {
				raw = d
			}
		}

		out[m.mask(k)] = m.secretDigest(raw)
	}

	return out
}

// kubeAPIResource is one resource of the discovery: its plural and whether it is namespaced.
type kubeAPIResource struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
}

// kubeResourceIndex finds the API path of an apiVersion and kind, reading each group
// version's discovery once.
type kubeResourceIndex struct {
	k     *kubeClient
	mu    sync.Mutex
	lists map[string][]kubeAPIResource
}

func newKubeResourceIndex(k *kubeClient) *kubeResourceIndex {
	return &kubeResourceIndex{k: k, lists: map[string][]kubeAPIResource{}}
}

func (x *kubeResourceIndex) resource(ctx context.Context, apiVersion, kind string) (kubeAPIResource, error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	list, ok := x.lists[apiVersion]
	if !ok {
		var answer struct {
			Resources []kubeAPIResource `json:"resources"`
		}

		if err := x.k.get(ctx, groupVersionPath(apiVersion), &answer); err != nil && !isNotFound(err) {
			return kubeAPIResource{}, err
		}

		list = answer.Resources
		x.lists[apiVersion] = list
	}

	for _, r := range list {
		if r.Kind == kind && !strings.Contains(r.Name, "/") {
			return r, nil
		}
	}

	return kubeAPIResource{}, fmt.Errorf("the cluster does not serve %s %s (yet)", apiVersion, kind)
}

// path is the API path of the object, namespace ignored for a cluster-wide kind.
func (x *kubeResourceIndex) path(ctx context.Context, apiVersion, kind, namespace, name string) (string, error) {
	r, err := x.resource(ctx, apiVersion, kind)
	if err != nil {
		return "", err
	}

	p := groupVersionPath(apiVersion)
	if r.Namespaced {
		p += "/namespaces/" + url.PathEscape(namespace)
	}

	return p + "/" + r.Name + "/" + url.PathEscape(name), nil
}

func groupVersionPath(apiVersion string) string {
	if strings.Contains(apiVersion, "/") {
		return "/apis/" + apiVersion
	}

	return "/api/" + apiVersion
}

// readLive is the object at path, nil when there is none.
func readLive(ctx context.Context, k *kubeClient, path string) (map[string]any, error) {
	var live map[string]any
	if err := k.get(ctx, path, &live); err != nil {
		if isNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return live, nil
}

// dryRunApply is the object as the API server would store it if fieldManager applied obj
// (server-side apply, conflicts forced, as the GitOps controllers do). Nothing is written.
func dryRunApply(ctx context.Context, k *kubeClient, path, fieldManager string, obj map[string]any) (map[string]any, error) {
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode object: %w", err)
	}

	var wanted map[string]any

	q := url.Values{"dryRun": {"All"}, "force": {"true"}, "fieldManager": {fieldManager}}
	if err := k.do(ctx, "PATCH", path+"?"+q.Encode(), "application/apply-patch+yaml", body, &wanted); err != nil {
		return nil, err
	}

	return wanted, nil
}

// diffObjects fills r's Change and Diff from the live and wanted objects (nil: none).
func diffObjects(r *kubeDiffResource, live, wanted map[string]any, masker *kubeDiffMasker) {
	from, to := diffNormalize(live, masker), diffNormalize(wanted, masker)

	switch {
	case live == nil && wanted == nil:
		r.Change = diffChangeUnchanged

		return
	case live == nil:
		r.Change = diffChangeCreated
	case wanted == nil:
		r.Change = diffChangeDeleted
	case from == to:
		r.Change = diffChangeUnchanged

		return
	default:
		r.Change = diffChangeChanged
	}

	d := unifiedDiff(from, to, "live", "wanted")
	if len(d) > diffMaxBytes {
		cut := strings.LastIndexByte(d[:diffMaxBytes], '\n')
		d, r.Truncated = d[:cut+1], true
	}

	r.Diff = d
}
