package plugin

import (
	"fmt"
	"strings"
	"testing"
)

func indexJSON(entry string) []byte {
	return fmt.Appendf(nil, `{"schemaVersion":%q,"plugins":[%s]}`, IndexSchemaVersion, entry)
}

func TestParseIndexKeepsAValidEntry(t *testing.T) {
	digest := strings.Repeat("a", 64)

	got, err := ParseIndex(indexJSON(fmt.Sprintf(`{"id":"demo","name":"Demo","url":"https://example.com/a.yaml","sha256":%q,
		"website":"https://example.com","icon":"demo-icon","futureKey":true}`, digest)))
	if err != nil {
		t.Fatal(err)
	}

	e := got.Plugins[0]
	if e.ID != "demo" || e.SHA256 != digest || e.Website != "https://example.com" || e.Icon != "demo-icon" || e.Resources == nil {
		t.Fatalf("entry: %+v", e)
	}
}

func TestParseIndexRefuses(t *testing.T) {
	digest := strings.Repeat("a", 64)
	entry := func(id, url, sha string) []byte {
		return indexJSON(fmt.Sprintf(`{"id":%q,"name":"x","url":%q,"sha256":%q}`, id, url, sha))
	}
	two := fmt.Sprintf(`{"id":"a","name":"x","url":"https://example.com/a","sha256":%q}`, digest)

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"not JSON", []byte("<html>"), "not a plugin index"},
		{"other format", []byte(`{"schemaVersion":"other","plugins":[]}`), "format"},
		{"bad id", entry("A B", "https://example.com/a", digest), "id"},
		{"http url", entry("a", "http://example.com/a", digest), "https"},
		{"no host", entry("a", "https://", digest), "https"},
		{"bad digest", entry("a", "https://example.com/a", "xyz"), "sha256"},
		{"same id twice", indexJSON(two + "," + two), "twice"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseIndex(tc.data); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNewIndexEntryNamesWhatThePluginReads(t *testing.T) {
	src := []byte(strings.Replace(string(mustBuiltinFile(t)), "id: kube-green\n", "id: demo\ndescription: Sleep\nmaintainer: someone\n", 1))

	c, err := CompileSource("demo.yaml", src)
	if err != nil {
		t.Fatal(err)
	}

	e := NewIndexEntry(c, src, "https://example.com/demo.yaml", "fallback")
	if e.ID != "demo" || e.Description != "Sleep" || e.Maintainer != "someone" || e.SHA256 != SHA256Hex(src) || e.Version != SchemaVersion {
		t.Fatalf("entry: %+v", e)
	}

	if !strings.Contains(strings.Join(e.Resources, " "), "sleepinfos.kube-green.com") {
		t.Fatalf("resources: %v", e.Resources)
	}

	bare := []byte(strings.Replace(string(mustBuiltinFile(t)), "id: kube-green\n", "id: demo\n", 1))

	c, err = CompileSource("demo.yaml", bare)
	if err != nil {
		t.Fatal(err)
	}

	if got := NewIndexEntry(c, bare, "https://example.com/demo.yaml", "fallback").Maintainer; got != "fallback" {
		t.Fatalf("maintainer %q", got)
	}
}

func mustBuiltinFile(t *testing.T) []byte {
	t.Helper()

	src, err := builtinFS.ReadFile("builtin/kube-green.yaml")
	if err != nil {
		t.Fatal(err)
	}

	return src
}
