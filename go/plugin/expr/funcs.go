package expr

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind is the static type of a value, as far as Check can tell.
type Kind int

const (
	KindUnknown Kind = iota
	KindNull
	KindBool
	KindNumber
	KindString
	KindList
	KindMap
)

func (k Kind) String() string {
	return [...]string{"unknown", "null", "bool", "number", "string", "list", "map"}[k]
}

// Ctx is what a function sees besides its arguments.
type Ctx struct {
	Env *Env
	Now time.Time // Env.Now, or the wall clock when it is zero
}

// Func is a library function. A pipe passes its left side as the first argument.
type Func struct {
	Min, Max int  // arity; Max < 0 is variadic
	Returns  Kind // for Check; KindUnknown when it depends on the arguments
	Call     func(ctx Ctx, args []Value) (Value, error)
	// Lazy, when set, replaces Call and receives its arguments unevaluated: an aggregation
	// evaluates a predicate once per object, with that object's Env.
	Lazy func(ctx Ctx, args []Arg) (Value, error)
}

// Arg is an argument of a Lazy function, not evaluated yet.
type Arg struct {
	n node
	e *evaluator // the call's: its Env, @ and step budget
}

// Value evaluates the argument where the call is written, as an eager argument would be.
func (a Arg) Value() (Value, error) { return a.e.eval(a.n) }

// Eval evaluates the argument with env (one object of an aggregation, a join candidate…),
// @ still the element of an enclosing filter, with a budget of its own: the caller's loop
// is bounded by its data, not by the expression.
func (a Arg) Eval(env Env) (Value, error) {
	e := evaluator{env: &env, src: a.e.src, at: append([]any(nil), a.e.at...), now: a.e.now}

	return e.eval(a.n)
}

// Path is the argument's names when it is a bare name or a chain of keys on one
// (sleepinfo, sleepinfo.health), nil otherwise.
func (a Arg) Path() []string {
	var keys []string

	n := a.n
	for {
		switch x := n.(type) {
		case *name:
			return append([]string{x.id}, keys...)
		case *member:
			keys = append([]string{x.key}, keys...)
			n = x.x
		default:
			return nil
		}
	}
}

// String is the argument's source position, for error messages.
func (a Arg) String() string { return a.n.pos().String() }

func (f Func) accepts(n int) bool { return n >= f.Min && (f.Max < 0 || n <= f.Max) }

func (f Func) arity() string {
	switch {
	case f.Max < 0:
		return fmt.Sprintf("at least %d arguments", f.Min)
	case f.Min == f.Max && f.Min == 1:
		return "1 argument"
	case f.Min == f.Max:
		return fmt.Sprintf("%d arguments", f.Min)
	default:
		return fmt.Sprintf("%d to %d arguments", f.Min, f.Max)
	}
}

// registry holds the library. It is written by init and Register only, before any evaluation.
var registry = map[string]Func{}

// Register adds or replaces a function. Call it from an init function: the registry is not
// safe for writes concurrent with evaluation.
func Register(name string, fn Func) { registry[name] = fn }

// Library is the library function name (not a Scope's own).
func Library(name string) (Func, bool) {
	fn, ok := registry[name]

	return fn, ok
}

// lookup finds a function among the local ones (a Scope's or an Env's Funcs) first, then the
// library.
func lookup(local map[string]Func, name string) (Func, bool) {
	if fn, ok := local[name]; ok {
		return fn, true
	}

	fn, ok := registry[name]

	return fn, ok
}

func init() {
	for name, fn := range builtins {
		Register(name, fn)
	}

	for name, fn := range timeBuiltins {
		Register(name, fn)
	}
}

var builtins = map[string]Func{
	// default(v, d): d when v is null; false, 0 and "" are kept.
	"default": {Min: 2, Max: 2, Call: func(_ Ctx, a []Value) (Value, error) {
		if a[0] == nil {
			return a[1], nil
		}

		return a[0], nil
	}},
	"has": {Min: 1, Max: 1, Returns: KindBool, Call: func(_ Ctx, a []Value) (Value, error) {
		return a[0] != nil, nil
	}},
	"len":      {Min: 1, Max: 1, Returns: KindNumber, Call: fnLen},
	"lower":    {Min: 1, Max: 1, Returns: KindString, Call: stringFunc(strings.ToLower)},
	"upper":    {Min: 1, Max: 1, Returns: KindString, Call: stringFunc(strings.ToUpper)},
	"join":     {Min: 2, Max: 2, Returns: KindString, Call: fnJoin},
	"split":    {Min: 2, Max: 2, Returns: KindList, Call: fnSplit},
	"int":      {Min: 1, Max: 1, Returns: KindNumber, Call: fnInt},
	"float":    {Min: 1, Max: 1, Returns: KindNumber, Call: fnFloat},
	"string":   {Min: 1, Max: 1, Returns: KindString, Call: stringFunc(func(s string) string { return s })},
	"quantity": {Min: 1, Max: 1, Returns: KindNumber, Call: fnQuantity},
	"bytes":    {Min: 1, Max: 1, Returns: KindString, Call: fnBytes},
	"first":    {Min: 1, Max: 1, Call: fnFirst},
	"contains": {Min: 2, Max: 2, Returns: KindBool, Call: func(_ Ctx, a []Value) (Value, error) {
		return contains(a[0], a[1]), nil
	}},
	"matches":  {Min: 2, Max: 2, Returns: KindBool, Call: fnMatches},
	"coalesce": {Min: 1, Max: -1, Call: fnCoalesce},
	// condition("Ready") is the object's condition of that type (a map), or null;
	// condition(obj, "Ready") reads another object's, so that $ | condition("Ready") works too.
	"condition": {Min: 1, Max: 2, Returns: KindUnknown, Call: fnCondition},
}

