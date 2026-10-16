// Package expr is the plugin expression language: JSONPath-style paths over a decoded
// Kubernetes object, Go operators, pipes and a fixed function library. It runs only in Go
// (the apps receive resolved values), loads no code, and bounds every evaluation in steps,
// depth and string length. The grammar and the functions are documented in docs/plugins.md.
package expr

import (
	"fmt"
	"time"
)

// Limits of one parse or evaluation.
const (
	MaxSteps     = 10_000   // nodes evaluated, plus elements walked by [*] and [?()]
	MaxDepth     = 32       // nesting of the expression
	MaxStringLen = 64 << 10 // bytes of a source, a string literal or a computed string
)

// Value is a decoded JSON value: nil, bool, int64, float64, string, []any or map[string]any.
// Integral numbers read from an object are int64.
type Value = any

// Env is what an expression can read.
type Env struct {
	Object       any              // $: the object, as decoded from JSON
	Fields       map[string]Value // fields.<name>: the plugin's per-object fields computed so far
	Health       string           // health: the object's computed health
	Left         any              // left: the outer object, inside a join predicate
	DataServices map[string]Value // dataServices.<name>
	Params       map[string]Value // params.<name>: an action's inputs
	Now          time.Time        // now (unix ms) and the reference of age, relativeTime, cronNext; zero is the wall clock
}

// Node is a parsed expression. It is an interface so that another backend (CEL) could
// implement the same language later.
type Node interface {
	// Eval evaluates the expression. Missing paths are null, never an error; errors are
	// type errors, limits and failing functions, each naming the expression.
	Eval(env Env) (Value, error)
	// Check reports, without evaluating, the names, functions and arities the scope does not
	// allow, and a non-boolean condition.
	Check(scope Scope) []Problem
	// String is the source.
	String() string
}

// Pos is a 1-based line and column (in runes) in the source.
type Pos struct{ Line, Col int }

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Error is a parse or evaluation error at a position of the expression.
type Error struct {
	Expr string
	Pos  Pos
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s (in %q)", e.Pos, e.Msg, shorten(e.Expr))
}

// shorten keeps error messages readable when the expression is long.
func shorten(s string) string {
	const limit = 80

	r := []rune(s)
	if len(r) <= limit {
		return s
	}

	return string(r[:limit]) + "…"
}

// Parse parses one expression.
func Parse(src string) (Node, error) {
	return parseAt(src, Pos{Line: 1, Col: 1})
}

// program is the hand-written backend of Node.
type program struct {
	src  string
	root node
}

func parseAt(src string, start Pos) (*program, error) {
	if len(src) > MaxStringLen {
		return nil, &Error{Expr: src, Pos: start, Msg: "expression longer than 64 KiB"}
	}

	toks, err := lex(src, start)
	if err != nil {
		return nil, err
	}

	p := &parser{src: src, toks: toks}

	root, err := p.parse()
	if err != nil {
		return nil, err
	}

	return &program{src: src, root: root}, nil
}

func (p *program) Eval(env Env) (Value, error) {
	e := evaluator{env: &env, src: p.src}

	return e.eval(p.root)
}

func (p *program) String() string { return p.src }
