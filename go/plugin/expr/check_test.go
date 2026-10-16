package expr

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	fields := Scope{Fields: []string{"asleep", "sleepAt"}, DataServices: []string{"summary"}, Params: []string{"replicas"}}

	tests := []struct {
		name, src string
		scope     Scope
		want      []string // problem substrings, in order; none when empty
	}{
		{"clean", `fields.asleep && $.spec.x == "y"`, fields, nil},
		{"unknown name", `nope.x`, Scope{}, []string{`1:1: unknown name "nope"`}},
		{"name not in scope", `params.replicas`, Scope{Names: []string{"fields"}}, []string{"params is not available here"}},
		{"unknown field", `fields.asleep || fields.wakeUp`, fields, []string{"1:25: unknown fields.wakeUp"}},
		{"unknown data service", `dataServices.nope.total`, fields, []string{"unknown dataServices.nope"}},
		{"unknown param", `params.count`, fields, []string{"unknown params.count"}},
		{"fields unchecked without a list", `fields.anything`, Scope{}, nil},
		{"unknown function", `$.a | nope()`, Scope{}, []string{`1:5: unknown function "nope"`}},
		{"arity", `default($.a)`, Scope{}, []string{"default takes 2 arguments, got 1"}},
		{"variadic arity", `coalesce()`, Scope{}, []string{"coalesce takes at least 1 arguments, got 0"}},
		{"arity through a pipe", `$.a | lower("x")`, Scope{}, []string{"lower takes 1 argument, got 2"}},
		{"dynamic regex", `matches($.a, $.b)`, Scope{}, []string{"matches needs a literal pattern"}},
		{"string plus number", `"a" + 1`, Scope{}, []string{"cannot apply + to string and number"}},
		{"bool times number", `true * 2`, Scope{}, []string{"cannot apply * to bool and number"}},
		{"negate a string", `-"x"`, Scope{}, []string{"cannot negate a string"}},
		{"data types are not guessed", `$.a + 1`, Scope{}, nil},
		{"condition is bool", `fields.asleep && health == "ok"`, Scope{Bool: true}, nil},
		{"condition of unknown type passes", `$.spec.enabled`, Scope{Bool: true}, nil},
		{"default depends on its arguments", `$.a | default(false)`, Scope{Bool: true}, nil},
		{"condition must be bool", `"asleep"`, Scope{Bool: true}, []string{"a condition must be a bool, this is a string"}},
		{"condition must be bool, number", `len($.a) + 1`, Scope{Bool: true}, []string{"this is a number"}},
		{"ternary of bools is bool", `$.a ? true : false`, Scope{Bool: true}, nil},
		{"problems inside a filter", `$.a[?(nope)]`, Scope{}, []string{"unknown name"}},
		{"several problems", `nope(x)`, Scope{}, []string{"unknown function", "unknown name"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse(tc.src)
			if err != nil {
				t.Fatal(err)
			}

			got := n.Check(tc.scope)
			if len(got) != len(tc.want) {
				t.Fatalf("%s: problems %v, want %v", tc.src, got, tc.want)
			}

			for i, p := range got {
				if !strings.Contains(p.String(), tc.want[i]) {
					t.Errorf("%s: problem %q, want %q", tc.src, p, tc.want[i])
				}
			}
		})
	}
}
