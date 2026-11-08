package expr

import (
	"fmt"
	"regexp"
)

// parser is a recursive-descent parser over the token list. Precedence, lowest first:
// pipes, ?: (right), ||, &&, comparisons and in, + -, * /, unary ! -, then paths.
type parser struct {
	src    string
	toks   []token
	i      int
	depth  int
	filter int // nesting of [?( )], where @ is in scope
	// pending is the call a pipe built, waiting to be the leftmost operand of the rest.
	pending node
}

func (p *parser) parse() (node, error) {
	n, err := p.expr()
	if err != nil {
		return nil, err
	}

	if t := p.peek(); t.kind != tEOF {
		return nil, p.errorf(t.pos, "unexpected %s", describe(t))
	}

	return n, nil
}

func (p *parser) errorf(at Pos, format string, args ...any) error {
	return &Error{Expr: p.src, Pos: at, Msg: fmt.Sprintf(format, args...)}
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) take() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}

	return t
}

// is reports whether the next token is the operator op.
func (p *parser) is(op string) bool {
	t := p.peek()

	return t.kind == tOp && t.text == op
}

func (p *parser) accept(op string) bool {
	if p.is(op) {
		p.i++

		return true
	}

	return false
}

func (p *parser) expect(op string) error {
	if p.accept(op) {
		return nil
	}

	t := p.peek()

	return p.errorf(t.pos, "expected %q, found %s", op, describe(t))
}

func describe(t token) string {
	switch t.kind {
	case tEOF:
		return "end of expression"
	case tIdent:
		return fmt.Sprintf("name %q", t.text)
	default:
		return fmt.Sprintf("%q", t.text)
	}
}

// enter bounds the recursion, so that "((((…" cannot exhaust the stack.
func (p *parser) enter(at Pos) error {
	p.depth++
	if p.depth > MaxDepth {
		return p.errorf(at, "expression nested deeper than %d", MaxDepth)
	}

	return nil
}

func (p *parser) leave() { p.depth-- }

// expr is a pipeline: a pipe takes everything to its left as the first argument of the
// call, and the expression goes on after the call with it as the leftmost operand:
// "a + b | string()" is string(a + b), "$.x | default(0) > 5" is default($.x, 0) > 5.
func (p *parser) expr() (node, error) {
	x, err := p.ternary()
	if err != nil {
		return nil, err
	}

	for p.is("|") {
		bar := p.take()

		fn := p.take()
		if fn.kind != tIdent {
			return nil, p.errorf(fn.pos, "expected a function after |, found %s", describe(fn))
		}

		if err := p.expect("("); err != nil {
			return nil, err
		}

		args, err := p.args()
		if err != nil {
			return nil, err
		}

		if p.pending, err = p.newCall(bar.pos, fn, append([]node{x}, args...)); err != nil {
			return nil, err
		}

		if x, err = p.ternary(); err != nil {
			return nil, err
		}
	}

	return x, nil
}

// ternary is "c ? t : f", right-associative. The false branch is not a pipeline, so that
// "c ? a : b | f()" pipes the whole conditional, as everywhere else.
func (p *parser) ternary() (node, error) {
	at := p.peek().pos
	if p.pending != nil {
		at = p.pending.pos()
	}

	if err := p.enter(at); err != nil {
		return nil, err
	}
	defer p.leave()

	c, err := p.binary(0)
	if err != nil {
		return nil, err
	}

	if !p.accept("?") {
		return c, nil
	}

	t, err := p.expr()
	if err != nil {
		return nil, err
	}

	if err := p.expect(":"); err != nil {
		return nil, err
	}

	f, err := p.ternary()
	if err != nil {
		return nil, err
	}

	return &cond{at: at, c: c, t: t, f: f}, nil
}

// levels of the binary operators, lowest first, as Go.
var levels = [][]string{
	{"||"},
	{"&&"},
	{"==", "!=", "<", "<=", ">", ">=", "in"},
	{"+", "-"},
	{"*", "/"},
}

func (p *parser) binaryOp(level int) (token, bool) {
	t := p.peek()
	for _, op := range levels[level] {
		if (t.kind == tOp || t.kind == tIdent) && t.text == op {
			return t, true
		}
	}

	return t, false
}

func (p *parser) binary(level int) (node, error) {
	if level == len(levels) {
		return p.unary()
	}

	l, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}

	for {
		t, ok := p.binaryOp(level)
		if !ok {
			return l, nil
		}

		p.take()

		r, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}

		l = &binary{at: t.pos, op: t.text, l: l, r: r}
	}
}