// stringFunc lifts a string function; null reads as "".
func stringFunc(f func(string) string) func(Ctx, []Value) (Value, error) {
	return func(_ Ctx, a []Value) (Value, error) { return f(toString(a[0])), nil }
}

func fnLen(_ Ctx, a []Value) (Value, error) {
	switch v := a[0].(type) {
	case nil:
		return int64(0), nil
	case string:
		return int64(utf8.RuneCountInString(v)), nil
	case []any:
		return int64(len(v)), nil
	case map[string]any:
		return int64(len(v)), nil
	default:
		return nil, fmt.Errorf("no length for %s", typeName(v))
	}
}

func fnJoin(_ Ctx, a []Value) (Value, error) {
	sep, ok := a[1].(string)
	if !ok {
		return nil, fmt.Errorf("the separator must be a string")
	}

	list, ok := a[0].([]any)
	if !ok {
		return toString(a[0]), nil
	}

	return joinBounded(list, sep), nil
}

func fnSplit(_ Ctx, a []Value) (Value, error) {
	sep, ok := a[1].(string)
	if !ok {
		return nil, fmt.Errorf("the separator must be a string")
	}

	s := toString(a[0])
	if s == "" {
		return []any{}, nil
	}

	parts := strings.Split(s, sep)

	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}

	return out, nil
}

// fnInt truncates a number, parses a string, reads a bool as 0 or 1; null otherwise.
func fnInt(c Ctx, a []Value) (Value, error) {
	f, err := fnFloat(c, a)
	if n, ok := f.(float64); ok && err == nil {
		if math.IsNaN(n) || n >= math.MaxInt64 || n < math.MinInt64 {
			return nil, nil
		}

		return int64(n), nil
	}

	return f, err
}

func fnFloat(_ Ctx, a []Value) (Value, error) {
	switch v := normalize(a[0]).(type) {
	case int64:
		return float64(v), nil
	case float64:
		return v, nil
	case bool:
		if v {
			return 1.0, nil
		}

		return 0.0, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return nil, nil
		}

		return f, nil
	default:
		return nil, nil
	}
}

// fnQuantity reads a Kubernetes quantity ("250m", "2Gi") as a number of its base unit;
// numbers pass through, anything else is null.
func fnQuantity(_ Ctx, a []Value) (Value, error) {
	switch v := normalize(a[0]).(type) {
	case int64, float64:
		return v, nil
	case string:
		q, ok := parseQuantity(v)
		if !ok {
			return nil, nil
		}

		return normalize(q), nil
	default:
		return nil, nil
	}
}

// fnBytes formats a size in binary units ("1.5 GiB"); a quantity string is read first.
func fnBytes(c Ctx, a []Value) (Value, error) {
	q, _ := fnQuantity(c, a)

	n, ok := number(q)
	if !ok {
		return "", nil
	}

	return formatBytes(n), nil
}

func fnFirst(_ Ctx, a []Value) (Value, error) {
	if l, ok := a[0].([]any); ok {
		if len(l) == 0 {
			return nil, nil
		}

		return normalize(l[0]), nil
	}

	return a[0], nil
}

// fnMatches compiles its pattern when it is not a literal (a literal one is compiled at
// parse time and never reaches here).
func fnMatches(_ Ctx, a []Value) (Value, error) {
	pattern, ok := a[1].(string)
	if !ok {
		return nil, fmt.Errorf("the pattern must be a string")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	return re.MatchString(toString(a[0])), nil
}

func fnCoalesce(_ Ctx, a []Value) (Value, error) {
	for _, v := range a {
		if v != nil {
			return v, nil
		}
	}

	return nil, nil
}

func fnCondition(c Ctx, a []Value) (Value, error) {
	obj, typ := c.Env.Object, a[0]
	if len(a) == 2 {
		obj, typ = a[0], a[1]
	}

	want, ok := typ.(string)
	if !ok {
		return nil, fmt.Errorf("the condition type must be a string")
	}

	for _, cond := range statusConditions(obj) {
		if cond["type"] == want {
			return cond, nil
		}
	}

	return nil, nil
}
