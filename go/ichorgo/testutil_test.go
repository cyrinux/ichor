package ichorgo

import (
	"encoding/json"
	"testing"
)

// equalJSON compares two values by their JSON encoding (what the UI actually sees).
func equalJSON(t *testing.T, got, want any) bool {
	t.Helper()

	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}

	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}

	if string(g) != string(w) {
		t.Logf("got  %s\nwant %s", g, w)

		return false
	}

	return true
}
