package ichorgo

import (
	"encoding/json"
	"strconv"
	"strings"
)

// logPair is one key/value of a structured log line, in order of appearance. Nested JSON
// objects are flattened into dotted keys; num marks a bare JSON number (timestamps).
type logPair struct {
	k, v string
	num  bool
}

const maxJSONDepth = 16

// jsonPairs parses s, which must be exactly one JSON object (surrounding blanks allowed),
// into ordered pairs. ok is false on any syntax error. It only allocates for the pairs and
// for strings with escapes, so it stays cheap on long log tails.
func jsonPairs(s string) ([]logPair, bool) {
	p := jsonScanner{s: s}
	p.skipSpace()

	pairs, ok := p.object("", nil, 0)
	if !ok {
		return nil, false
	}

	p.skipSpace()

	return pairs, p.i == len(s)
}

type jsonScanner struct {
	s string
	i int
}

func (p *jsonScanner) skipSpace() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonScanner) eat(c byte) bool {
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++

		return true
	}

	return false
}

func (p *jsonScanner) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}

	return 0
}

// object appends the members of the object at p.i to pairs, keys prefixed with prefix.
func (p *jsonScanner) object(prefix string, pairs []logPair, depth int) ([]logPair, bool) {
	if depth > maxJSONDepth || !p.eat('{') {
		return pairs, false
	}

	p.skipSpace()

	if p.eat('}') {
		if prefix != "" {
			pairs = append(pairs, logPair{k: prefix, v: "{}"})
		}

		return pairs, true
	}

	for {
		p.skipSpace()

		key, ok := p.str()
		if !ok {
			return pairs, false
		}

		if prefix != "" {
			key = prefix + "." + key
		}

		p.skipSpace()

		if !p.eat(':') {
			return pairs, false
		}

		p.skipSpace()

		if p.peek() == '{' {
			pairs, ok = p.object(key, pairs, depth+1)
		} else {
			var v string

			var num bool

			v, num, ok = p.value()
			pairs = append(pairs, logPair{k: key, v: v, num: num})
		}

		if !ok {
			return pairs, false
		}

		p.skipSpace()

		switch {
		case p.eat(','):
			continue
		case p.eat('}'):
			return pairs, true
		default:
			return pairs, false
		}
	}
}

// value reads a non-object value: strings are decoded, arrays kept as their JSON text.
func (p *jsonScanner) value() (v string, num, ok bool) {
	switch c := p.peek(); {
	case c == '"':
		v, ok = p.str()

		return v, false, ok
	case c == '[':
		start := p.i
		ok = p.skipArray()

		return p.s[start:p.i], false, ok
	case c == 't' || c == 'f' || c == 'n':
		for _, lit := range []string{"true", "false", "null"} {
			if strings.HasPrefix(p.s[p.i:], lit) {
				p.i += len(lit)

				return lit, false, true
			}
		}

		return "", false, false
	default:
		start := p.i
		for p.i < len(p.s) && strings.IndexByte("+-.eE0123456789", p.s[p.i]) >= 0 {
			p.i++
		}

		return p.s[start:p.i], true, p.i > start
	}
}

// str reads a JSON string; only strings with escapes go through encoding/json.
func (p *jsonScanner) str() (string, bool) {
	if !p.eat('"') {
		return "", false
	}

	start := p.i
	escaped := false

	for ; p.i < len(p.s); p.i++ {
		switch p.s[p.i] {
		case '\\':
			escaped = true
			p.i++
		case '"':
			raw := p.s[start:p.i]
			p.i++

			if !escaped {
				return raw, true
			}

			var v string
			if err := json.Unmarshal([]byte(p.s[start-1:p.i]), &v); err != nil {
				return "", false
			}

			return v, true
		}
	}

	return "", false
}

// skipArray moves past the array at p.i (nested arrays and objects included).
func (p *jsonScanner) skipArray() bool {
	depth := 0

	for ; p.i < len(p.s); p.i++ {
		switch p.s[p.i] {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				p.i++

				return true
			}
		case '"':
			end := jsonStringEnd(p.s, p.i)
			if end < 0 {
				return false
			}

			p.i = end
		}
	}

	return false
}

// logfmtPairs parses a whole line of logfmt (key=value key="quoted value"). Any token that
// is not key=value rejects the line, so prose with an "=" in it is left alone.
func logfmtPairs(s string) ([]logPair, bool) {
	var pairs []logPair

	for i := 0; ; {
		for i < len(s) && s[i] == ' ' {
			i++
		}

		if i == len(s) {
			return pairs, len(pairs) >= 2
		}

		start := i
		for i < len(s) && isLogfmtKeyByte(s[i]) {
			i++
		}

		if i == start || i == len(s) || s[i] != '=' {
			return nil, false
		}

		key := s[start:i]
		i++

		var v string

		if i < len(s) && s[i] == '"' {
			end := jsonStringEnd(s, i)
			if end < 0 || (end+1 < len(s) && s[end+1] != ' ') {
				return nil, false
			}

			v = unquoteLogfmt(s[i : end+1])
			i = end + 1
		} else {
			vs := i
			for i < len(s) && s[i] != ' ' {
				i++
			}

			v = s[vs:i]
		}

		pairs = append(pairs, logPair{k: key, v: v})
	}
}

func isLogfmtKeyByte(c byte) bool {
	return isWordByte(c) || c == '_' || c == '-' || c == '.' || c == '/' || c == '@'
}

// unquoteLogfmt decodes a Go-quoted value (logrus uses %q), keeping the inner text when the
// escapes are not valid Go.
func unquoteLogfmt(q string) string {
	if !strings.Contains(q, `\`) {
		return q[1 : len(q)-1]
	}

	if v, err := strconv.Unquote(q); err == nil {
		return v
	}

	return q[1 : len(q)-1]
}
