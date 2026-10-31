package kubediff

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

	"github.com/cyrinux/ichor/go/kube"
	"go.yaml.in/yaml/v4"
)

// What a GitOps tool would change in the cluster, object by object: the manifests it would
// apply are sent to the API server as a server-side apply dry run (defaults, webhooks and
// field ownership applied, nothing written), and the answer is compared with the live object.
// Shared by the Flux diff and the Argo CD diff.

const (
	ChangeCreated   = "created"
	ChangeChanged   = "changed"
	ChangeDeleted   = "deleted"
	ChangeUnchanged = "unchanged"
	ChangeEncrypted = "encrypted" // SOPS: the key is in the cluster, never on the phone
	ChangeIgnored   = "ignored"   // the tool leaves it alone (reconcile disabled, create only)
	ChangeError     = "error"     // the dry run was refused: Error says why

	// MaxBytes caps the diff of one object; diffContext is the unchanged lines around a change.
	MaxBytes    = 64 << 10
	diffContext = 3
	// diffMaxCells bounds the line comparison of one object (rows x columns of the table).
	diffMaxCells = 1 << 20

	// SecretMask prefixes what replaces a Secret value: a short keyed digest, so a change
	// still shows.
	SecretMask = "redacted hmac:"
	// MaskedValue replaces, in every object, a value read from a Secret for substitution.
	MaskedValue = "••••••"
	// diffMaskMinLen leaves short Secret values ("true", "80") alone: masking them would hide
	// unrelated text, and they reveal little.
	diffMaskMinLen = 6
)

// Result is what reconciling one GitOps object (a Flux Kustomization, an Argo CD
// Application) would change, object by object.
type Result struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Revision is the source revision built; Applied the one the controller last applied.
	Revision  string     `json:"revision"`
	Applied   string     `json:"applied"`
	Resources []Resource `json:"resources"`
	Warnings  []string   `json:"warnings"`
}

// Resource is one object of a diff.
type Resource struct {
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
	// LiveChanges are the plain values changed in the cluster, for "Keep in Git"
	// (gitops_keep_live.go); absent when there are none.
	LiveChanges []LiveChange `json:"liveChanges,omitempty"`
}

func (r Resource) Less(o Resource) bool {
	return r.Kind+"\x00"+r.Namespace+"\x00"+r.Name < o.Kind+"\x00"+o.Namespace+"\x00"+o.Name
}

// Masker hides what must not leave the cluster in one diff: the values read from
// Secrets for substitution (wherever they land), and Secrets' own values, shown as an HMAC
// under a key of this diff only, so a change still shows but a short value cannot be guessed
// from a screenshot.
type Masker struct {
	values []string // longest first: a value inside another does not leave a tail
	key    []byte
}

func NewMasker() *Masker {
	key := make([]byte, 32)
	_, _ = rand.Read(key)

	return &Masker{key: key}
}

