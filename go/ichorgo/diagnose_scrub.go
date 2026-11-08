package ichorgo

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// A log line or an event can quote a credential. The diagnosis strips what looks like one
// from its report in both modes, real names or anonymized, and for the phone's own model too
// (it is cheap). It runs on the whole report, so a message from Argo CD or a health check
// goes through it as well as the log tails and the events.
//
// The audit log and the diagnostics ring have redactSecrets, which keeps hex digests for
// them to stay useful. A report leaving the phone errs the other way.

const (
	scrubbedSecret = "[redacted]"
	// A path of names has shorter words than an encoded run, even one split by a "/" or two.
	encodedRunMinLen  = 40
	encodedWordMinLen = 16
	encodedMinDigits  = 2
	// A bearer value of letters alone, under this, is prose: "bearer token expired".
	bearerProseMaxLen = 20
)

var (
	pemBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?-----END [A-Z0-9 ]+-----`)
	// A block whose end was cut off: its body falls to encodedRun.
	pemHeader = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+-----`)

	authHeader = regexp.MustCompile(`(?i)(\b(?:proxy-)?authorization["']?\s*[:=]\s*["']?)(?:[a-z]+\s+)?[^\s"',;]+`)
	// token=abc, auth_token="abc", "password":"abc". A bare colon stays: "token: expired" is prose.
	secretValue = regexp.MustCompile(`(?i)([a-z0-9_.-]*(?:token|password|passwd|secret|api[_-]?key)["']?\s*(?:=\s*["']?|:\s*["']))(?:bearer\s+)?[^\s"'&,;]+`)
	bearerToken = regexp.MustCompile(`(?i)(\bbearer\s+)([A-Za-z0-9._~+/=-]+)`)

	secretLiterals = []*regexp.Regexp{
		regexp.MustCompile(`\beyJ[\w-]+\.[\w-]+\.[\w-]+`),  // JWT
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),         // AWS access key
		regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}`), // GitHub token
	}

	encodedRun = regexp.MustCompile(`[A-Za-z0-9+/=_-]{40,}`)

	// Digests and container ids are long hex by nature and hide nothing.
	encodedRunExempt = []string{"sha256:", "containerd:", "cri-o:", "docker:"}
)

// scrubSecrets replaces what looks like a secret in s with [redacted].
func scrubSecrets(s string) string {
	s = pemBlock.ReplaceAllString(s, scrubbedSecret)
	s = pemHeader.ReplaceAllString(s, scrubbedSecret)
	s = authHeader.ReplaceAllString(s, "${1}"+scrubbedSecret)
	s = secretValue.ReplaceAllString(s, "${1}"+scrubbedSecret)
	s = bearerToken.ReplaceAllStringFunc(s, scrubBearer)

	for _, literal := range secretLiterals {
		s = literal.ReplaceAllString(s, scrubbedSecret)
	}

	return scrubEncodedRuns(s)
}

func scrubBearer(match string) string {
	parts := bearerToken.FindStringSubmatch(match)

	value := parts[2]
	if len(value) < bearerProseMaxLen && strings.IndexFunc(value, func(r rune) bool { return !unicode.IsLetter(r) }) < 0 {
		return match
	}

	return parts[1] + scrubbedSecret
}

func scrubEncodedRuns(s string) string {
	var b strings.Builder

	last := 0

	for _, loc := range encodedRun.FindAllStringIndex(s, -1) {
		before := s[:loc[0]]
		exempt := slices.ContainsFunc(encodedRunExempt, func(prefix string) bool { return strings.HasSuffix(before, prefix) })

		if exempt || !looksLikeEncodedSecret(s[loc[0]:loc[1]]) {
			continue
		}

		b.WriteString(s[last:loc[0]])
		b.WriteString(scrubbedSecret)
		last = loc[1]
	}

	b.WriteString(s[last:])

	return b.String()
}

// looksLikeEncodedSecret tells long hex or base64 from a long path of names or a pod name
// with its hashes: base64 mixes cases and digits in long words.
func looksLikeEncodedSecret(run string) bool {
	longest := ""

	for _, w := range strings.FieldsFunc(run, func(r rune) bool { return strings.ContainsRune("/-_+=", r) }) {
		if len(w) > len(longest) {
			longest = w
		}
	}

	digits := 0

	for _, r := range run {
		if unicode.IsDigit(r) {
			digits++
		}
	}

	if digits < encodedMinDigits {
		return false // "aaaa…" is filler, and anything encoded of this length holds digits
	}

	isHex := strings.IndexFunc(longest, func(r rune) bool { return !unicode.Is(unicode.ASCII_Hex_Digit, r) }) < 0
	if isHex && len(longest) >= encodedRunMinLen {
		return true
	}

	return len(longest) >= encodedWordMinLen && strings.ToLower(run) != run && strings.ToUpper(run) != run
}

// scrubFooter is the report's last line, "" when nothing was stripped between raw and clean.
func scrubFooter(raw, clean string) string {
	n := strings.Count(clean, scrubbedSecret) - strings.Count(raw, scrubbedSecret)
	if n <= 0 {
		return ""
	}

	return fmt.Sprintf("\n%d secrets-like strings redacted\n", n)
}
