package expr

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// node is a node of the hand-written AST.
type node interface{ pos() Pos }

type (
	literal struct {
		at  Pos
		val Value
	}
	listLit struct {
		at    Pos
		items []node
	}
	// name is a root: $, @, fields, health, left, dataServices, params, conditions or now.
	name struct {
		at Pos
		id string
	}
	member struct { // x.key
		at  Pos
		x   node
		key string
	}
	index struct { // x[idx]: a list position (negative from the end) or a map key
		at  Pos
		x   node
		idx node
	}
	wildcard struct { // x[*]
		at Pos
		x  node
	}
	filter struct { // x[?(pred)]
		at   Pos
		x    node
		pred node
	}
	unary struct {
		at Pos
		op string
		x  node
	}
	binary struct {
		at   Pos
		op   string
		l, r node
	}
	cond struct { // c ? t : f
		at      Pos
		c, t, f node
	}
	call struct {
		at   Pos
		fn   string
		args []node
		re   *regexp.Regexp // matches with a literal pattern, compiled at parse time
	}
)

func (n *literal) pos() Pos  { return n.at }
func (n *listLit) pos() Pos  { return n.at }
func (n *name) pos() Pos     { return n.at }
func (n *member) pos() Pos   { return n.at }
func (n *index) pos() Pos    { return n.at }
func (n *wildcard) pos() Pos { return n.at }
func (n *filter) pos() Pos   { return n.at }
func (n *unary) pos() Pos    { return n.at }
func (n *binary) pos() Pos   { return n.at }
func (n *cond) pos() Pos     { return n.at }
func (n *call) pos() Pos     { return n.at }

// projection is the result of [*] or [?()] while the path goes on: the following segments
// apply to each element ($.items[*].name is every name). It leaves the path as a list.
type projection []any

// evaluator holds one evaluation's budget and the stack of @ values. The parser bounds the
// nesting; the steps bound the work, and every list a function or + builds is charged by
// its length.
type evaluator struct {
	env   *Env
	src   string
	steps int
	at    []any
	now   time.Time
}

func (e *evaluator) errorf(n node, format string, args ...any) error {
	return &Error{Expr: e.src, Pos: n.pos(), Msg: fmt.Sprintf(format, args...)}
}

// eval evaluates n; a projection leaves as a plain list.
func (e *evaluator) eval(n node) (Value, error) {
	v, err := e.evalRaw(n)
	if p, ok := v.(projection); ok {
		return []any(p), err
	}

	return v, err
}

func (e *evaluator) evalRaw(n node) (Value, error) {
	e.steps++
	if e.steps > MaxSteps {
		return nil, e.errorf(n, "evaluation exceeds %d steps", MaxSteps)
	}

	return e.evalNode(n)
}

func (e *evaluator) evalNode(n node) (Value, error) {
	switch n := n.(type) {
	case *literal:
		return n.val, nil
	case *listLit:
		return e.evalList(n)
	case *name:
		return e.evalName(n)
	case *member:
		return e.evalMember(n)
	case *index:
		return e.evalIndex(n)
	case *wildcard:
		return e.evalWildcard(n)
	case *filter:
		return e.evalFilter(n)
	case *unary:
		return e.evalUnary(n)
	case *binary:
		return e.evalBinary(n)
	case *cond:
		c, err := e.eval(n.c)
		if err != nil {
			return nil, err
		}

		if truthy(c) {
			return e.eval(n.t)
		}

		return e.eval(n.f)
	case *call:
		return e.evalCall(n)
	default:
		return nil, e.errorf(n, "unknown node %T", n)
	}
}

