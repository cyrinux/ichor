package ichorgo

import (
	"encoding/json"
	"fmt"
)

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode result: %w", err)
	}

	return string(b), nil
}

// errText is err's message, "" for nil: the error field of a progress event.
func errText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
