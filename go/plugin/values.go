package plugin

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/plugin/expr"
	"go.yaml.in/yaml/v4"
)

// At is where a value starts in the file: its first line, and its first column when the
// value sits on one line (0 inside a block scalar, whose indentation is stripped).
type At struct {
	Line, Col int
}

// Expr is an expression written in the YAML, parsed when the file loads. A parse error is
// kept and reported by Validate with the others.
type Expr struct {
	Src string
	At
	node expr.Node
	err  error
}

// NewExpr parses src as an expression written at at (zero when not from a file).
func NewExpr(src string, at At) Expr {
	e := Expr{Src: src, At: at}
	if src != "" {
		e.node, e.err = expr.Parse(src)
	}

	return e
}

// IsZero reports whether the expression is absent.
func (e Expr) IsZero() bool { return e.Src == "" }

// Node is the parsed expression, nil when absent or when it does not parse.
func (e Expr) Node() expr.Node { return e.node }

func (e *Expr) UnmarshalYAML(n *yaml.Node) error {
	src, at, err := scalar(n, "an expression")
	if err != nil {
		return err
	}

	*e = NewExpr(src, at)

	return nil
}

func (e Expr) MarshalJSON() ([]byte, error) { return json.Marshal(e.Src) }

// Template is text with {{ expression }} holes, parsed when the file loads.
type Template struct {
	Src string
	At
	tpl *expr.Template
	err error
}

// NewTemplate parses src as a template written at at.
func NewTemplate(src string, at At) Template {
	t := Template{Src: src, At: at}
	if src != "" {
		t.tpl, t.err = expr.ParseTemplate(src)
	}

	return t
}

// IsZero reports whether the template is absent.
func (t Template) IsZero() bool { return t.Src == "" }

// Parsed is the parsed template, nil when absent or when it does not parse.
func (t Template) Parsed() *expr.Template { return t.tpl }

func (t *Template) UnmarshalYAML(n *yaml.Node) error {
	src, at, err := scalar(n, "a text")
	if err != nil {
		return err
	}

	*t = NewTemplate(src, at)

	return nil
}

func (t Template) MarshalJSON() ([]byte, error) { return json.Marshal(t.Src) }

// Tone is a tone word (ok, warning…) or an expression that evaluates to one (health).
type Tone struct {
	Literal string
	Expr    Expr
}

// IsZero reports whether the tone is absent.
func (t Tone) IsZero() bool { return t.Literal == "" && t.Expr.IsZero() }

func (t *Tone) UnmarshalYAML(n *yaml.Node) error {
	src, at, err := scalar(n, "a tone")
	if err != nil {
		return err
	}

	if n.Tag != "!!str" && n.Tag != "!!null" {
		return fmt.Errorf("line %d: a tone is a word (%s) or an expression, not %s", n.Line, strings.Join(Tones, ", "), n.Value)
	}

	if slices.Contains(Tones, src) {
		*t = Tone{Literal: src}
	} else {
		*t = Tone{Expr: NewExpr(src, at)}
	}

	return nil
}

func (t Tone) MarshalJSON() ([]byte, error) {
	if t.Literal != "" {
		return json.Marshal(t.Literal)
	}

	return json.Marshal(t.Expr.Src)
}

func (t *Title) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		src, at, err := scalar(n, "a title")
		*t = Title{Expr: NewExpr(src, at)}

		return err
	}

	type plain Title

	return n.Load((*plain)(t), yaml.WithKnownFields())
}

func (t Title) MarshalJSON() ([]byte, error) {
	if !t.Expr.IsZero() {
		return json.Marshal(t.Expr.Src)
	}

	type plain Title

	return json.Marshal(plain(t))
}

func (i *Item) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*i = Item{Action: n.Value}

		return nil
	}

	type plain Item

	return n.Load((*plain)(i), yaml.WithKnownFields())
}

func (i Item) MarshalJSON() ([]byte, error) {
	if i.Action != "" {
		return json.Marshal(i.Action)
	}

	type plain Item

	return json.Marshal(plain(i))
}

// scalar reads a scalar node's text and where its content starts: a quoted value one column
// after its quote, a block scalar's on the line after its | or >.
func scalar(n *yaml.Node, what string) (string, At, error) {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}

	at := At{Line: n.Line, Col: n.Column}

	if n.Kind != yaml.ScalarNode {
		return "", at, fmt.Errorf("line %d: %s is a single value, not a list or a map", n.Line, what)
	}

	if n.Tag == "!!null" {
		return "", at, nil
	}

	switch {
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		at = At{Line: n.Line + 1}
	case n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0:
		at.Col++
	}

	return n.Value, at, nil
}

// fileAt is the file position of pos, a position inside a value that starts at at; Col is 0
// when it cannot be known (escapes in a quoted value shift it, a block scalar loses its
// indentation).
func fileAt(at At, pos expr.Pos) At {
	if at.Line == 0 {
		return At{}
	}

	if pos.Line > 1 || at.Col == 0 {
		return At{Line: at.Line + pos.Line - 1}
	}

	return At{Line: at.Line, Col: at.Col + pos.Col - 1}
}
