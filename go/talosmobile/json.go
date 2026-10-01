package talosmobile

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
