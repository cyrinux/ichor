package ichorgo

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Text scanners used by the privacy mask. They only find and replace; the mapping itself
// lives in privacyMask.

var ipv4Candidate = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)

// ipv6Candidate over-matches (times, MACs, "host:port"); candidates are validated by netip.
var ipv6Candidate = regexp.MustCompile(`[0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*`)

// isWordByte treats letters, digits and any non-ASCII byte as part of a word.
func isWordByte(c byte) bool {
	return c >= 0x80 || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// replaceIPv4 calls repl for every standalone dotted-quad in s: four octets of 0-255
// without leading zeros, not glued to letters, digits or more dotted numbers, so versions
// ("v1.14.1", "1.2.3.4.5") and identifiers are left alone.
func replaceIPv4(s string, repl func(ip string) (string, bool)) string {
	if strings.Count(s, ".") < 3 {
		return s
	}

	var b strings.Builder

	last := 0

	for _, loc := range ipv4Candidate.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]

		if start > 0 && (isWordByte(s[start-1]) || s[start-1] == '.') {
			continue
		}

		if end < len(s) && (isWordByte(s[end]) || s[end] == '.' && end+1 < len(s) && isDigit(s[end+1])) {
			continue
		}

		ip := s[start:end]
		if !validOctets(ip) {
			continue
		}

		r, ok := repl(ip)
		if !ok {
			continue
		}

		b.WriteString(s[last:start])
		b.WriteString(r)
		last = end
	}

	if last == 0 {
		return s
	}

	b.WriteString(s[last:])

	return b.String()
}

func validOctets(ip string) bool {
	for octet := range strings.SplitSeq(ip, ".") {
		if len(octet) > 1 && octet[0] == '0' {
			return false
		}

		n := 0
		for i := range len(octet) {
			n = n*10 + int(octet[i]-'0')
		}

		if n > 255 {
			return false
		}
	}

	return true
}

// replaceIPv6 calls repl for every standalone IPv6 address in s. repl decides (parsing
// the candidate) whether it is an address; a candidate glued to a word is retried from
// each of its colons, so "inet6:2001:db8::1" still yields "2001:db8::1".
func replaceIPv6(s string, repl func(addr string) (string, bool)) string {
	if !strings.Contains(s, ":") {
		return s
	}

	var b strings.Builder

	last := 0

	for _, loc := range ipv6Candidate.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]

		// Trailing punctuation ("addr: fe80::1." or "fd00::1:") is not part of the address.
		for end > start+2 && (s[end-1] == '.' || s[end-1] == ':' && s[end-2] != ':') {
			end--
		}

		if end < len(s) && isWordByte(s[end]) {
			continue
		}

		for from := start; from < end; {
			if from == 0 || !isWordByte(s[from-1]) {
				if r, ok := repl(s[from:end]); ok {
					b.WriteString(s[last:from])
					b.WriteString(r)
					last = end

					break
				}
			}

			next := strings.IndexByte(s[from:end], ':')
			if next < 0 {
				break
			}

			from += next + 1
		}
	}

	if last == 0 {
		return s
	}

	b.WriteString(s[last:])

	return b.String()
}

// maskTerm is a literal (hostname, domain, context, user word) replaced as a whole word,
// case-insensitively.
type maskTerm struct {
	match      string // lowercase
	repl       string
	suffixOnly bool // single-label domain: only after a dot ("host.lan"), or as the whole value
}

// replaceTerms replaces whole-word occurrences of terms (sorted longest first) in a single
// pass, so a replacement is never matched again. Word characters are letters and digits:
// "-", "." and "_" separate words, so "cyril-app" matches the term "cyril".
func replaceTerms(s string, terms []maskTerm) string {
	if len(terms) == 0 || s == "" {
		return s
	}

	var b strings.Builder

	last := 0

	for i := 0; i < len(s); {
		if i > 0 && isWordByte(s[i-1]) {
			i++

			continue
		}

		t, ok := matchTerm(s, i, terms)
		if !ok {
			i++

			continue
		}

		b.WriteString(s[last:i])
		b.WriteString(t.repl)
		i += len(t.match)
		last = i
	}

	if last == 0 {
		return s
	}

	b.WriteString(s[last:])

	return b.String()
}

func matchTerm(s string, i int, terms []maskTerm) (maskTerm, bool) {
	first := lowerByte(s[i])

	for _, t := range terms {
		end := i + len(t.match)
		if t.match[0] != first || end > len(s) || !strings.EqualFold(s[i:end], t.match) {
			continue
		}

		if end < len(s) && isWordByte(s[end]) {
			continue
		}

		whole := i == 0 && end == len(s)
		if t.suffixOnly && !whole && (i == 0 || s[i-1] != '.') {
			continue
		}

		return t, true
	}

	return maskTerm{}, false
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}

	return c
}

// maskJSONStrings applies mask to every string value of a valid JSON document, leaving
// keys, numbers and layout untouched. Re-encoded values stay valid JSON strings.
func maskJSONStrings(doc string, mask func(string) string) string {
	var b strings.Builder

	last := 0

	for i := 0; i < len(doc); i++ {
		if doc[i] != '"' {
			continue
		}

		end := jsonStringEnd(doc, i)
		if end < 0 {
			break
		}

		if !isJSONKey(doc, end+1) {
			var v string
			if err := json.Unmarshal([]byte(doc[i:end+1]), &v); err == nil {
				if masked := mask(v); masked != v {
					if enc, err := json.Marshal(masked); err == nil {
						b.WriteString(doc[last:i])
						b.Write(enc)
						last = end + 1
					}
				}
			}
		}

		i = end
	}

	if last == 0 {
		return doc
	}

	b.WriteString(doc[last:])

	return b.String()
}

// jsonStringEnd returns the index of the quote closing the string opened at start.
func jsonStringEnd(doc string, start int) int {
	for j := start + 1; j < len(doc); j++ {
		switch doc[j] {
		case '\\':
			j++
		case '"':
			return j
		}
	}

	return -1
}

func isJSONKey(doc string, after int) bool {
	for ; after < len(doc); after++ {
		switch doc[after] {
		case ' ', '\t', '\n', '\r':
			continue
		case ':':
			return true
		default:
			return false
		}
	}

	return false
}
