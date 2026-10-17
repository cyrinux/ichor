package ichorgo

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// A config too large for one QR code, even gzipped, goes over several (scripts/qr-parts.py):
//
//	ichor:<i>/<n>:<chunk>
//
// i is 1-based, n at most qrPartsMax, and the chunks, in order, are the base64url (no
// padding) of the gzip of the config. The apps collect the parts in any order (QRPart),
// then QRPartsJoin turns them into the "ichor-config:" text a one-code scan gives.

// Error codes the apps map to a message: each error starts with one, then ": ".
const (
	QRPartsErrInvalid  = "qr-parts-invalid"
	QRPartsErrMismatch = "qr-parts-mismatch"
	QRPartsErrMissing  = "qr-parts-missing"
)

const (
	qrPartsMax     = 16   // parts in a set
	qrPartChunkMax = 2048 // base64url characters in one part
)

// qrPartHeader matches what is meant as a part; anything else is not a part at all.
var qrPartHeader = regexp.MustCompile(`^ichor:(\d+)/(\d+):`)

type qrPart struct {
	Index int    `json:"index"`
	Total int    `json:"total"`
	Chunk string `json:"chunk"`
}

// QRPart reads a scanned text as one part of a multi-code config: its JSON
// {"index":i,"total":n,"chunk":"…"}, or "" when the text is not a part (the scanner then
// imports it as before). A part with bad numbers or a bad chunk is a qr-parts-invalid error.
func QRPart(text string) (out string, err error) {
	// The chunk is a piece of a credential: only the error is masked.
	defer maskErr(&err)

	trimmed := strings.TrimSpace(text)

	m := qrPartHeader.FindStringSubmatch(trimmed)
	if m == nil {
		return "", nil
	}

	part := qrPart{Chunk: trimmed[len(m[0]):]}

	part.Index, err = strconv.Atoi(m[1])
	if err != nil {
		return "", qrPartsError(QRPartsErrInvalid, "part number out of range")
	}

	part.Total, err = strconv.Atoi(m[2])
	if err != nil {
		return "", qrPartsError(QRPartsErrInvalid, "part count out of range")
	}

	if err := part.check(); err != nil {
		return "", err
	}

	b, err := json.Marshal(part)
	if err != nil {
		return "", qrPartsError(QRPartsErrInvalid, "encode part")
	}

	return string(b), nil
}

// QRPartsJoin turns a complete set of parts (a JSON array of QRPart results, any order,
// duplicates allowed) into the "ichor-config:" text QRCodeText returns for a one-code gzip
// scan of the same config.
func QRPartsJoin(partsJSON string) (out string, err error) {
	// The result is a credential the user imports: only the error is masked.
	defer maskErr(&err)

	var parts []qrPart
	if err := json.Unmarshal([]byte(partsJSON), &parts); err != nil {
		return "", qrPartsError(QRPartsErrInvalid, "parts are not a JSON list")
	}

	chunks, err := orderedChunks(parts)
	if err != nil {
		return "", err
	}

	gz, err := base64.RawURLEncoding.DecodeString(strings.Join(chunks, ""))
	if err != nil {
		return "", qrPartsError(QRPartsErrInvalid, "the codes do not hold base64url data")
	}

	if err := checkImportableGzip(gz); err != nil {
		return "", err
	}

	return importTextPrefix + base64.RawURLEncoding.EncodeToString(gz), nil
}

// orderedChunks checks the set is one, complete and consistent, and returns its chunks in
// order.
func orderedChunks(parts []qrPart) ([]string, error) {
	if len(parts) == 0 {
		return nil, qrPartsError(QRPartsErrMissing, "no part scanned")
	}

	for _, p := range parts {
		if err := p.check(); err != nil {
			return nil, err
		}
	}

	total := parts[0].Total
	chunks := make([]string, total)

	for _, p := range parts {
		if p.Total != total {
			return nil, qrPartsError(QRPartsErrMismatch, fmt.Sprintf("parts of a %d-code and a %d-code set", total, p.Total))
		}

		if prev := chunks[p.Index-1]; prev != "" && prev != p.Chunk {
			return nil, qrPartsError(QRPartsErrMismatch, fmt.Sprintf("two different codes numbered %d", p.Index))
		}

		chunks[p.Index-1] = p.Chunk
	}

	var missing []string

	for i, c := range chunks {
		if c == "" {
			missing = append(missing, strconv.Itoa(i+1))
		}
	}

	if len(missing) > 0 {
		return nil, qrPartsError(QRPartsErrMissing, fmt.Sprintf("%d of %d codes scanned, missing %s",
			total-len(missing), total, strings.Join(missing, ", ")))
	}

	return chunks, nil
}

// check applies the limits of the format; the error never quotes the chunk.
func (p qrPart) check() error {
	switch {
	case p.Total < 1 || p.Total > qrPartsMax:
		return qrPartsError(QRPartsErrInvalid, fmt.Sprintf("a set has 1 to %d codes", qrPartsMax))
	case p.Index < 1 || p.Index > p.Total:
		return qrPartsError(QRPartsErrInvalid, "part number out of range")
	case p.Chunk == "":
		return qrPartsError(QRPartsErrInvalid, "empty part")
	case len(p.Chunk) > qrPartChunkMax:
		return qrPartsError(QRPartsErrInvalid, fmt.Sprintf("part longer than %d characters", qrPartChunkMax))
	case !isBase64URL(p.Chunk):
		return qrPartsError(QRPartsErrInvalid, "part is not base64url")
	}

	return nil
}

func isBase64URL(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_'
	})
}

// checkImportableGzip gunzips gz (at most importTextMax bytes) and checks it is a
// talosconfig or a kubeconfig.
func checkImportableGzip(gz []byte) error {
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return qrPartsError(QRPartsErrInvalid, "the codes do not hold a compressed config")
	}

	plain, err := io.ReadAll(io.LimitReader(r, importTextMax+1))
	if err != nil {
		return qrPartsError(QRPartsErrInvalid, "the compressed config is damaged")
	}

	if len(plain) > importTextMax {
		return qrPartsError(QRPartsErrInvalid, "the config is too large")
	}

	if !isImportableConfig(string(plain)) {
		return qrPartsError(QRPartsErrInvalid, "the codes do not hold a talosconfig or kubeconfig")
	}

	return nil
}

// isImportableConfig: a talosconfig with a context, or a kubeconfig the import accepts.
func isImportableConfig(text string) bool {
	if cfg, err := parseTalosconfig(text); err == nil && len(cfg.Contexts) > 0 {
		return true
	}

	_, err := loadKubeconfigDoc(text)

	return err == nil
}

func qrPartsError(code, detail string) error {
	return errors.New(code + ": " + detail)
}
