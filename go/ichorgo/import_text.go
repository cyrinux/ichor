package ichorgo

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// A QR code holds about 2.9 KB: a kubeconfig with an embedded client certificate does not
// fit as text. Compressed, it usually does:
//
//	printf 'ichor-config:%s' "$(gzip -9c ~/.kube/config | basenc --base64url -w0)" | qrencode -o config.png
//
// The prefix works for a talosconfig as well.
const importTextPrefix = "ichor-config:"

// importTextMax bounds what a compressed payload may expand to.
const importTextMax = 1 << 20

// DecodeImportText returns the config a pasted, scanned or opened text holds: the text
// itself, or the YAML of an "ichor-config:" payload (base64url of gzip, padding optional).
func DecodeImportText(text string) (out string, err error) {
	// The result is a credential the user imports: only the error is masked.
	defer maskErr(&err)

	trimmed := strings.TrimSpace(text)

	payload, ok := strings.CutPrefix(trimmed, importTextPrefix)
	if !ok {
		return text, nil
	}

	payload = strings.Join(strings.Fields(payload), "")

	compressed, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(payload, "="))
	if err != nil {
		return "", fmt.Errorf("compressed config: %w", err)
	}

	r, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", fmt.Errorf("compressed config: %w", err)
	}

	plain, err := io.ReadAll(io.LimitReader(r, importTextMax+1))
	if err != nil {
		return "", fmt.Errorf("compressed config: %w", err)
	}

	if len(plain) > importTextMax {
		return "", errors.New("compressed config is too large")
	}

	return string(plain), nil
}
