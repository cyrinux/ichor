package expr

import (
	"errors"
	"testing"
)

// FuzzParse checks that no source panics the lexer, the parser, Check or Eval, and that
// every parse error is an *Error with a position.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		`$.metadata.name`, `$.a[?(@.type == "Ready")].status | first()`, `$["k-1"][0][*]`,
		`fields.x ? 1 : 2.5e3`, `cronNext("20:00", "1-5", "UTC") > now`, `!conditions.Ready || "a" in ["a"]`,
		`a | b(c | d()) > 1`, `matches($.x, "^[a-z]+$")`, `"é" + '\''`, `-(-1) / 0`, `[1,[2,[3]]][1][1][0]`,
	} {
		f.Add(seed)
	}

	env := fixtureEnv(f)

	f.Fuzz(func(t *testing.T, src string) {
		var e *Error

		n, err := Parse(src)
		if err != nil {
			if !errors.As(err, &e) {
				t.Fatalf("%q: error %T, want *Error", src, err)
			}

			return
		}

		n.Check(Scope{Bool: true})
		_, _ = n.Eval(env)

		if _, err := ParseTemplate("{{" + src + "}}"); err != nil && !errors.As(err, &e) {
			t.Fatalf("template %q: error %T", src, err)
		}
	})
}
