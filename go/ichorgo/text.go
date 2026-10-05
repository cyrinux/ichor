package ichorgo

import (
	"strings"
	"unicode/utf8"
)

// clipUTF8 cuts s to at most limit bytes without splitting a character, marking the cut with
// an ellipsis. Invalid UTF-8 (a remote body, a raw packet field) is repaired first.
func clipUTF8(s string, limit int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if len(s) <= limit {
		return s
	}

	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}

	return s[:limit] + "…"
}
