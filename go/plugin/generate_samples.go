package plugin

import (
	"slices"
	"strings"
)

// samples are two objects built from the schema, with invented names: the first satisfies
// every ok path (each condition True, the ok phase), the second triggers the first critical
// rule, else the first warning one. The rules neither triggers come back for a comment.
func (d *draft) samples() (objects []map[string]any, untriggered []draftRule) {
	ok, failing := d.sampled()

	one := d.sampleObject("demo-one", "demo-shop")
	two := d.sampleObject("demo-two", "demo-lab")

	if st := d.sampleStatus(ok, nil); len(st) > 0 {
		one["status"] = st
	}

	if st := d.sampleStatus(ok, failing); len(st) > 0 {
		two["status"] = st
	}

	for _, r := range d.rules {
		if (ok == nil || r != *ok) && (failing == nil || r != *failing) {
			untriggered = append(untriggered, r)
		}
	}

	return []map[string]any{one, two}, untriggered
}

func (d *draft) sampleObject(name, namespace string) map[string]any {
	meta := map[string]any{"name": name, "creationTimestamp": "now-4m"}
	if d.namespaced {
		meta["namespace"] = namespace
	}

	obj := map[string]any{"metadata": meta}
	if spec := sampleValue(d.v.Property("spec"), "spec", 0); spec != nil {
		if m, isMap := spec.(map[string]any); !isMap || len(m) > 0 {
			obj["spec"] = spec
		}
	}

	return obj
}

// sampleStatus: the conditions all True and the ok phase; failing turns its condition False
// or sets its phase.
func (d *draft) sampleStatus(ok, failing *draftRule) map[string]any {
	st := map[string]any{}

	if d.hasConds && len(d.conditions) > 0 {
		var conds []any

		for _, t := range d.conditions {
			status := "True"
			if failing != nil && failing.when == "conditions."+t+" == false" {
				status = "False"
			}

			conds = append(conds, map[string]any{"type": t, "status": status, "reason": "Demo", "lastTransitionTime": "now-4m"})
		}

		st["conditions"] = conds
	}

	if d.phaseField != "" {
		switch {
		case failing != nil && failing.health == "critical" && len(d.phase.critical) > 0:
			st["phase"] = d.phase.critical[0]
		case ok != nil && len(d.phase.ok) > 0:
			st["phase"] = d.phase.ok[0]
		}
	}

	return st
}

// sampleValue fills a schema node's required properties by type, one level of objects
// deep: strings "demo-<prop>", integers 1, booleans their default else false, enums their
// first value, arrays empty.
func sampleValue(node map[string]any, prop string, depth int) any {
	if node == nil {
		return nil
	}

	if enum, _ := node["enum"].([]any); len(enum) > 0 { //nolint:errcheck // nil when absent
		return enum[0]
	}

	switch node["type"] {
	case "string":
		return "demo-" + strings.ToLower(prop)
	case "integer", "number":
		return 1
	case "boolean":
		if b, isBool := node["default"].(bool); isBool {
			return b
		}

		return false
	case "array":
		return []any{}
	case "object":
		out := map[string]any{}
		if depth > 1 {
			return out
		}

		props, _ := node["properties"].(map[string]any) //nolint:errcheck // nil when absent
		required, _ := node["required"].([]any)         //nolint:errcheck // nil when absent

		names := make([]string, 0, len(required))
		for _, r := range required {
			if s, isString := r.(string); isString {
				names = append(names, s)
			}
		}

		slices.Sort(names)

		for _, name := range names {
			child, _ := props[name].(map[string]any) //nolint:errcheck // nil when absent
			if v := sampleValue(child, name, depth+1); v != nil {
				out[name] = v
			}
		}

		return out
	}

	return nil
}
