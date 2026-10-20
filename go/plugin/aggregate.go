package plugin

import (
	"errors"
	"fmt"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// The aggregations of a data service. Over a resource they name it first: count(res),
// count(res, pred), sum(res, e), min(res, e), max(res, e), worst(res.health) or worst(res, e).
// In a grouped data service they walk the group: count(), count(pred), sum(e), min(e),
// max(e), worst(e). Their other argument is evaluated once per object, with that object as
// $ and its fields and health, so they are Lazy. Over anything else (a list, a data
// service's values) count, sum, min, max and worst read the value itself.
var aggregationNames = []string{"count", "sum", "min", "max", "worst"}

// aggregations are the functions with their arities for Check; the runtime's (with a
// renderer) do the work.
var aggregations = aggregationFuncs(nil, nil)

// aggregationFuncs are the aggregations of r over a resource (group nil) or over group.
func aggregationFuncs(r *renderer, group []*object) map[string]expr.Func {
	arity := map[string][2]int{"count": {0, 2}, "sum": {1, 2}, "min": {1, 2}, "max": {1, 2}, "worst": {1, 2}}
	out := make(map[string]expr.Func, len(aggregationNames))

	for _, name := range aggregationNames {
		returns := expr.KindNumber
		if name == "worst" {
			returns = expr.KindString
		}

		out[name] = expr.Func{Min: arity[name][0], Max: arity[name][1], Returns: returns,
			Lazy: func(_ expr.Ctx, args []expr.Arg) (expr.Value, error) {
				vals, err := r.aggValues(name, group, args)
				if err != nil {
					return nil, err
				}

				return fold(name, vals), nil
			}}
	}

	// readable(res): the resource was listed, so that an empty one means none rather than
	// none visible (a Secret list the account may not read).
	out["readable"] = expr.Func{Min: 1, Max: 1, Returns: expr.KindBool,
		Lazy: func(_ expr.Ctx, args []expr.Arg) (expr.Value, error) {
			path := args[0].Path()
			if len(path) != 1 {
				return nil, errNoResource
			}

			return r == nil || !r.in.Unread[path[0]], nil
		}}

	return out
}

var errNoResource = errors.New("name a resource first")

// aggValues are the values an aggregation folds: one per object (its predicate or
// expression, else true for count and the health for worst), or the elements of a value
// that is not a resource.
func (r *renderer) aggValues(name string, group []*object, args []expr.Arg) ([]expr.Value, error) {
	objs, per := group, args

	if group == nil {
		var err error

		objs, per, err = r.overResource(name, args)
		if errors.Is(err, errNoResource) && len(args) == 1 {
			v, err := args[0].Value()

			return elementsOf(v), err
		}

		if err != nil {
			return nil, err
		}
	}

	out := make([]expr.Value, 0, len(objs))

	for _, o := range objs {
		switch {
		case len(per) > 0:
			v, err := per[0].Eval(r.objectEnv(o))
			if err != nil {
				return nil, err
			}

			out = append(out, v)
		case name == "count":
			out = append(out, true)
		case name == "worst":
			out = append(out, o.health)
		default:
			return nil, fmt.Errorf("%s needs an expression to add up", name)
		}
	}

	return out, nil
}

// overResource reads the resource an aggregation names (res, or res.health for worst) and
// the arguments left.
func (r *renderer) overResource(name string, args []expr.Arg) ([]*object, []expr.Arg, error) {
	if len(args) == 0 {
		return nil, nil, errNoResource
	}

	path := args[0].Path()
	if len(path) == 0 {
		return nil, nil, errNoResource
	}

	if _, ok := r.c.Plugin.Resources[path[0]]; !ok {
		return nil, nil, errNoResource
	}

	switch {
	case len(path) == 1:
	case len(path) == 2 && path[1] == "health" && name == "worst" && len(args) == 1:
	default:
		return nil, nil, fmt.Errorf("write %s(%s, …), not %s", name, path[0], args[0])
	}

	return r.objects[path[0]], args[1:], nil
}

// elementsOf is a list's elements, a map's one value, nothing for null, else the value.
func elementsOf(v expr.Value) []expr.Value {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	default:
		return []expr.Value{x}
	}
}

// fold reduces the values: count counts the true ones, sum adds the numbers (an int while
// every number is one), min and max are null without a number, worst is ok without a tone.
func fold(name string, vals []expr.Value) expr.Value {
	switch name {
	case "count":
		n := int64(0)

		for _, v := range vals {
			if expr.Truthy(v) {
				n++
			}
		}

		return n
	case "worst":
		w := "ok"
		for _, v := range vals {
			w = worse(w, toneOf(v))
		}

		return w
	default:
		return foldNumbers(name, vals)
	}
}

func foldNumbers(name string, vals []expr.Value) expr.Value {
	var (
		acc   float64
		seen  bool
		float bool
	)

	for _, v := range vals {
		n, ok := number(v)
		if !ok {
			continue
		}

		if _, isFloat := expr.Normalize(v).(float64); isFloat {
			float = true
		}

		switch {
		case !seen:
			acc = n
		case name == "sum":
			acc += n
		case name == "min" && n < acc, name == "max" && n > acc:
			acc = n
		}

		seen = true
	}

	switch {
	case !seen && name == "sum":
		return int64(0)
	case !seen:
		return nil
	case float:
		return acc
	default:
		return int64(acc)
	}
}
