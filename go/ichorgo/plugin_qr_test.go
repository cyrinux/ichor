package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

// A scanned code is a plugin link (fetched, then checked) or the plugin file itself (checked);
// anything else is refused before anything is fetched or stored.
func TestPluginQRPayload(t *testing.T) {
	yaml := kubeGreenCopy(t)

	cases := []struct {
		name, text, kind, value, err string
	}{
		{"link", "ichor://plugin?url=https%3A%2F%2Fraw.githubusercontent.com%2Fexample%2Fplugins%2Fmain%2Fwidgets.yaml",
			"url", "https://raw.githubusercontent.com/example/plugins/main/widgets.yaml", ""},
		{"link unescaped, spaces around", "  ichor://plugin?url=https://example.org/p.yaml \n", "url", "https://example.org/p.yaml", ""},
		{"file", yaml, "yaml", yaml, ""},
		{"file without the schema comment", "schemaVersion: ichor.dev/plugin/v1alpha1\nid: x\n", "yaml", "schemaVersion: ichor.dev/plugin/v1alpha1\nid: x\n", ""},
		{"http link", "ichor://plugin?url=http://example.org/p.yaml", "", "", "https"},
		{"file link", "ichor://plugin?url=file:///etc/passwd", "", "", "https"},
		{"link without url", "ichor://plugin?id=x", "", "", "https"},
		{"other ichor link", "ichor://cluster?name=x", "", "", "not a plugin"},
		{"a talosconfig", "context: prod\ncontexts:\n  prod: {}\n", "", "", "not a plugin"},
		{"a website", "https://example.org", "", "", "not a plugin"},
		{"nothing", "   ", "", "", "not a plugin"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := PluginQRPayload(c.text)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("got %s, %v; want an error with %q", out, err, c.err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var got map[string]string
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}

			if got["kind"] != c.kind || got[c.kind] != c.value {
				t.Fatalf("got %v, want %s %q", got, c.kind, c.value)
			}
		})
	}
}
