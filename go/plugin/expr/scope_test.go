package expr

import (
	"strings"
	"testing"
)

// Vars and Funcs are what a plugin defines for one place: resource keys and aggregations.
func TestScopeVarsAndFuncs(t *testing.T) {
	count := Func{Min: 1, Max: 2, Returns: KindNumber, Call: func(_ Ctx, a []Value) (Value, error) {
		l, _ := a[0].([]any)

		return int64(len(l)), nil
	}}

	n, err := Parse(`count(sleepinfo) + len(name)`)
	if err != nil {
		t.Fatal(err)
	}

	if p := n.Check(Scope{}); len(p) != 3 {
		t.Fatalf("without Vars and Funcs: %v", p)
	}

	scope := Scope{Vars: []string{"sleepinfo", "name"}, Funcs: map[string]Func{"count": count}, Bool: true}
	if p := n.Check(scope); len(p) != 1 || !strings.Contains(p[0].Message, "this is a number") {
		t.Fatalf("with Vars and Funcs: %v", p)
	}

	got, err := n.Eval(Env{
		Vars:  map[string]Value{"sleepinfo": []any{1, 2, 3}, "name": "ab"},
		Funcs: map[string]Func{"count": count},
	})
	if err != nil || got != int64(5) {
		t.Fatalf("Eval = %v, %v", got, err)
	}

	// A root keeps its meaning over a Var of the same name.
	n, _ = Parse(`health`)
	if got, _ := n.Eval(Env{Health: "ok", Vars: map[string]Value{"health": "shadow"}}); got != "ok" {
		t.Fatalf("health = %v", got)
	}
}