func (m *Masker) Add(v string) {
	if len(v) < diffMaskMinLen || slices.Contains(m.values, v) {
		return
	}

	m.values = append(m.values, v)
	slices.SortFunc(m.values, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
}

// mask hides the values in a text (an error message).
func (m *Masker) Mask(s string) string {
	for _, v := range m.values {
		s = strings.ReplaceAll(s, v, MaskedValue)
	}

	return s
}

// maskValue is a copy of v (decoded JSON) with the values hidden in every string, before
// it is rendered: a multi-line or quoted value is still found whole.
func (m *Masker) maskValue(v any) any {
	switch x := v.(type) {
	case string:
		return m.Mask(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[m.Mask(k)] = m.maskValue(val)
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
func (m *Masker) secretDigest(raw []byte) string {
	h := hmac.New(sha256.New, m.key)
	h.Write(raw)

	return SecretMask + hex.EncodeToString(h.Sum(nil)[:4])
}

// Normalize strips what the API server owns and hides secrets, then renders the object
// as YAML with sorted keys: two renderings differ only where the object does. nil (no
// object) is "".
func Normalize(obj map[string]any, masker *Masker) string {
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
func (m *Masker) redactSecretValues(v any, encoded bool) any {
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

		out[m.Mask(k)] = m.secretDigest(raw)
	}

	return out
}

// APIResource is one resource of the discovery: its plural and whether it is namespaced.
type APIResource struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
}

// Index finds the API path of an apiVersion and kind, reading each group
// version's discovery once.
type Index struct {
	k     kube.Client
	mu    sync.Mutex
	lists map[string][]APIResource
}

func NewIndex(k kube.Client) *Index {
	return &Index{k: k, lists: map[string][]APIResource{}}
}

func (x *Index) Resource(ctx context.Context, apiVersion, kind string) (APIResource, error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	list, ok := x.lists[apiVersion]
	if !ok {
		var answer struct {
			Resources []APIResource `json:"resources"`
		}

		if err := x.k.Get(ctx, groupVersionPath(apiVersion), &answer); err != nil && !kube.IsNotFound(err) {
			return APIResource{}, err
		}

		list = answer.Resources
		x.lists[apiVersion] = list
	}

	for _, r := range list {
		if r.Kind == kind && !strings.Contains(r.Name, "/") {
			return r, nil
		}
	}

	return APIResource{}, fmt.Errorf("the cluster does not serve %s %s (yet)", apiVersion, kind)
}

// path is the API path of the object, namespace ignored for a cluster-wide kind.
func (x *Index) Path(ctx context.Context, apiVersion, kind, namespace, name string) (string, error) {
	r, err := x.Resource(ctx, apiVersion, kind)
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

// ReadLive is the object at path, nil when there is none.
func ReadLive(ctx context.Context, k kube.Client, path string) (map[string]any, error) {
	var live map[string]any
	if err := k.Get(ctx, path, &live); err != nil {
		if kube.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	return live, nil
}

// DryRunApply is the object as the API server would store it if fieldManager applied obj
// (server-side apply, conflicts forced, as the GitOps controllers do). Nothing is written.
func DryRunApply(ctx context.Context, k kube.Client, path, fieldManager string, obj map[string]any) (map[string]any, error) {
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode object: %w", err)
	}

	var wanted map[string]any

	q := url.Values{"dryRun": {"All"}, "force": {"true"}, "fieldManager": {fieldManager}}
	if err := k.Do(ctx, "PATCH", path+"?"+q.Encode(), "application/apply-patch+yaml", body, &wanted); err != nil {
		return nil, err
	}

	return wanted, nil
}

// Objects fills r's Change and Diff from the live and wanted objects (nil: none).
func Objects(r *Resource, live, wanted map[string]any, masker *Masker) {
	from, to := Normalize(live, masker), Normalize(wanted, masker)

	switch {
	case live == nil && wanted == nil:
		r.Change = ChangeUnchanged

		return
	case live == nil:
		r.Change = ChangeCreated
	case wanted == nil:
		r.Change = ChangeDeleted
	case from == to:
		r.Change = ChangeUnchanged

		return
	default:
		r.Change = ChangeChanged
	}

	d := Unified(from, to, "live", "wanted")
	if len(d) > MaxBytes {
		cut := strings.LastIndexByte(d[:MaxBytes], '\n')
		d, r.Truncated = d[:cut+1], true
	}

	r.Diff = d

	if r.Change == ChangeChanged {
		r.LiveChanges = LiveChanges(live, wanted, masker)
	}
}

// RenderRaw renders an object as sorted YAML without stripping anything: for a change
// the normalisation would hide.
func RenderRaw(obj map[string]any) string {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(obj); err != nil {
		return fmt.Sprintf("# not shown: %v\n", err)
	}

	return buf.String()
}

// SplitAPIVersion is the group ("" for the core one) and the version of an apiVersion.
func SplitAPIVersion(apiVersion string) (group, version string) {
	if g, v, ok := strings.Cut(apiVersion, "/"); ok {
		return g, v
	}

	return "", apiVersion
}

// changeOrder lists what needs a look first.
var changeOrder = []string{ChangeError, ChangeCreated, ChangeChanged, ChangeDeleted, ChangeEncrypted, ChangeIgnored, ChangeUnchanged}

// Sort orders the objects of a diff: what needs a look first, then by kind, namespace and name.
func Sort(rs []Resource) {
	slices.SortStableFunc(rs, func(a, b Resource) int {
		if c := cmp.Compare(slices.Index(changeOrder, a.Change), slices.Index(changeOrder, b.Change)); c != 0 {
			return c
		}

		switch {
		case a.Less(b):
			return -1
		case b.Less(a):
			return 1
		default:
			return 0
		}
	})
}
