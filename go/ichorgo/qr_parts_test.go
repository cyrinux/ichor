package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// splitParts cuts the base64url of gz into n part texts, as scripts/qr-parts.py does.
func splitParts(gz []byte, n int) []string {
	encoded := base64.RawURLEncoding.EncodeToString(gz)
	size := (len(encoded) + n - 1) / n

	out := make([]string, 0, n)

	for i := range n {
		end := min((i+1)*size, len(encoded))
		out = append(out, fmt.Sprintf("ichor:%d/%d:%s", i+1, n, encoded[i*size:end]))
	}

	return out
}

// partsJSON reads each text through QRPart and returns the JSON list QRPartsJoin takes.
func partsJSON(t *testing.T, texts ...string) string {
	t.Helper()

	list := make([]json.RawMessage, 0, len(texts))

	for _, text := range texts {
		part, err := QRPart(text)
		if err != nil || part == "" {
			t.Fatalf("QRPart(%.20q) = %q, %v", text, part, err)
		}

		list = append(list, json.RawMessage(part))
	}

	b, _ := json.Marshal(list)

	return string(b)
}

func TestQRPart(t *testing.T) {
	cases := map[string]struct {
		text, want, err string
	}{
		"part":          {"ichor:2/3:abc-_09", `{"index":2,"total":3,"chunk":"abc-_09"}`, ""},
		"trimmed":       {"  ichor:1/1:QUJD\n", `{"index":1,"total":1,"chunk":"QUJD"}`, ""},
		"plain yaml":    {"context: a\ncontexts: {}\n", "", ""},
		"import text":   {"ichor-config:H4sI", "", ""},
		"no numbers":    {"ichor:abc", "", ""},
		"half header":   {"ichor:1/3", "", ""},
		"index zero":    {"ichor:0/3:abc", "", QRPartsErrInvalid},
		"index past n":  {"ichor:4/3:abc", "", QRPartsErrInvalid},
		"too many":      {"ichor:1/17:abc", "", QRPartsErrInvalid},
		"huge number":   {"ichor:1/99999999999999999999:abc", "", QRPartsErrInvalid},
		"empty chunk":   {"ichor:1/3:", "", QRPartsErrInvalid},
		"padding":       {"ichor:1/3:QQ==", "", QRPartsErrInvalid},
		"bad character": {"ichor:1/3:ab+c", "", QRPartsErrInvalid},
		"long chunk":    {"ichor:1/3:" + strings.Repeat("A", qrPartChunkMax+1), "", QRPartsErrInvalid},
		"longest chunk": {"ichor:1/1:" + strings.Repeat("A", qrPartChunkMax), `{"index":1,"total":1,"chunk":"` + strings.Repeat("A", qrPartChunkMax) + `"}`, ""},
	}

	for name, c := range cases {
		got, err := QRPart(c.text)

		switch {
		case c.err != "" && (err == nil || !strings.HasPrefix(err.Error(), c.err+": ")):
			t.Errorf("%s: err = %v, want %s", name, err, c.err)
		case c.err == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case got != c.want:
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestQRPartsJoin(t *testing.T) {
	yaml := singleTokenKubeconfig("https://a.example.org:6443", "sample-token")
	gz := gzipped(t, yaml)
	parts := splitParts(gz, 3)
	other := splitParts(gzipped(t, singleTokenKubeconfig("https://b.example.org:6443", "other-token")), 3)
	notConfig := splitParts(gzipped(t, "just: some yaml\n"), 2)

	want, err := QRCodeText("", gz, 0)
	if err != nil {
		t.Fatal(err)
	}

	ok := map[string]string{
		"in order":           partsJSON(t, parts...),
		"reverse order":      partsJSON(t, parts[2], parts[1], parts[0]),
		"duplicate same":     partsJSON(t, parts[1], parts[0], parts[1], parts[2]),
		"one code, one part": partsJSON(t, splitParts(gz, 1)...),
	}

	for name, in := range ok {
		got, err := QRPartsJoin(in)
		if err != nil {
			t.Errorf("%s: %v", name, err)

			continue
		}

		if got != want {
			t.Errorf("%s: not the one-code import text", name)
		}

		if decoded, _ := DecodeImportText(got); decoded != yaml {
			t.Errorf("%s: decodes to another config", name)
		}
	}

	conflict := strings.Replace(parts[1], "ichor:2/3:", "ichor:2/3:A", 1)

	bad := map[string]struct{ in, code string }{
		"empty list":           {"[]", QRPartsErrMissing},
		"missing part":         {partsJSON(t, parts[0], parts[2]), QRPartsErrMissing},
		"totals differ":        {partsJSON(t, parts[0], parts[1], "ichor:3/4:QUJD"), QRPartsErrMismatch},
		"duplicate conflicts":  {partsJSON(t, parts[0], parts[1], conflict, parts[2]), QRPartsErrMismatch},
		"chunk from other set": {partsJSON(t, parts[0], other[1], parts[2]), QRPartsErrInvalid},
		"not a config":         {partsJSON(t, notConfig...), QRPartsErrInvalid},
		"not gzip":             {partsJSON(t, "ichor:1/1:"+base64.RawURLEncoding.EncodeToString([]byte("plain text"))), QRPartsErrInvalid},
		"not json":             {"ichor:1/1:abc", QRPartsErrInvalid},
		"too many parts":       {`[{"index":1,"total":17,"chunk":"QUJD"}]`, QRPartsErrInvalid},
		"index out of range":   {`[{"index":3,"total":2,"chunk":"QUJD"}]`, QRPartsErrInvalid},
		"oversized chunk":      {`[{"index":1,"total":1,"chunk":"` + strings.Repeat("A", qrPartChunkMax+1) + `"}]`, QRPartsErrInvalid},
		"bad characters":       {`[{"index":1,"total":1,"chunk":"QU/D"}]`, QRPartsErrInvalid},
	}

	for name, c := range bad {
		got, err := QRPartsJoin(c.in)
		if err == nil || !strings.HasPrefix(err.Error(), c.code+": ") {
			t.Errorf("%s: err = %v, want %s", name, err, c.code)
		}

		if got != "" {
			t.Errorf("%s: returned a result with the error", name)
		}

		if err != nil && strings.Contains(err.Error(), parts[0][len("ichor:1/3:"):][:8]) {
			t.Errorf("%s: error quotes a chunk", name)
		}
	}
}

// A zip bomb: the parts expand past importTextMax.
func TestQRPartsJoinTooLarge(t *testing.T) {
	gz := gzipped(t, strings.Repeat("a", importTextMax+1))

	_, err := QRPartsJoin(partsJSON(t, splitParts(gz, 2)...))
	if err == nil || !strings.HasPrefix(err.Error(), QRPartsErrInvalid+": ") {
		t.Fatalf("err = %v", err)
	}
}

// testdata/qr_parts/parts.txt is scripts/qr-parts.py's output for sample-config.yaml
// (--chunk 800); scripts/test_qr_parts.py checks the same files from the Python side.
func TestQRPartsJoinScriptOutput(t *testing.T) {
	config, err := os.ReadFile("testdata/qr_parts/sample-config.yaml")
	if err != nil {
		t.Fatal(err)
	}

	lines, err := os.ReadFile("testdata/qr_parts/parts.txt")
	if err != nil {
		t.Fatal(err)
	}

	texts := strings.Fields(string(lines))
	if len(texts) < 3 {
		t.Fatalf("%d parts", len(texts))
	}

	for i, j := 0, len(texts)-1; i < j; i, j = i+1, j-1 {
		texts[i], texts[j] = texts[j], texts[i]
	}

	joined, err := QRPartsJoin(partsJSON(t, texts...))
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeImportText(joined)
	if err != nil {
		t.Fatal(err)
	}

	if decoded != string(config) {
		t.Fatal("the parts decode to another config")
	}
}
