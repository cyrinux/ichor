package ichorgo

import (
	"bytes"
	"compress/gzip"
	"testing"
)

// qrBitstream lays out data codewords as an encoder does: optional ECI, one byte-mode
// segment, the terminator, then pad codewords.
func qrBitstream(payload []byte, countBits int, eci bool) []byte {
	var bits []byte

	put := func(v, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, byte(v>>i&1))
		}
	}

	if eci {
		put(0b0111, 4)
		put(26, 8) // UTF-8
	}

	put(0b0100, 4)
	put(len(payload), countBits)

	for _, b := range payload {
		put(int(b), 8)
	}

	put(0, 4)

	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}

	out := make([]byte, 0, len(bits)/8+3)

	for i := 0; i < len(bits); i += 8 {
		var b byte
		for _, bit := range bits[i : i+8] {
			b = b<<1 | bit
		}

		out = append(out, b)
	}

	return append(out, 0xec, 0x11, 0xec)
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write([]byte(s))

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func TestQRCodeText(t *testing.T) {
	yaml := singleTokenKubeconfig("https://a.example.org:6443", "t")
	gz := gzipped(t, yaml)

	cases := map[string]struct {
		raw     []byte
		version int
	}{
		"payload":            {gz, 0},
		"codewords v20":      {qrBitstream(gz, 16, false), 20},
		"codewords v5":       {qrBitstream(gz, 8, false), 5},
		"codewords unknown":  {qrBitstream(gz, 16, false), 0},
		"codewords with eci": {qrBitstream(gz, 16, true), 0},
	}

	for name, c := range cases {
		got, err := DecodeImportText(qrText(t, "� garbled", c.raw, c.version))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if got != yaml {
			t.Errorf("%s: got %q", name, got)
		}
	}

	// A truncated stream is still taken as gzip: the import then reports it.
	if _, err := DecodeImportText(qrText(t, "x", gz[:len(gz)/2], 0)); err == nil {
		t.Error("truncated gzip accepted")
	}
}

func TestQRCodeTextKeepsText(t *testing.T) {
	yaml := "context: a\ncontexts: {}\n"

	for name, raw := range map[string][]byte{
		"no bytes":   nil,
		"plain text": qrBitstream([]byte(yaml), 16, false),
		"numeric":    {0b0001_0000, 0b0000_0100, 0x00},
		"garbage":    {0xff, 0xff, 0xff},
	} {
		if got := qrText(t, yaml, raw, 0); got != yaml {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func qrText(t *testing.T, text string, raw []byte, version int) string {
	t.Helper()

	out, err := QRCodeText(text, raw, version)
	if err != nil {
		t.Fatal(err)
	}

	return out
}
