package plugin

import (
	"slices"
	"strings"
	"testing"
)

// A field name or tag mistake in this package is an error, not a panic of plugin-validate.
func TestTagEnum(t *testing.T) {
	types, err := tagEnum(Param{}, "Type")
	if err != nil || !slices.Equal(types, []string{"string", "int", "bool", "enum"}) {
		t.Fatalf("Type: %v %v", types, err)
	}

	for field, want := range map[string]string{
		"NoSuchField": "plugin: no field NoSuchField on plugin.Param",
		"Label":       "plugin: no enum on plugin.Param.Label",
	} {
		if values, err := tagEnum(Param{}, field); values != nil || err == nil || err.Error() != want {
			t.Errorf("%s: %v %v, want %q", field, values, err, want)
		}
	}
}

// The validator reports such a mistake as a finding of the file it checks.
func TestValidatorEnumReportsTagError(t *testing.T) {
	v := &validator{p: &Plugin{}}
	v.enum("params[0].label", "x", Param{}, "Label")

	if len(v.problems) != 1 || v.problems[0].Severity != SeverityError ||
		!strings.Contains(v.problems[0].Msg, "no enum on plugin.Param.Label") {
		t.Fatalf("problems %+v", v.problems)
	}
}
