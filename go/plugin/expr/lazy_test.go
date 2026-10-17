package expr

import (
	"errors"
	"slices"
	"testing"
)

func TestLazyArguments(t *testing.T) {
	var paths [][]string

	// each(list, pred) counts the elements pred holds for, with the element as $.
	each := Func{Min: 2, Max: 2, Lazy: func(_ Ctx, args []Arg) (Value, error) {
		paths = append(paths, args[0].Path(), args[1].Path())

		list, err := args[0].Value()
		if err != nil {
			return nil, err
		}

		n := int64(0)

		for _, el := range list.([]any) {
			v, err := args[1].Eval(Env{Object: el})
			if err != nil {
				return nil, err
			}

			if Truthy(v) {
				n++
			}
		}

		return n, nil
	}}

	node, err := Parse(`each(items.all, $ > 1)`)
	if err != nil {
		t.Fatal(err)
	}

	env := Env{Vars: map[string]Value{"items": map[string]any{"all": []any{int64(1), int64(2), int64(3)}}}, Funcs: map[string]Func{"each": each}}

	got, err := node.Eval(env)
	if err != nil || got != int64(2) {
		t.Fatalf("each = %v, %v", got, err)
	}

	if !slices.Equal(paths[0], []string{"items", "all"}) || paths[1] != nil {
		t.Errorf("paths %v", paths)
	}

	// A failing argument names the expression and its position.
	bad, _ := Parse(`each(items.all, $ - "x")`)

	_, err = bad.Eval(env)

	var e *Error
	if !errors.As(err, &e) || e.Pos != (Pos{1, 19}) {
		t.Errorf("error %v", err)
	}

	// A Lazy function's own error is wrapped with its name.
	fail := Func{Min: 0, Max: 0, Lazy: func(Ctx, []Arg) (Value, error) { return nil, errors.New("nope") }}
	call, _ := Parse(`fail()`)

	if _, err := call.Eval(Env{Funcs: map[string]Func{"fail": fail}}); err == nil || err.Error() != `1:1: fail: nope (in "fail()")` {
		t.Errorf("error %v", err)
	}
}

// A Lazy function inside a filter still sees @, the element being filtered.
func TestLazyArgumentInsideFilter(t *testing.T) {
	id := Func{Min: 1, Max: 1, Lazy: func(_ Ctx, args []Arg) (Value, error) { return args[0].Value() }}
	again := Func{Min: 1, Max: 1, Lazy: func(c Ctx, args []Arg) (Value, error) { return args[0].Eval(*c.Env) }}
	obj := map[string]any{"items": []any{map[string]any{"x": int64(1)}, map[string]any{"x": int64(2)}}}

	for _, src := range []string{`$.items[?(id(@.x) == 2)]`, `$.items[?(again(@.x) == 2)]`} {
		node, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}

		got, err := node.Eval(Env{Object: obj, Funcs: map[string]Func{"id": id, "again": again}})
		if l, ok := got.([]any); err != nil || !ok || len(l) != 1 {
			t.Errorf("%s = %v, %v", src, got, err)
		}
	}
}

func TestLibraryAndDisplay(t *testing.T) {
	if fn, ok := Library("join"); !ok || fn.Call == nil {
		t.Fatal("join is not in the library")
	}

	if _, ok := Library("count"); ok {
		t.Error("count is an aggregation, not a library function")
	}

	if Display([]any{"a", int64(1)}) != "a, 1" || Display(nil) != "" || !Truthy(1.0) || Truthy("") || Normalize(2.0) != int64(2) {
		t.Error("value helpers")
	}
}
