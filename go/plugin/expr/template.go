package expr

import "strings"

// Template is text with {{ expression }} holes (labels, messages, action bodies), using the
// same grammar and limits as Parse.
type Template struct {
	src   string
	parts []templatePart
}

// templatePart is literal text, or an expression when expr is set.
type templatePart struct {
	text string
	expr *program
}

// ParseTemplate parses text with {{ … }} holes. A "}}" inside a quoted string of the
// expression does not close it.
func ParseTemplate(src string) (*Template, error) {
	if len(src) > MaxStringLen {
		return nil, &Error{Expr: src, Pos: Pos{1, 1}, Msg: "template longer than 64 KiB"}
	}

	t := &Template{src: src}
	rest, off := src, 0

	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			t.addText(rest)

			return t, nil
		}

		t.addText(rest[:open])

		body := rest[open+2:]

		end := closing(body)
		if end < 0 {
			return nil, &Error{Expr: src, Pos: posAt(src, off+open), Msg: "unclosed {{"}
		}

		start := off + open + 2

		e, err := parseAt(body[:end], posAt(src, start))
		if err != nil {
			if pe, ok := err.(*Error); ok {
				pe.Expr = src
			}

			return nil, err
		}

		t.parts = append(t.parts, templatePart{expr: e})
		consumed := open + 2 + end + 2
		rest, off = rest[consumed:], off+consumed
	}
}

func (t *Template) addText(s string) {
	if s != "" {
		t.parts = append(t.parts, templatePart{text: s})
	}
}

// closing is the index of the "}}" that ends an expression, skipping quoted strings; -1
// when there is none.
func closing(s string) int {
	var quote byte

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case quote != 0 && c == '\\':
			i++
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
		case c == '"' || c == '\'':
			quote = c
		case c == '}' && i+1 < len(s) && s[i+1] == '}':
			return i
		}
	}

	return -1
}

// posAt is the line and column of byte offset off in src.
func posAt(src string, off int) Pos {
	p := Pos{Line: 1, Col: 1}

	for _, r := range src[:off] {
		if r == '\n' {
			p.Line++
			p.Col = 1
		} else {
			p.Col++
		}
	}

	return p
}

// Render evaluates every hole and joins the parts; a value is written as by string().
func (t *Template) Render(env Env) (string, error) {
	var b strings.Builder

	for _, part := range t.parts {
		if part.expr == nil {
			b.WriteString(part.text)
		} else {
			v, err := part.expr.Eval(env)
			if err != nil {
				return "", err
			}

			b.WriteString(toString(v))
		}

		if b.Len() > MaxStringLen {
			return "", &Error{Expr: t.src, Pos: Pos{1, 1}, Msg: "rendered template longer than 64 KiB"}
		}
	}

	return b.String(), nil
}

// Check checks every hole; Scope.Bool does not apply to a template.
func (t *Template) Check(scope Scope) []Problem {
	scope.Bool = false

	var out []Problem

	for _, part := range t.parts {
		if part.expr != nil {
			out = append(out, part.expr.Check(scope)...)
		}
	}

	return out
}

func (t *Template) String() string { return t.src }
