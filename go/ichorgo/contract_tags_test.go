package ichorgo

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestContractTypesAreTagged walks every type a contract root can reach and refuses an exported
// field with no json tag. encoding/json writes such a field under its Go name (LiveChanges where
// the apps read liveChanges), and an empty list under omitempty hides it from the fixtures, so the
// tag is checked on the type rather than on the JSON (CYR-581).
//
// The walk cannot see inside interface values (any, map[string]any) nor json.RawMessage; those
// carry free-form payloads the apps read without a model (diag attributes, plugin cards).
func TestContractTypesAreTagged(t *testing.T) {
	var missing []string

	seen := map[reflect.Type]bool{}

	for _, f := range contractFixtures {
		missing = append(missing, untaggedFields(reflect.TypeOf(f.build()), seen)...)
	}

	slices.Sort(missing)

	for _, m := range slices.Compact(missing) {
		t.Errorf("%s is exported with no json tag: tag it (json:\"-\" to keep it out of the JSON)", m)
	}
}

var (
	jsonRawType = reflect.TypeFor[json.RawMessage]()
	timeType    = reflect.TypeFor[time.Time]()
)

// untaggedFields is every exported field reachable from t that has no json tag, as pkg.Type.Field.
// seen stops cycles and repeats across roots.
func untaggedFields(t reflect.Type, seen map[reflect.Type]bool) []string {
	if t == nil || seen[t] || t == jsonRawType || t == timeType {
		return nil
	}

	seen[t] = true

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return untaggedFields(t.Elem(), seen)
	case reflect.Map:
		return untaggedFields(t.Elem(), seen)
	case reflect.Struct:
		return untaggedStructFields(t, seen)
	default:
		return nil
	}
}

func untaggedStructFields(t reflect.Type, seen map[reflect.Type]bool) []string {
	var out []string

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}

		tag, tagged := f.Tag.Lookup("json")
		if tag == "-" {
			continue
		}

		// An embedded struct with no tag is flattened by encoding/json: walk it, do not flag it.
		if !tagged && !f.Anonymous {
			out = append(out, typeName(t)+"."+f.Name)
		}

		out = append(out, untaggedFields(f.Type, seen)...)
	}

	return out
}

// typeName is pkg.Type, the package's last path element (kubediff.Resource).
func typeName(t reflect.Type) string {
	pkg := t.PkgPath()
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}

	if pkg == "" {
		return t.String()
	}

	return pkg + "." + t.Name()
}
