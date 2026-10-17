package expr

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// normalize maps what a decoder produces onto the Value set: integral numbers become int64
// (encoding/json decodes every number as float64), other Go numbers int64 or float64.
func normalize(v any) Value {
	switch x := v.(type) {
	case nil, bool, string, int64, []any, map[string]any:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return int64(x)
		}

		return x
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case float32:
		return normalize(float64(x))
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}

		if f, err := x.Float64(); err == nil {
			return normalize(f)
		}

		return string(x)
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}

		return out
	case map[string]string:
		out := make(map[string]any, len(x))
		for k, s := range x {
			out[k] = s
		}

		return out
	default:
		return x
	}
}

// truthy: null, false, 0, "" and empty lists and maps are false.
func truthy(v Value) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}

func number(v Value) (float64, bool) {
	switch x := normalize(v).(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	default:
		return 0, false
	}
}

// equal is deep equality; numbers compare by value (1 == 1.0), other types never equal.
func equal(a, b Value) bool {
	a, b = normalize(a), normalize(b)

	i, intA := a.(int64)
	j, intB := b.(int64)

	if intA && intB {
		return i == j // exact: float64 loses integers past 2^53
	}

	if x, ok := number(a); ok {
		y, ok := number(b)

		return ok && x == y
	}

	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)

		return ok && x == y
	case string:
		y, ok := b.(string)

		return ok && x == y
	case []any:
		y, ok := b.([]any)

		return ok && slices.EqualFunc(x, y, func(p, q any) bool { return equal(p, q) })
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}

		for k, v := range x {
			w, ok := y[k]
			if !ok || !equal(v, w) {
				return false
			}
		}

		return true
	default:
		return false
	}
}

// ordered compares two numbers or two strings; anything else (null included) is false.
func ordered(op string, a, b Value) bool {
	var c int

	i, intA := normalize(a).(int64)
	j, intB := normalize(b).(int64)

	if intA && intB {
		c = cmp.Compare(i, j)
	} else if x, ok := number(a); ok {
		y, ok := number(b)
		if !ok || math.IsNaN(x) || math.IsNaN(y) {
			return false
		}

		c = cmp.Compare(x, y)
	} else {
		x, ok1 := a.(string)
		y, ok2 := b.(string)

		if !ok1 || !ok2 {
			return false
		}

		c = strings.Compare(x, y)
	}

	switch op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case ">":
		return c > 0
	default:
		return c >= 0
	}
}

// contains is membership: an element of a list, a key of a map, a substring of a string.
func contains(haystack, needle Value) bool {
	switch h := haystack.(type) {
	case []any:
		return slices.ContainsFunc(h, func(v any) bool { return equal(v, needle) })
	case map[string]any:
		k, ok := needle.(string)
		_, found := h[k]

		return ok && found
	case string:
		s, ok := needle.(string)

		return ok && strings.Contains(h, s)
	default:
		return false
	}
}

// arith is + - * /. A null operand gives null (a missing field does not fail the whole
// expression); / is a float division and gives null on zero; + also joins strings and lists.
func arith(op string, a, b Value) (Value, error) {
	a, b = normalize(a), normalize(b)
	if a == nil || b == nil {
		return nil, nil
	}

	if op == "+" {
		if v, ok, err := concat(a, b); ok {
			return v, err
		}
	}

	x, okX := number(a)
	y, okY := number(b)

	if !okX || !okY {
		return nil, fmt.Errorf("cannot apply %s to %s and %s", op, typeName(a), typeName(b))
	}

	if op == "/" {
		if y == 0 {
			return nil, nil
		}

		return normalize(x / y), nil
	}

	i, intX := a.(int64)
	j, intY := b.(int64)

	if intX && intY {
		if v, ok := intArith(op, i, j); ok {
			return v, nil
		}
	}

	switch op {
	case "+":
		return x + y, nil
	case "-":
		return x - y, nil
	default:
		return x * y, nil
	}
}

// intArith is + - * on ints; ok is false on overflow, and the caller computes in float.
func intArith(op string, i, j int64) (int64, bool) {
	switch op {
	case "+":
		r := i + j

		return r, (r > i) == (j > 0)
	case "-":
		r := i - j

		return r, (r < i) == (j > 0)
	default:
		if i == 0 || j == 0 {
			return 0, true
		}

		r := i * j

		return r, r/j == i && !(i == -1 && j == math.MinInt64) && !(j == -1 && i == math.MinInt64)
	}
}

// concat joins two strings or two lists; ok is false for other operands.
func concat(a, b Value) (Value, bool, error) {
	if x, ok := a.(string); ok {
		y, ok := b.(string)
		if !ok {
			return nil, true, fmt.Errorf("cannot add string and %s (use string())", typeName(b))
		}

		if len(x)+len(y) > MaxStringLen {
			return nil, true, fmt.Errorf("string longer than 64 KiB")
		}

		return x + y, true, nil
	}

	if x, ok := a.([]any); ok {
		y, ok := b.([]any)
		if !ok {
			return nil, true, fmt.Errorf("cannot add list and %s", typeName(b))
		}

		return append(slices.Clip(x), y...), true, nil
	}

	return nil, false, nil
}

// toString is the display form: "" for null, a list's elements joined by ", ", JSON for a map.
func toString(v Value) string {
	switch x := normalize(v).(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		return joinBounded(x, ", ")
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}

		return string(b)
	}
}

// Display is a value's display form, as a template writes it: "" for null, a list's
// elements joined by ", ", JSON for a map.
func Display(v Value) string { return toString(v) }

// Truthy is how a condition reads a value: null, false, 0, "" and empty lists and maps are false.
func Truthy(v Value) bool { return truthy(normalize(v)) }

// Normalize maps a decoded value onto the Value set (integral numbers are int64).
func Normalize(v any) Value { return normalize(v) }

// joinBounded joins the elements' display forms and stops once past MaxStringLen: the caller
// sees a string over the limit and reports it, without the whole string being built.
func joinBounded(list []any, sep string) string {
	var b strings.Builder

	for i, el := range list {
		if i > 0 {
			b.WriteString(sep)
		}

		b.WriteString(toString(el))

		if b.Len() > MaxStringLen {
			break
		}
	}

	return b.String()
}

func typeName(v Value) string {
	switch normalize(v).(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case int64:
		return "int"
	case float64:
		return "float"
	case string:
		return "string"
	case []any:
		return "list"
	case map[string]any:
		return "map"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// mapValues are a map's values in key order, so that [*] over a map is deterministic.
func mapValues(m map[string]any) []any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = m[k]
	}

	return out
}

// statusConditions are the object's status.conditions maps.
func statusConditions(obj any) []map[string]any {
	list, _ := field(field(obj, "status"), "conditions").([]any)

	out := make([]map[string]any, 0, len(list))

	for _, c := range list {
		if m, ok := c.(map[string]any); ok {
			out = append(out, m)
		}
	}

	return out
}

// conditionsByType is the conditions.<Type> sugar: true when that condition's status is "True".
func conditionsByType(obj any) Value {
	conds := statusConditions(obj)
	if len(conds) == 0 {
		return nil
	}

	out := make(map[string]any, len(conds))

	for _, c := range conds {
		if t, ok := c["type"].(string); ok {
			out[t] = c["status"] == "True"
		}
	}

	return out
}
