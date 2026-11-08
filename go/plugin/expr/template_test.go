package expr

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestTemplate(t *testing.T) {
	tests := []struct {
		src, want string
	}{
		{"plain text", "plain text"},
		{"", ""},
		{"{{ $.metadata.name }} sleeps", "nightly sleeps"},
		{"{{ $.metadata.namespace }}/{{ $.metadata.name }}", "demo-shop/nightly"},
		{`{{ "}}" }}`, "}}"},
		{`{{ '{{' + "}}" }}!`, "{{}}!"},
		{"{{ fields.asleep ? 'asleep' : 'awake' }}", "asleep"},
		{"{{ $.spec.replicas + 1 }} of {{ $.nope }}", "4 of "},
		{"{{ now }}", strconv.FormatInt(now.UnixMilli(), 10)},
		{"tags: {{ $.spec.tags }}", "tags: a, b"},
	}

	for _, tc := range tests {
		tpl, err := ParseTemplate(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}

		got, err := tpl.Render(fixtureEnv(t))
		if err != nil || got != tc.want {
			t.Errorf("%q = %q, %v; want %q", tc.src, got, err, tc.want)
		}

		if tpl.String() != tc.src {
			t.Errorf("String() = %q", tpl.String())
		}
	}
}

func TestTemplateErrors(t *testing.T) {
	tests := []struct {
		src  string
		pos  Pos
		want string
	}{
		{"a {{ $.x", Pos{1, 3}, "unclosed {{"},
		{"line one\nok {{ $.x == }}", Pos{2, 14}, "unexpected end"},
		{"{{ 1 }} {{ = }}", Pos{1, 12}, "compare with =="},
	}

	for _, tc := range tests {
		_, err := ParseTemplate(tc.src)

		var e *Error
		if !errors.As(err, &e) || e.Pos != tc.pos || !strings.Contains(e.Msg, tc.want) || e.Expr != tc.src {
			t.Errorf("%q: %v, want %q at %v", tc.src, err, tc.want, tc.pos)
		}
	}
}

func TestTemplateCheckAndEvalErrors(t *testing.T) {
	tpl, err := ParseTemplate("{{ name }} at {{ fields.nope }}")
	if err != nil {
		t.Fatal(err)
	}

	problems := tpl.Check(Scope{Fields: []string{"x"}, Bool: true})
	if len(problems) != 2 || !strings.Contains(problems[0].Message, `unknown name "name"`) ||
		problems[0].Pos != (Pos{1, 4}) || !strings.Contains(problems[1].Message, "unknown fields.nope") {
		t.Fatalf("problems %v", problems)
	}

	if _, err := tpl.Render(Env{}); err == nil {
		t.Fatal("rendering an unknown name succeeds")
	}
}

// TestTemplateValue: a template that is one hole keeps its value's type; any other is text.
func TestTemplateValue(t *testing.T) {
	tests := []struct {
		src  string
		want any
	}{
		{"{{ $.spec.replicas }}", int64(3)},
		{"{{ $.spec.replicas > 1 }}", true},
		{"{{ $.nope }}", nil},
		{" {{ $.spec.replicas }}", " 3"},
		{"{{ $.spec.replicas }}x", "3x"},
		{"plain", "plain"},
	}

	for _, tc := range tests {
		tpl, err := ParseTemplate(tc.src)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}

		got, err := tpl.Value(fixtureEnv(t))
		if err != nil || got != tc.want {
			t.Errorf("%q = %#v, %v; want %#v", tc.src, got, err, tc.want)
		}
	}
}
