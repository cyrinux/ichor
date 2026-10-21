package ichorgo

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Keeping a live change in Git: an object changed in the cluster by hand (kubectl scale, set
// image, edit) differs from what its GitOps tool would apply. The diff lists the plain values
// that differ (diffLiveChanges); the "keep" quick edit writes the live ones into the manifest,
// only where the file itself holds the wanted value, so a value set by an overlay, a patch, a
// substitution or a chart is left alone and reported instead of being written in the wrong place.

// diffMaxLiveChanges bounds the values listed for one object.
const diffMaxLiveChanges = 50

// diffLiveChange is one plain value an object has live but not as its tool would apply it.
//
//	{"path":["spec","template","spec","containers","name=app","image"],
//	 "label":"spec.template.spec.containers[app].image","wanted":"app:1.2","live":"app:1.3","type":"str"}
//
// path is a key per mapping; in a list, the item's index, or name=<its name> for a list of
// named items (containers, env, ports). type is the live value's YAML tag: str, int, float or bool.
type diffLiveChange struct {
	Path   []string `json:"path"`
	Label  string   `json:"label"`
	Wanted string   `json:"wanted"`
	Live   string   `json:"live"`
	Type   string   `json:"type"`
}

// keepNamePrefix marks a path segment naming a list item by its name.
const keepNamePrefix = "name="

// diffLiveChanges are the plain values (strings, numbers, booleans) of live that differ from
// wanted, at paths both objects have. Never for a Secret, nor for a value holding a Secret
// value read for substitution: what is kept goes to Git in clear.
func diffLiveChanges(live, wanted map[string]any, masker *kubeDiffMasker) []diffLiveChange {
	if live == nil || wanted == nil || wanted["kind"] == "Secret" || live["kind"] == "Secret" {
		return nil
	}

	var out []diffLiveChange

	var walk func(path []string, label string, w, l any)
	walk = func(path []string, label string, w, l any) {
		if len(out) >= diffMaxLiveChanges {
			return
		}

		switch wv := w.(type) {
		case map[string]any:
			lv, ok := l.(map[string]any)
			if !ok {
				return
			}

			keys := make([]string, 0, len(wv))
			for k := range wv {
				if _, ok := lv[k]; ok && !keepSkipped(path, k) {
					keys = append(keys, k)
				}
			}

			slices.Sort(keys)

			for _, k := range keys {
				walk(append(slices.Clone(path), k), joinLabel(label, k), wv[k], lv[k])
			}
		case []any:
			lv, ok := l.([]any)
			if !ok {
				return
			}

			if wn, ln := namedItems(wv), namedItems(lv); wn != nil && ln != nil {
				for _, item := range wv {
					name := itemName(item)
					if li, ok := ln[name]; ok {
						walk(append(slices.Clone(path), keepNamePrefix+name), label+"["+name+"]", item, li)
					}
				}

				return
			}

			if len(wv) != len(lv) {
				return
			}

			for i := range wv {
				walk(append(slices.Clone(path), strconv.Itoa(i)), label+"["+strconv.Itoa(i)+"]", wv[i], lv[i])
			}
		default:
			wt, wok := keepScalar(w)
			lt, typ, lok := keepScalarTyped(l)
			if !wok || !lok || wt == lt {
				return
			}

			if masker != nil && (masker.mask(wt) != wt || masker.mask(lt) != lt) {
				return
			}

			out = append(out, diffLiveChange{Path: path, Label: label, Wanted: wt, Live: lt, Type: typ})
		}
	}

	walk(nil, "", wanted, live)

	return out
}

// keepSkipped: what the API server or the tool owns, never a change to keep in Git.
func keepSkipped(path []string, key string) bool {
	switch len(path) {
	case 0:
		return key == "status" || key == "apiVersion" || key == "kind"
	case 1:
		return path[0] == "metadata" && slices.Contains([]string{
			"name", "namespace", "managedFields", "resourceVersion", "generation", "uid",
			"creationTimestamp", "selfLink", "ownerReferences", "finalizers",
		}, key)
	case 2:
		return path[0] == "metadata" && path[1] == "annotations" &&
			(key == "kubectl.kubernetes.io/last-applied-configuration" || key == "deployment.kubernetes.io/revision")
	}

	return false
}

func joinLabel(label, key string) string {
	if label == "" {
		return key
	}

	return label + "." + key
}

// namedItems indexes a list whose items are all mappings with a distinct string name; nil otherwise.
func namedItems(items []any) map[string]any {
	if len(items) == 0 {
		return nil
	}

	out := make(map[string]any, len(items))

	for _, item := range items {
		name := itemName(item)
		if name == "" {
			return nil
		}

		if _, dup := out[name]; dup {
			return nil
		}

		out[name] = item
	}

	return out
}

func itemName(item any) string {
	m, _ := item.(map[string]any)
	s, _ := m["name"].(string)

	return s
}

func keepScalar(v any) (string, bool) {
	s, _, ok := keepScalarTyped(v)

	return s, ok
}

// keepScalarTyped is a decoded JSON scalar as YAML would write it, and its tag.
func keepScalarTyped(v any) (string, string, bool) {
	switch x := v.(type) {
	case string:
		if strings.ContainsAny(x, "\n\r") || len(x) > 1024 {
			return "", "", false
		}

		return x, "str", true
	case bool:
		return strconv.FormatBool(x), "bool", true
	case int:
		return strconv.Itoa(x), "int", true
	case int64:
		return strconv.FormatInt(x, 10), "int", true
	case float64:
		if x == float64(int64(x)) && x < 1e15 && x > -1e15 {
			return strconv.FormatInt(int64(x), 10), "int", true
		}

		return strconv.FormatFloat(x, 'g', -1, 64), "float", true
	}

	return "", "", false
}

// keepTargets are the values of the "keep" edit to write: each change whose path the
// document has, as a plain value equal to the wanted one, with the index of its change in
// applied. The others are skipped, with why.
func keepTargets(root *yaml.Node, e quickEdit) (targets []quickEditTarget, applied []int, skipped []string, err error) {
	if len(e.Changes) == 0 {
		return nil, nil, nil, errors.New("say the live values to keep")
	}

	if len(e.Changes) > diffMaxLiveChanges {
		return nil, nil, nil, fmt.Errorf("at most %d values at once", diffMaxLiveChanges)
	}

	for i, c := range e.Changes {
		if !slices.Contains([]string{"str", "int", "float", "bool"}, c.Type) {
			return nil, nil, nil, fmt.Errorf("the value of %s has an unknown type %q", c.Label, c.Type)
		}

		if strings.ContainsAny(c.Live, "\n\r") || len(c.Live) > 1024 {
			return nil, nil, nil, fmt.Errorf("%s: a quick edit writes one line: edit it in the editor", c.Label)
		}

		n := keepNode(root, c.Path)

		switch {
		case n == nil:
			skipped = append(skipped, c.Label+": not set in this file")
		case n.Kind != yaml.ScalarNode:
			skipped = append(skipped, c.Label+": not a plain value in this file")
		case !keepSameValue(n.Value, c.Wanted):
			skipped = append(skipped, c.Label+": the file has "+n.Value+", set to "+c.Wanted+" elsewhere (an overlay, a patch or a substitution)")
		default:
			targets = append(targets, quickEditTarget{node: n, value: c.Live, tag: "!!" + c.Type})
			applied = append(applied, i)
		}
	}

	if len(targets) == 0 {
		return nil, nil, skipped, fmt.Errorf("none of the live values are set in this file: %s", strings.Join(skipped, "; "))
	}

	return targets, applied, skipped, nil
}

// keepNode is the node at path in root: a key per mapping, an index or name=<name> per list.
func keepNode(root *yaml.Node, path []string) *yaml.Node {
	n := root

	for _, seg := range path {
		if n == nil {
			return nil
		}

		if n.Kind == yaml.SequenceNode && strings.HasPrefix(seg, keepNamePrefix) {
			name := strings.TrimPrefix(seg, keepNamePrefix)
			i := slices.IndexFunc(n.Content, func(item *yaml.Node) bool { return scalarAt(item, "name") == name })

			if i < 0 {
				return nil
			}

			n = n.Content[i]

			continue
		}

		n = pathStep(n, seg)
	}

	return n
}

// keepSameValue: the file's text and the wanted value are the same value (8080 and
// "8080", 1.0 and 1, true and True).
func keepSameValue(file, wanted string) bool {
	if file == wanted {
		return true
	}

	if a, err := strconv.ParseFloat(file, 64); err == nil {
		if b, err := strconv.ParseFloat(wanted, 64); err == nil {
			return a == b
		}
	}

	if a, err := strconv.ParseBool(file); err == nil {
		if b, err := strconv.ParseBool(wanted); err == nil {
			return a == b
		}
	}

	return false
}

// verifyKeep parses the rewritten file again: same documents, and each applied change now
// holds its live value.
func verifyKeep(out string, count int, e quickEdit, applied []int) error {
	docs, err := parseQuickEditDocs(out)
	if err != nil || len(docs) != count {
		return errors.New("the rewritten file does not parse the same: edit it in the editor")
	}

	index, err := quickEditDocIndex(docs, e)
	if err != nil {
		return fmt.Errorf("the rewritten file does not parse the same: %w", err)
	}

	for _, i := range applied {
		c := e.Changes[i]
		if n := keepNode(docs[index], c.Path); n == nil || n.Kind != yaml.ScalarNode || n.Value != c.Live {
			return errors.New("the rewritten file does not hold the live value: edit it in the editor")
		}
	}

	return nil
}
