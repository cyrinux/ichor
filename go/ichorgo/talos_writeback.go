package ichorgo

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Writing a kept machine config change back to the cluster repository: the edits between the
// config a draft started from and the draft itself become the smallest strategic-merge patch
// (TalosConfigPatchFrom), which is then written into the node's file of the repository without
// touching its other lines (TalosRepoWriteBack, talos_writeback_splice.go), and proposed as a
// pull request by the apps.

// talosV1Alpha1 keys the v1alpha1 document of a config (machine, cluster); other documents are
// keyed by apiVersion, kind and name.
const talosV1Alpha1 = "v1alpha1"

var errTalosPatchSame = errors.New("the draft is the same as the config it started from: nothing to write back")

// talosConfigChange is one value the draft changes: from is nil for a new value, to nil for a
// removed one (removals are not in the patch: Talos merges, it does not delete keys).
type talosConfigChange struct {
	Doc  int    `json:"doc"`
	Kind string `json:"kind"`
	Path string `json:"path"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

type talosConfigPatch struct {
	Patch          string              `json:"patch"`
	Changes        []talosConfigChange `json:"changes"`
	TouchesSecrets bool                `json:"touchesSecrets"`
	Notes          []string            `json:"notes"`
}

// TalosConfigPatchFrom is the strategic-merge patch that turns baseYAML into draftYAML (both
// redacted, as the editor holds them): {"patch","changes":[{"doc","kind","path","from","to"}],
// "touchesSecrets","notes"}. Only the changed values are in it, a changed list as a whole (a
// strategic merge replaces lists); a removed value is listed with to null and left out, with a
// note. touchesSecrets: a change is at a secret's path, which is never written back. Local.
func TalosConfigPatchFrom(baseYAML, draftYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	p, err := talosConfigPatchFrom(baseYAML, draftYAML)
	if err != nil {
		return "", err
	}

	return toJSON(p)
}

func talosConfigPatchFrom(baseYAML, draftYAML string) (talosConfigPatch, error) {
	base, err := talosDocsByKey(baseYAML)
	if err != nil {
		return talosConfigPatch{}, fmt.Errorf("read the config: %w", err)
	}

	draft, err := yamlDocuments([]byte(draftYAML))
	if err != nil {
		return talosConfigPatch{}, fmt.Errorf("read the draft: %w", err)
	}

	res := talosConfigPatch{Changes: []talosConfigChange{}, Notes: []string{}}
	seen := map[string]bool{}

	var docs []map[string]any

	for i, d := range draft {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}

		key := talosDocKey(m)
		seen[key] = true

		patch := talosDiffDoc(i, key, base[key], m, &res)
		if len(patch) > 0 {
			docs = append(docs, withDocIdentity(key, m, patch))
		}
	}

	for key := range base {
		if !seen[key] {
			res.Changes = append(res.Changes, talosConfigChange{Doc: -1, Kind: key, Path: "", From: key, To: nil})
			res.Notes = append(res.Notes, "The "+key+" document was removed: remove it from the repository by hand.")
		}
	}

	for _, c := range res.Changes {
		if talosSecretPath(c.Path) || containsRedacted(c.To) {
			res.TouchesSecrets = true
		}
	}

	if len(res.Changes) == 0 {
		return talosConfigPatch{}, errTalosPatchSame
	}

	text, err := encodeYAMLDocs(docs)
	if err != nil {
		return talosConfigPatch{}, err
	}

	res.Patch = text

	return res, nil
}

// talosDiffDoc records the changes from base to draft (document number doc, keyed key) and
// returns the patch of that document: the changed values only, nil when none.
func talosDiffDoc(doc int, key string, base, draft map[string]any, res *talosConfigPatch) map[string]any {
	kind := key
	if key != talosV1Alpha1 {
		kind, _ = draft["kind"].(string)
	}

	var walk func(path []string, b, d map[string]any) map[string]any
	walk = func(path []string, b, d map[string]any) map[string]any {
		patch := map[string]any{}

		for _, k := range keysSorted(d) {
			if len(path) == 0 && key != talosV1Alpha1 && (k == "apiVersion" || k == "kind" || k == "name") {
				continue
			}

			p := append(slices.Clone(path), k)
			bv, had := b[k]
			dm, dIsMap := d[k].(map[string]any)
			bm, bIsMap := bv.(map[string]any)

			switch {
			case dIsMap && bIsMap:
				if sub := walk(p, bm, dm); len(sub) > 0 {
					patch[k] = sub
				}
			case !had:
				res.Changes = append(res.Changes, talosConfigChange{Doc: doc, Kind: kind, Path: strings.Join(p, "."), To: d[k]})
				patch[k] = d[k]
			case !reflect.DeepEqual(bv, d[k]):
				res.Changes = append(res.Changes, talosConfigChange{Doc: doc, Kind: kind, Path: strings.Join(p, "."), From: bv, To: d[k]})
				patch[k] = d[k]
			}
		}

		for _, k := range keysSorted(b) {
			if _, ok := d[k]; !ok {
				label := strings.Join(append(slices.Clone(path), k), ".")
				res.Changes = append(res.Changes, talosConfigChange{Doc: doc, Kind: kind, Path: label, From: b[k]})
				res.Notes = append(res.Notes, label+" was removed: a patch only adds and changes values, remove it from the repository by hand.")
			}
		}

		return patch
	}

	if base == nil {
		base = map[string]any{}
	}

	return walk(nil, base, draft)
}

// withDocIdentity gives a document's patch what names it: apiVersion, kind and name for a
// multi-doc document (v1alpha1's machine and cluster need none).
func withDocIdentity(key string, doc, patch map[string]any) map[string]any {
	if key == talosV1Alpha1 {
		return patch
	}

	for _, k := range []string{"apiVersion", "kind", "name"} {
		if v, ok := doc[k]; ok {
			patch[k] = v
		}
	}

	return patch
}

// talosDocKey names a config document: v1alpha1, or apiVersion/kind/name.
func talosDocKey(m map[string]any) string {
	if m["machine"] != nil || m["cluster"] != nil || m["version"] == talosV1Alpha1 {
		return talosV1Alpha1
	}

	api, _ := m["apiVersion"].(string)
	kind, _ := m["kind"].(string)
	name, _ := m["name"].(string)

	return api + "/" + kind + "/" + name
}

func talosDocsByKey(text string) (map[string]map[string]any, error) {
	docs, err := yamlDocuments([]byte(text))
	if err != nil {
		return nil, err
	}

	out := map[string]map[string]any{}

	for _, d := range docs {
		if m, ok := d.(map[string]any); ok {
			out[talosDocKey(m)] = m
		}
	}

	return out, nil
}

// talosSecretPath tells a dotted path at or under a secret's.
func talosSecretPath(path string) bool {
	for _, keys := range talosSecretPaths {
		p := strings.Join(keys, ".")
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}

	return false
}

// containsRedacted tells a value holding a masked secret.
func containsRedacted(v any) bool {
	switch x := v.(type) {
	case string:
		return x == redacted
	case map[string]any:
		for _, e := range x {
			if containsRedacted(e) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(x, containsRedacted)
	}

	return false
}

func keysSorted[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// encodeYAMLDocs writes docs as one YAML stream, two-space indented.
func encodeYAMLDocs(docs []map[string]any) (string, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return "", fmt.Errorf("encode the patch: %w", err)
		}
	}

	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encode the patch: %w", err)
	}

	return buf.String(), nil
}