func (e *evaluator) evalList(n *listLit) (Value, error) {
	out := make([]any, 0, len(n.items))

	for _, item := range n.items {
		v, err := e.eval(item)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, nil
}

// charge counts a list that a function or + built against the step budget, so that split
// and concatenation cannot grow memory faster than the steps.
func (e *evaluator) charge(n node, v Value) error {
	if l, ok := v.([]any); ok {
		if e.steps += len(l); e.steps > MaxSteps {
			return e.errorf(n, "evaluation exceeds %d steps", MaxSteps)
		}
	}

	return nil
}

func (e *evaluator) clock() time.Time {
	if e.now.IsZero() {
		e.now = e.env.Now
		if e.now.IsZero() {
			e.now = time.Now()
		}
	}

	return e.now
}

func (e *evaluator) evalName(n *name) (Value, error) {
	switch n.id {
	case "$":
		return normalize(e.env.Object), nil
	case "@":
		if len(e.at) == 0 {
			return nil, nil
		}

		return e.at[len(e.at)-1], nil
	case "fields":
		return valueMap(e.env.Fields), nil
	case "health":
		return e.env.Health, nil
	case "left":
		return normalize(e.env.Left), nil
	case "dataServices":
		return valueMap(e.env.DataServices), nil
	case "params":
		return valueMap(e.env.Params), nil
	case "conditions":
		return conditionsByType(e.env.Object), nil
	case "now":
		return e.clock().UnixMilli(), nil
	default:
		if v, ok := e.env.Vars[n.id]; ok {
			return normalize(v), nil
		}

		return nil, e.errorf(n, "unknown name %q", n.id)
	}
}

// valueMap keeps a nil map null (a typed nil map in an interface is not nil).
func valueMap(m map[string]Value) Value {
	if m == nil {
		return nil
	}

	return m
}

func (e *evaluator) evalMember(n *member) (Value, error) {
	x, err := e.evalRaw(n.x)
	if err != nil {
		return nil, err
	}

	return project(x, func(v any) any { return field(v, n.key) }), nil
}

// field is m[key] when v is a map, else null.
func field(v any, key string) any {
	if m, ok := v.(map[string]any); ok {
		return normalize(m[key])
	}

	return nil
}

// project applies get to x, or to each element of a projection, dropping nulls.
func project(x any, get func(any) any) any {
	p, ok := x.(projection)
	if !ok {
		return get(x)
	}

	out := make(projection, 0, len(p))

	for _, v := range p {
		if r := get(v); r != nil {
			out = append(out, r)
		}
	}

	return out
}

func (e *evaluator) evalIndex(n *index) (Value, error) {
	x, err := e.evalRaw(n.x)
	if err != nil {
		return nil, err
	}

	idx, err := e.eval(n.idx)
	if err != nil {
		return nil, err
	}

	switch i := idx.(type) {
	case string:
		return project(x, func(v any) any { return field(v, i) }), nil
	case int64:
		return project(x, func(v any) any { return at(v, i) }), nil
	case nil:
		return nil, nil
	default:
		return nil, e.errorf(n, "index must be an int or a string, not %s", typeName(idx))
	}
}

// at is l[i] when v is a list, from the end when i is negative; null out of range.
func at(v any, i int64) any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}

	if i < 0 {
		i += int64(len(l))
	}

	if i < 0 || i >= int64(len(l)) {
		return nil
	}

	return normalize(l[i])
}

// elements are what [*] and [?()] walk: a list's items, a map's values by key, or the
// elements of each element of a projection.
func elements(x any) []any {
	switch v := x.(type) {
	case []any:
		return v
	case map[string]any:
		return mapValues(v)
	case projection:
		var out []any
		for _, el := range v {
			out = append(out, elements(el)...)
		}

		return out
	default:
		return nil
	}
}

func (e *evaluator) evalWildcard(n *wildcard) (Value, error) {
	x, err := e.evalRaw(n.x)
	if err != nil {
		return nil, err
	}

	els := elements(x)
	if e.steps += len(els); e.steps > MaxSteps {
		return nil, e.errorf(n, "evaluation exceeds %d steps", MaxSteps)
	}

	out := make(projection, 0, len(els))
	for _, el := range els {
		out = append(out, normalize(el))
	}

	return out, nil
}

