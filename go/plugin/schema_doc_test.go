package plugin

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// schemaKeys are the YAML keys of every type of schema.go reachable from Plugin.
func schemaKeys(t reflect.Type, seen map[reflect.Type]bool, out []string) []string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct || seen[t] || t.PkgPath() != reflect.TypeFor[Plugin]().PkgPath() {
		return out
	}

	seen[t] = true

	for i := range t.NumField() {
		f := t.Field(i)

		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}

		if !slices.Contains(out, name) {
			out = append(out, name)
		}

		out = schemaKeys(f.Type, seen, out)
	}

	return out
}

// docs/plugins.md is the reference of the format: every key of schema.go is in it, written
// as code.
func TestDocsNameEverySchemaKey(t *testing.T) {
	doc, err := os.ReadFile("../../docs/plugins.md")
	if err != nil {
		t.Fatal(err)
	}

	keys := schemaKeys(reflect.TypeFor[Plugin](), map[reflect.Type]bool{}, nil)
	if len(keys) < 60 {
		t.Fatalf("only %d keys found: %v", len(keys), keys)
	}

	for _, key := range keys {
		inCode := regexp.MustCompile("`[^`\n]*\\b" + regexp.QuoteMeta(key) + "\\b[^`\n]*`")
		if !inCode.Match(doc) {
			t.Errorf("docs/plugins.md does not name the key %q (in `code`)", key)
		}
	}
}
