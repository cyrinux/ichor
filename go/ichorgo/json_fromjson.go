package ichorgo

import (
	"encoding/json"
	"fmt"
)

// fromJSON reads a listener/spec payload; a call that hands malformed JSON gets a plain
// error rather than a panic (gomobile crosses the boundary as strings).
func fromJSON(text string, v any) error {
	if err := json.Unmarshal([]byte(text), v); err != nil {
		return fmt.Errorf("read the arguments: %w", err)
	}

	return nil
}
