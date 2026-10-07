package ichorgo

import (
	"bytes"
	"encoding/base64"
)

// A config can also go into a QR code as raw gzip, in byte mode:
//
//	gzip -9 < talosconfig-phone | qrencode -8 -t ansiutf8
//
// Scanners decode a QR code to text, which mangles binary: the app hands the code's bytes
// here as well, and a gzip payload among them becomes the "ichor-config:" form
// DecodeImportText expands.

var gzipMagic = []byte{0x1f, 0x8b}

// QRCodeText returns the text to import from a scanned QR code: text is the scanner's
// decoded string, raw the code's bytes (its payload, or its data codewords as read off
// the symbol) and version the symbol version (0 when unknown). A gzip payload in raw
// becomes an "ichor-config:" text; otherwise text is returned as is.
func QRCodeText(text string, raw []byte, version int) (out string, err error) {
	// The result is a credential the user imports: only the error is masked.
	defer maskErr(&err)

	payload := qrGzipPayload(raw, version)
	if payload == nil {
		return text, nil
	}

	return importTextPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

// qrGzipPayload finds a gzip stream in raw: raw itself, or the byte-mode data of a QR
// bitstream. Nil when there is none.
func qrGzipPayload(raw []byte, version int) []byte {
	if bytes.HasPrefix(raw, gzipMagic) {
		return raw
	}

	classes := []int{0, 1, 2}
	if version > 0 {
		classes = []int{qrVersionClass(version)}
	}

	for _, class := range classes {
		if data, ok := qrByteSegments(raw, class); ok && bytes.HasPrefix(data, gzipMagic) {
			return data
		}
	}

	return nil
}

// qrVersionClass groups the versions sharing character-count widths: 1-9, 10-26, 27-40.
func qrVersionClass(version int) int {
	switch {
	case version <= 9:
		return 0
	case version <= 26:
		return 1
	default:
		return 2
	}
}

// Byte-mode character-count width per version class (ISO/IEC 18004, table 3).
var qrByteCount = [3]int{8, 16, 16}

// qrByteSegments parses the data codewords of a QR symbol and returns its byte-mode
// data. Not ok when the bitstream does not parse or holds text in another mode.
func qrByteSegments(codewords []byte, class int) ([]byte, bool) {
	r := bitReader{data: codewords}

	var out []byte

	for r.left() >= 4 {
		switch mode := r.read(4); mode {
		case 0b0000: // terminator
			return out, len(out) > 0
		case 0b0100: // byte
			n := r.read(qrByteCount[class])
			if n < 0 || r.left() < n*8 {
				return nil, false
			}

			for range n {
				out = append(out, byte(r.read(8)))
			}
		case 0b0111: // ECI: the designator is 1 to 3 bytes, told by its leading bits
			if !r.skipECI() {
				return nil, false
			}
		case 0b0011: // structured append: sequence and parity
			if !r.skip(16) {
				return nil, false
			}
		case 0b0101, 0b1001: // FNC1
			if mode == 0b1001 && !r.skip(8) {
				return nil, false
			}
		default: // numeric, alphanumeric, kanji or invalid: not a binary payload
			return nil, false
		}
	}

	return out, len(out) > 0
}

type bitReader struct {
	data []byte
	pos  int // in bits
}

func (r *bitReader) left() int { return len(r.data)*8 - r.pos }

// read returns the next n bits, or -1 past the end.
func (r *bitReader) read(n int) int {
	if n > r.left() {
		return -1
	}

	v := 0

	for range n {
		bit := r.data[r.pos/8] >> (7 - r.pos%8) & 1
		v = v<<1 | int(bit)
		r.pos++
	}

	return v
}

func (r *bitReader) skip(n int) bool { return r.read(n) >= 0 }

func (r *bitReader) skipECI() bool {
	first := r.read(8)

	switch {
	case first < 0:
		return false
	case first&0x80 == 0:
		return true
	case first&0xc0 == 0x80:
		return r.skip(8)
	case first&0xe0 == 0xc0:
		return r.skip(16)
	default:
		return false
	}
}
