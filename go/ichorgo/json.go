package ichorgo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}

	return string(b), nil
}

// emitJSON sends v as JSON to a listener's callback; a value that cannot be encoded (none
// of the progress types can fail) is dropped rather than sent broken.
func emitJSON(v any, send func(string)) {
	if js, err := toJSON(v); err == nil {
		send(js)
	}
}

// splitCSV splits a comma-separated list, trimmed, blanks dropped, first occurrence kept.
func splitCSV(s string) []string {
	var out []string

	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}

	return out
}

// errText is err's message, "" for nil: the error field of a progress event.
func errText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
