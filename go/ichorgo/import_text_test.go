package ichorgo

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeImportText(t *testing.T) {
	yaml := singleTokenKubeconfig("https://a.example.org:6443", "t")

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(yaml))
	_ = zw.Close()

	encoded := base64.URLEncoding.EncodeToString(buf.Bytes())

	for name, text := range map[string]string{
		"plain":      yaml,
		"compressed": importTextPrefix + encoded,
		"wrapped":    "  " + importTextPrefix + encoded[:10] + "\n" + encoded[10:] + "\n",
	} {
		got, err := DecodeImportText(text)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if got != yaml {
			t.Errorf("%s: got %q", name, got)
		}
	}

	for _, bad := range []string{importTextPrefix + "!!!", importTextPrefix + base64.RawURLEncoding.EncodeToString([]byte("not gzip"))} {
		if _, err := DecodeImportText(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}

	// A bomb: far more than the limit once expanded.
	buf.Reset()
	zw = gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(strings.Repeat("a", importTextMax+10)))
	_ = zw.Close()

	if _, err := DecodeImportText(importTextPrefix + base64.RawURLEncoding.EncodeToString(buf.Bytes())); err == nil {
		t.Error("oversized payload accepted")
	}
}
