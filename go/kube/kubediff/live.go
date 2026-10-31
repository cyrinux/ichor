package kubediff

import (
	"slices"
	"strconv"
	"strings"
)

// MaxLiveChanges bounds the values listed for one object.
const MaxLiveChanges = 50

// LiveChange is one plain value an object has live but not as its tool would apply it.
//
//	{"path":["spec","template","spec","containers","name=app","image"],
//	 "label":"spec.template.spec.containers[app].image","wanted":"app:1.2","live":"app:1.3","type":"str"}
//
// path is a key per mapping; in a list, the item's index, or name=<its name> for a list of
// named items (containers, env, ports). type is the live value's YAML tag: str, int, float or bool.
type LiveChange struct {
	Path   []string `json:"path"`
	Label  string   `json:"label"`
	Wanted string   `json:"wanted"`
	Live   string   `json:"live"`
	Type   string   `json:"type"`
}

// NamePrefix marks a path segment naming a list item by its name.
const NamePrefix = "name="

// LiveChanges are the plain values (strings, numbers, booleans) of live that differ from
// wanted, at paths both objects have. Never for a Secret, nor for a value holding a Secret
// value read for substitution: what is kept goes to Git in clear.
func LiveChanges(live, wanted map[string]any, masker *Masker) []LiveChange {
	if live == nil || wanted == nil || wanted["kind"] == "Secret" || live["kind"] == "Secret" {
		return nil
	}

	var out []LiveChange

	var walk func(path []string, label string, w, l any)
	walk = func(path []string, label string, w, l any) {
		if len(out) >= MaxLiveChanges {
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
						walk(append(slices.Clone(path), NamePrefix+name), label+"["+name+"]", item, li)
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

			if masker != nil && (masker.Mask(wt) != wt || masker.Mask(lt) != lt) {
				return
			}

			out = append(out, LiveChange{Path: path, Label: label, Wanted: wt, Live: lt, Type: typ})
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
