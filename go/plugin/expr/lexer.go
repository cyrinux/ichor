package expr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tNumber
	tString
	tIdent
	tOp // punctuation and operators, text in token.text
)

type token struct {
	kind tokenKind
	text string // the identifier, the operator, or the raw literal
	val  Value  // the literal's value (tNumber, tString)
	pos  Pos
}

// operators, two-character ones first so "<=" is not read as "<" then "=".
var operators = []string{
	"==", "!=", "<=", ">=", "&&", "||",
	"<", ">", "!", "+", "-", "*", "/", "?", ":", "|", ".", ",", "(", ")", "[", "]", "$", "@",
}

// lexer walks the source rune by rune, keeping the line and column.
type lexer struct {
	src  string
	off  int
	pos  Pos
	toks []token
}

func lex(src string, start Pos) ([]token, error) {
	l := &lexer{src: src, pos: start}

	for {
		l.skipSpace()

		if l.off >= len(l.src) {
			l.toks = append(l.toks, token{kind: tEOF, pos: l.pos})

			return l.toks, nil
		}

		if err := l.next(); err != nil {
			return nil, err
		}
	}
}

func (l *lexer) errorf(at Pos, format string, args ...any) error {
	return &Error{Expr: l.src, Pos: at, Msg: fmt.Sprintf(format, args...)}
}

func (l *lexer) peek() rune {
	r, _ := utf8.DecodeRuneInString(l.src[l.off:])

	return r
}

// advance moves past n bytes that hold no newline but the last rune read.
func (l *lexer) advance(n int) {
	for _, r := range l.src[l.off : l.off+n] {
		if r == '\n' {
			l.pos.Line++
			l.pos.Col = 1
		} else {
			l.pos.Col++
		}
	}

	l.off += n
}

func (l *lexer) skipSpace() {
	for l.off < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.off:])
		if !unicode.IsSpace(r) {
			return
		}

		l.advance(size)
	}
}

func (l *lexer) next() error {
	at, r := l.pos, l.peek()

	switch {
	case r == '"' || r == '\'':
		return l.lexString(at, r)
	case r >= '0' && r <= '9':
		return l.lexNumber(at)
	case r == '_' || unicode.IsLetter(r):
		end := l.off
		for end < len(l.src) {
			c, size := utf8.DecodeRuneInString(l.src[end:])
			if c != '_' && !unicode.IsLetter(c) && !unicode.IsDigit(c) {
				break
			}

			end += size
		}

		l.toks = append(l.toks, token{kind: tIdent, text: l.src[l.off:end], pos: at})
		l.advance(end - l.off)

		return nil
	}

	for _, op := range operators {
		if strings.HasPrefix(l.src[l.off:], op) {
			l.toks = append(l.toks, token{kind: tOp, text: op, pos: at})
			l.advance(len(op))

			return nil
		}
	}

	switch r {
	case '=':
		return l.errorf(at, "unexpected '=' (compare with ==)")
	case '&':
		return l.errorf(at, "unexpected '&' (and is &&)")
	default:
		return l.errorf(at, "unexpected character %q", r)
	}
}

func (l *lexer) lexNumber(at Pos) error {
	end, float := l.off, false

	digits := func() {
		for end < len(l.src) && l.src[end] >= '0' && l.src[end] <= '9' {
			end++
		}
	}

	digits()

	if end+1 < len(l.src) && l.src[end] == '.' && l.src[end+1] >= '0' && l.src[end+1] <= '9' {
		float = true
		end++
		digits()
	}

	if end < len(l.src) && (l.src[end] == 'e' || l.src[end] == 'E') {
		float = true
		end++

		if end < len(l.src) && (l.src[end] == '+' || l.src[end] == '-') {
			end++
		}

		digits()
	}

	text := l.src[l.off:end]

	var val Value
	if float {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return l.errorf(at, "bad number %q", text)
		}

		val = f
	} else {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return l.errorf(at, "bad number %q", text)
		}

		val = n
	}

	l.toks = append(l.toks, token{kind: tNumber, text: text, val: val, pos: at})
	l.advance(end - l.off)

	return nil
}

// lexString reads a "double" or 'single' quoted string with the escapes \\ \" \' \n \r \t
// and \uXXXX.
func (l *lexer) lexString(at Pos, quote rune) error {
	var b strings.Builder

	i := l.off + 1
	for i < len(l.src) {
		c := l.src[i]

		switch {
		case rune(c) == quote:
			l.toks = append(l.toks, token{kind: tString, text: l.src[l.off : i+1], val: b.String(), pos: at})
			l.advance(i + 1 - l.off)

			return nil
		case c == '\\' && i+1 < len(l.src):
			n, err := unescape(l.src[i+1:], &b)
			if err != nil {
				return l.errorf(at, "%v", err)
			}

			i += 1 + n
		default:
			b.WriteByte(c)
			i++
		}

		if b.Len() > MaxStringLen {
			return l.errorf(at, "string longer than 64 KiB")
		}
	}

	return l.errorf(at, "unterminated string")
}

// unescape writes the escape at the start of s (after the backslash) and returns its length.
func unescape(s string, b *strings.Builder) (int, error) {
	switch s[0] {
	case '\\', '"', '\'', '/':
		b.WriteByte(s[0])
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case 'u':
		if len(s) < 5 {
			return 0, errorString("bad \\u escape")
		}

		n, err := strconv.ParseUint(s[1:5], 16, 32)
		if err != nil {
			return 0, errorString("bad \\u escape")
		}

		b.WriteRune(rune(n))

		return 5, nil
	default:
		return 0, errorString("unknown escape \\" + string(s[0]))
	}

	return 1, nil
}

type errorString string

func (e errorString) Error() string { return string(e) }
