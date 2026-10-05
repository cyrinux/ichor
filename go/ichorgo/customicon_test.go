package ichorgo

import (
	"encoding/base64"
	"strings"
	"testing"
)

// A 1×1 transparent PNG.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="

func TestCustomIcon(t *testing.T) {
	big := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, customIconMaxBytes+1))

	tests := []struct {
		name        string
		annotations map[string]string
		labels      map[string]string
		want        customIconRef
		wantOK      bool
	}{
		{name: "none"},
		{name: "bundled slug", annotations: map[string]string{customIconKey: "grafana"}, want: customIconRef{icon: "grafana"}, wantOK: true},
		{name: "slug is lowercased and trimmed", annotations: map[string]string{customIconKey: " Grafana "}, want: customIconRef{icon: "grafana"}, wantOK: true},
		{name: "unbundled slug", annotations: map[string]string{customIconKey: "my-homelab-thing"}, want: customIconRef{remote: "my-homelab-thing"}, wantOK: true},
		{name: "label slug", labels: map[string]string{customIconKey: "grafana"}, want: customIconRef{icon: "grafana"}, wantOK: true},
		{
			name:        "annotation wins over label",
			annotations: map[string]string{customIconKey: "https://example.org/a.png"},
			labels:      map[string]string{customIconKey: "grafana"},
			want:        customIconRef{url: "https://example.org/a.png"},
			wantOK:      true,
		},
		{name: "https url", annotations: map[string]string{customIconKey: "https://git.example.org/logo.webp?v=2"}, want: customIconRef{url: "https://git.example.org/logo.webp?v=2"}, wantOK: true},
		{name: "plain http refused", annotations: map[string]string{customIconKey: "http://example.org/a.png"}},
		{name: "credentials refused", annotations: map[string]string{customIconKey: "https://user:pw@example.org/a.png"}},
		{name: "no host refused", annotations: map[string]string{customIconKey: "https:///a.png"}},
		{name: "data uri", annotations: map[string]string{customIconKey: "data:image/png;base64," + tinyPNG}, want: customIconRef{url: "data:image/png;base64," + tinyPNG}, wantOK: true},
		{
			name:        "data uri whitespace from a folded YAML block is dropped",
			annotations: map[string]string{customIconKey: "data:image/png;base64," + tinyPNG[:20] + "\n  " + tinyPNG[20:]},
			want:        customIconRef{url: "data:image/png;base64," + tinyPNG},
			wantOK:      true,
		},
		{name: "svg refused (neither app decodes it)", annotations: map[string]string{customIconKey: "data:image/svg+xml;base64,PHN2Zy8+"}},
		{name: "not base64 refused", annotations: map[string]string{customIconKey: "data:image/png;base64,***"}},
		{name: "too big refused", annotations: map[string]string{customIconKey: big}},
		{name: "garbage refused", annotations: map[string]string{customIconKey: "../../etc/passwd"}},
		{name: "invalid annotation does not fall back to the label", annotations: map[string]string{customIconKey: "nope!"}, labels: map[string]string{customIconKey: "grafana"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := customIcon(tt.annotations, tt.labels)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("customIcon() = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestMapArgoAppCustomIcon(t *testing.T) {
	o := argoObjectByName(t, "web")
	o.Metadata.Annotations = map[string]string{customIconKey: "https://example.org/web.png"}

	a := mapArgoApp(o, map[string]bool{})
	if a.Icon != "" || a.RemoteIcon != "" || a.IconURL != "https://example.org/web.png" {
		t.Fatalf("icon = %q, remote = %q, url = %q", a.Icon, a.RemoteIcon, a.IconURL)
	}
}

func TestMapArgoAppInvalidCustomIconKeepsCatalog(t *testing.T) {
	plain := mapArgoApp(argoObjectByName(t, "cilium"), map[string]bool{})

	o := argoObjectByName(t, "cilium")
	o.Metadata.Annotations = map[string]string{customIconKey: "data:text/html;base64," + strings.Repeat("A", 8)}

	a := mapArgoApp(o, map[string]bool{})
	if a.Icon != plain.Icon || a.RemoteIcon != plain.RemoteIcon || a.IconURL != "" {
		t.Fatalf("icon = %q, remote = %q, url = %q; want the catalog's %q", a.Icon, a.RemoteIcon, a.IconURL, plain.Icon)
	}
}