func (e *evaluator) evalFilter(n *filter) (Value, error) {
	x, err := e.evalRaw(n.x)
	if err != nil {
		return nil, err
	}

	out := projection{}

	for _, el := range elements(x) {
		el = normalize(el)
		e.at = append(e.at, el)
		keep, err := e.eval(n.pred)
		e.at = e.at[:len(e.at)-1]

		if err != nil {
			return nil, err
		}

		if truthy(keep) {
			out = append(out, el)
		}
	}

	return out, nil
}

func (e *evaluator) evalUnary(n *unary) (Value, error) {
	x, err := e.eval(n.x)
	if err != nil {
		return nil, err
	}

	if n.op == "!" {
		return !truthy(x), nil
	}

	switch v := x.(type) {
	case nil:
		return nil, nil
	case int64:
		return -v, nil
	case float64:
		return -v, nil
	default:
		return nil, e.errorf(n, "cannot negate %s", typeName(x))
	}
}

func (e *evaluator) evalBinary(n *binary) (Value, error) {
	l, err := e.eval(n.l)
	if err != nil {
		return nil, err
	}

	// && and || evaluate their right side only when it decides.
	switch n.op {
	case "&&", "||":
		if truthy(l) == (n.op == "||") {
			return n.op == "||", nil
		}

		r, err := e.eval(n.r)
		if err != nil {
			return nil, err
		}

		return truthy(r), nil
	}

	r, err := e.eval(n.r)
	if err != nil {
		return nil, err
	}

	switch n.op {
	case "==":
		return equal(l, r), nil
	case "!=":
		return !equal(l, r), nil
	case "<", "<=", ">", ">=":
		return ordered(n.op, l, r), nil
	case "in":
		return contains(r, l), nil
	default:
		v, err := arith(n.op, l, r)
		if err != nil {
			return nil, e.errorf(n, "%v", err)
		}

		return v, e.charge(n, v)
	}
}

func (e *evaluator) evalCall(n *call) (Value, error) {
	fn, ok := lookup(e.env.Funcs, n.fn)
	if !ok {
		return nil, e.errorf(n, "unknown function %q", n.fn)
	}

	if !fn.accepts(len(n.args)) {
		return nil, e.errorf(n, "%s takes %s, got %d", n.fn, fn.arity(), len(n.args))
	}

	if fn.Lazy != nil {
		return e.evalLazy(n, fn)
	}

	args := make([]Value, len(n.args))
	for i, a := range n.args {
		v, err := e.eval(a)
		if err != nil {
			return nil, err
		}

		args[i] = v
	}

	if n.re != nil {
		s := toString(args[0])
		if len(s) > MaxStringLen {
			return nil, e.errorf(n, "matches on a string longer than 64 KiB")
		}

		return n.re.MatchString(s), nil
	}

	v, err := fn.Call(Ctx{Env: e.env, Now: e.clock()}, args)
	if err != nil {
		return nil, e.errorf(n, "%s: %v", n.fn, err)
	}

	if s, ok := v.(string); ok && len(s) > MaxStringLen {
		return nil, e.errorf(n, "%s returns a string longer than 64 KiB", n.fn)
	}

	return v, e.charge(n, v)
}

// evalLazy calls a Lazy function with its arguments unevaluated.
func (e *evaluator) evalLazy(n *call, fn Func) (Value, error) {
	args := make([]Arg, len(n.args))
	for i, a := range n.args {
		args[i] = Arg{n: a, e: e}
	}

	v, err := fn.Lazy(Ctx{Env: e.env, Now: e.clock()}, args)
	if err != nil {
		var exprErr *Error
		if errors.As(err, &exprErr) {
			return nil, err
		}

		return nil, e.errorf(n, "%s: %v", n.fn, err)
	}

	return v, e.charge(n, v)
}
