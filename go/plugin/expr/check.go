package expr

import (
	"fmt"
	"slices"
)

// Roots are the names an expression can start from besides $ and @.
var Roots = []string{"fields", "health", "left", "dataServices", "params", "conditions", "now"}

// Scope is what an expression may use where it is written (a field, a health rule, an action
// body…), so that the compiler can report mistakes with the YAML line before any evaluation.
type Scope struct {
	Names        []string // roots allowed here (from Roots); nil allows all of them
	Fields       []string // fields.<name> defined so far; nil skips the check
	DataServices []string // dataServices.<name>; nil skips the check
	Params       []string // params.<name>; nil skips the check
	Bool         bool     // a condition (when, enabledWhen): the value must be a bool
	// Vars are extra root names defined here (a plugin's resource keys, name, namespace…),
	// given at evaluation by Env.Vars.
	Vars []string
	// Funcs are functions defined only here (aggregations), given at evaluation by Env.Funcs.
	Funcs map[string]Func
}

// Problem is a static error at a position of the expression.
type Problem struct {
	Pos     Pos
	Message string
}

func (p Problem) String() string { return p.Pos.String() + ": " + p.Message }

func (p *program) Check(scope Scope) []Problem {
	c := checker{scope: scope}

	if k := c.walk(p.root); scope.Bool && k != KindUnknown && k != KindBool {
		c.report(p.root, "a condition must be a bool, this is a %s", k)
	}

	return c.problems
}

type checker struct {
	scope    Scope
	problems []Problem
}

func (c *checker) report(n node, format string, args ...any) {
	c.problems = append(c.problems, Problem{Pos: n.pos(), Message: fmt.Sprintf(format, args...)})
}

// walk reports the problems under n and returns n's static type (KindUnknown when it depends
// on the data), in one pass so that long chains stay linear.
func (c *checker) walk(n node) Kind {
	switch n := n.(type) {
	case *literal:
		return literalKind(n.val)
	case *name:
		return c.checkName(n)
	case *listLit:
		for _, item := range n.items {
			c.walk(item)
		}

		return KindList
	case *member:
		c.checkMember(n)
		c.walk(n.x)
	case *index:
		c.walk(n.x)
		c.walk(n.idx)
	case *wildcard:
		c.walk(n.x)

		return KindList
	case *filter:
		c.walk(n.x)
		c.walk(n.pred)

		return KindList
	case *unary:
		k := c.walk(n.x)
		if n.op == "!" {
			return KindBool
		}

		if k != KindUnknown && k != KindNumber && k != KindNull {
			c.report(n, "cannot negate a %s", k)
		}

		return KindNumber
	case *binary:
		return c.checkBinary(n, c.walk(n.l), c.walk(n.r))
	case *cond:
		c.walk(n.c)

		if t, f := c.walk(n.t), c.walk(n.f); t == f {
			return t
		}
	case *call:
		k := c.checkCall(n)

		for _, a := range n.args {
			c.walk(a)
		}

		return k
	}

	return KindUnknown
}

func (c *checker) checkName(n *name) Kind {
	switch {
	case n.id == "$" || n.id == "@":
	case slices.Contains(c.scope.Vars, n.id):
	case !slices.Contains(Roots, n.id):
		c.report(n, "unknown name %q (names start with $, or one of %v)", n.id, Roots)
	case c.scope.Names != nil && !slices.Contains(c.scope.Names, n.id):
		c.report(n, "%s is not available here", n.id)
	case n.id == "now":
		return KindNumber
	case n.id == "health":
		return KindString
	}

	return KindUnknown
}

// checkMember checks fields.x, dataServices.x and params.x against what the scope defines.
func (c *checker) checkMember(n *member) {
	root, ok := n.x.(*name)
	if !ok {
		return
	}

	var known []string

	switch root.id {
	case "fields":
		known = c.scope.Fields
	case "dataServices":
		known = c.scope.DataServices
	case "params":
		known = c.scope.Params
	default:
		return
	}

	if known != nil && !slices.Contains(known, n.key) {
		c.report(n, "unknown %s.%s", root.id, n.key)
	}
}

// checkBinary reports an operator applied to literal types it rejects, and returns the
// result's type.
func (c *checker) checkBinary(n *binary, l, r Kind) Kind {
	switch n.op {
	case "+", "-", "*", "/":
	default:
		return KindBool
	}

	if l == KindUnknown || r == KindUnknown || l == KindNull || r == KindNull {
		if n.op == "+" {
			return KindUnknown // a number, a string or a list
		}

		return KindNumber
	}

	numbers := l == KindNumber && r == KindNumber
	joinable := n.op == "+" && l == r && (l == KindString || l == KindList)

	switch {
	case numbers:
		return KindNumber
	case joinable:
		return l
	default:
		c.report(n, "cannot apply %s to %s and %s", n.op, l, r)

		return KindUnknown
	}
}

func (c *checker) checkCall(n *call) Kind {
	fn, ok := lookup(c.scope.Funcs, n.fn)

	switch {
	case !ok:
		c.report(n, "unknown function %q", n.fn)

		return KindUnknown
	case !fn.accepts(len(n.args)):
		c.report(n, "%s takes %s, got %d", n.fn, fn.arity(), len(n.args))
	case n.fn == "matches" && n.re == nil:
		c.report(n, "matches needs a literal pattern, so that it compiles once")
	}

	return fn.Returns
}

func literalKind(v Value) Kind {
	switch v.(type) {
	case nil:
		return KindNull
	case bool:
		return KindBool
	case int64, float64:
		return KindNumber
	case string:
		return KindString
	default:
		return KindUnknown
	}
}