func (p *parser) unary() (node, error) {
	t := p.peek()
	if p.pending == nil && t.kind == tOp && (t.text == "!" || t.text == "-") {
		if err := p.enter(t.pos); err != nil {
			return nil, err
		}
		defer p.leave()

		p.take()

		x, err := p.unary()
		if err != nil {
			return nil, err
		}

		return &unary{at: t.pos, op: t.text, x: x}, nil
	}

	return p.postfix()
}

// args reads call arguments up to the closing parenthesis (the opening one is taken).
func (p *parser) args() ([]node, error) {
	var args []node

	if p.accept(")") {
		return args, nil
	}

	for {
		a, err := p.expr()
		if err != nil {
			return nil, err
		}

		args = append(args, a)

		if p.accept(")") {
			return args, nil
		}

		if err := p.expect(","); err != nil {
			return nil, err
		}
	}
}

// newCall builds a call; matches compiles its literal pattern now, so that a bad regex is a
// parse error and evaluation never compiles.
func (p *parser) newCall(at Pos, name token, args []node) (node, error) {
	c := &call{at: at, fn: name.text, args: args}

	if name.text == "matches" && len(args) == 2 {
		if lit, ok := args[1].(*literal); ok {
			pattern, isString := lit.val.(string)
			if !isString {
				return nil, p.errorf(lit.at, "matches needs a string pattern")
			}

			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, p.errorf(lit.at, "bad regular expression: %v", err)
			}

			c.re = re
		}
	}

	return c, nil
}

func (p *parser) postfix() (node, error) {
	x, err := p.primary()
	if err != nil {
		return nil, err
	}

	for {
		t := p.peek()

		switch {
		case p.accept("."):
			key := p.take()
			if key.kind != tIdent {
				return nil, p.errorf(key.pos, "expected a field name after '.', found %s", describe(key))
			}

			x = &member{at: key.pos, x: x, key: key.text}
		case p.accept("["):
			if x, err = p.bracket(t.pos, x); err != nil {
				return nil, err
			}
		default:
			return x, nil
		}
	}
}

// bracket reads what follows "[": "*]", "?(pred)]" or "index]".
func (p *parser) bracket(at Pos, x node) (node, error) {
	if p.accept("*") {
		return &wildcard{at: at, x: x}, p.expect("]")
	}

	if p.accept("?") {
		if err := p.expect("("); err != nil {
			return nil, err
		}

		p.filter++
		pred, err := p.expr()
		p.filter--

		if err != nil {
			return nil, err
		}

		if err := p.expect(")"); err != nil {
			return nil, err
		}

		return &filter{at: at, x: x, pred: pred}, p.expect("]")
	}

	idx, err := p.expr()
	if err != nil {
		return nil, err
	}

	return &index{at: at, x: x, idx: idx}, p.expect("]")
}

func (p *parser) primary() (node, error) {
	// The call a pipe just built is the leftmost operand of what follows it.
	if p.pending != nil {
		n := p.pending
		p.pending = nil

		return n, nil
	}

	t := p.take()

	switch t.kind {
	case tNumber, tString:
		return &literal{at: t.pos, val: t.val}, nil
	case tIdent:
		return p.ident(t)
	case tOp:
		return p.primaryOp(t)
	default:
		return nil, p.errorf(t.pos, "unexpected %s", describe(t))
	}
}

func (p *parser) ident(t token) (node, error) {
	switch t.text {
	case "true":
		return &literal{at: t.pos, val: true}, nil
	case "false":
		return &literal{at: t.pos, val: false}, nil
	case "null":
		return &literal{at: t.pos, val: nil}, nil
	case "in":
		return nil, p.errorf(t.pos, "unexpected %s", describe(t))
	}

	if !p.accept("(") {
		return &name{at: t.pos, id: t.text}, nil
	}

	args, err := p.args()
	if err != nil {
		return nil, err
	}

	return p.newCall(t.pos, t, args)
}

func (p *parser) primaryOp(t token) (node, error) {
	switch t.text {
	case "$":
		return &name{at: t.pos, id: "$"}, nil
	case "@":
		if p.filter == 0 {
			return nil, p.errorf(t.pos, "@ is only defined inside a filter [?( )]")
		}

		return &name{at: t.pos, id: "@"}, nil
	case "(":
		x, err := p.expr()
		if err != nil {
			return nil, err
		}

		return x, p.expect(")")
	case "[":
		return p.list(t.pos)
	default:
		return nil, p.errorf(t.pos, "unexpected %s", describe(t))
	}
}

// list reads a list literal "[a, b]" (the opening bracket is taken).
func (p *parser) list(at Pos) (node, error) {
	l := &listLit{at: at}

	if p.accept("]") {
		return l, nil
	}

	for {
		item, err := p.expr()
		if err != nil {
			return nil, err
		}

		l.items = append(l.items, item)

		if p.accept("]") {
			return l, nil
		}

		if err := p.expect(","); err != nil {
			return nil, err
		}
	}
}
