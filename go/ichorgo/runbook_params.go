package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// A runbook's parameters (docs/runbooks.md): asked once before the plan, written ${name} in
// any value of a step's target and params. The substitution works on the parsed scalars, so a
// value can never add YAML, and comes before validation, so the checks and the gate see the
// final step.
const (
	runbookMaxParams = 16

	paramKindString    = "string"
	paramKindNamespace = "namespace"
	paramKindWorkload  = "workload"
	paramKindNode      = "node"
	paramKindInt       = "int"
	paramKindBool      = "bool"
)

var (
	runbookParamName = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	// $${ is a literal ${; ${name} a parameter.
	runbookPlaceholder = regexp.MustCompile(`\$\$\{|\$\{([^}]*)\}`)
	runbookParamKinds  = []string{paramKindString, paramKindNamespace, paramKindWorkload, paramKindNode, paramKindInt, paramKindBool}
)

// runbookParam is one entry of the top-level params list. Default is what the file says,
// whichever YAML type it was written in.
type runbookParam struct {
	Name    string `yaml:"name" json:"name"`
	Prompt  string `yaml:"prompt" json:"prompt"`
	Default string `yaml:"default" json:"default"`
	Kind    string `yaml:"kind" json:"kind"`
}

func (p runbookParam) checkValue(value string) error {
	switch p.Kind {
	case paramKindInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("parameter %s must be a whole number", p.Name)
		}
	case paramKindBool:
		if value != "true" && value != "false" {
			return fmt.Errorf("parameter %s must be true or false", p.Name)
		}
	}

	return nil
}

func checkRunbookParams(params []runbookParam) ([]runbookParam, error) {
	if len(params) > runbookMaxParams {
		return nil, fmt.Errorf("the runbook has %d parameters (at most %d)", len(params), runbookMaxParams)
	}

	out := make([]runbookParam, 0, len(params))
	seen := map[string]bool{}

	for _, p := range params {
		p.Name, p.Prompt, p.Kind = strings.TrimSpace(p.Name), strings.TrimSpace(p.Prompt), strings.TrimSpace(p.Kind)

		switch {
		case !runbookParamName.MatchString(p.Name):
			return nil, fmt.Errorf("parameter name %q must start with a lowercase letter and hold only letters and digits", p.Name)
		case seen[p.Name]:
			return nil, fmt.Errorf("parameter %s is declared twice", p.Name)
		}

		seen[p.Name] = true

		if p.Kind == "" {
			p.Kind = paramKindString
		}

		if !slices.Contains(runbookParamKinds, p.Kind) {
			return nil, fmt.Errorf("parameter %s: unknown kind %q (%s)", p.Name, p.Kind, strings.Join(runbookParamKinds, ", "))
		}

		if p.Prompt == "" {
			p.Prompt = p.Name
		}

		if p.Default != "" {
			if err := p.checkValue(p.Default); err != nil {
				return nil, err
			}
		}

		out = append(out, p)
	}

	return out, nil
}

// runbookParamValues is what each ${name} becomes. Without given values (given nil and draft)
// a parameter with no default stays as it is written.
func runbookParamValues(params []runbookParam, given map[string]string, draft bool) (map[string]string, error) {
	values := make(map[string]string, len(params))

	var missing []string

	for _, p := range params {
		value := strings.TrimSpace(given[p.Name])
		if value == "" {
			value = p.Default
		}

		switch {
		case value == "" && draft:
			values[p.Name] = draftParamValue(p)
		case value == "":
			missing = append(missing, p.Name)
		default:
			if err := p.checkValue(value); err != nil {
				return nil, err
			}

			values[p.Name] = value
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing a value for %s", strings.Join(missing, ", "))
	}

	return values, nil
}

// draftParamValue stands for a value not given yet: its own placeholder, shown as written,
// except where only a number or a bool decodes. The number passes every parameter's range
// (replicas, a wait's timeoutSeconds, intervalSeconds and minReplicas).
func draftParamValue(p runbookParam) string {
	switch p.Kind {
	case paramKindInt:
		return "30"
	case paramKindBool:
		return "false"
	}

	return "${" + p.Name + "}"
}

// substituteRunbookStep replaces the placeholders in the step's target and params.
func substituteRunbookStep(step *yaml.Node, values map[string]string) error {
	if step.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(step.Content); i += 2 {
		if key := step.Content[i].Value; key == "target" || key == "params" {
			if err := substituteRunbookNode(step.Content[i+1], values); err != nil {
				return err
			}
		}
	}

	return nil
}

func substituteRunbookNode(node *yaml.Node, values map[string]string) error {
	if node.Kind != yaml.ScalarNode {
		for _, child := range node.Content {
			if err := substituteRunbookNode(child, values); err != nil {
				return err
			}
		}

		return nil
	}

	var unknown error

	replaced := runbookPlaceholder.ReplaceAllStringFunc(node.Value, func(match string) string {
		if match == "$${" {
			return "${"
		}

		name := match[2 : len(match)-1]

		value, ok := values[name]
		if !ok {
			unknown = fmt.Errorf("unknown parameter ${%s}", name)
		}

		return value
	})
	if unknown != nil {
		return unknown
	}

	if replaced != node.Value {
		// The final text decides the type: "${count}" decodes as the int it now is.
		node.Value, node.Tag, node.Style = replaced, "", 0
	}

	return nil
}

// loadRunbookWith is the runbook with its values in place, ready to plan and run: a required
// parameter without a value is an error naming it.
func loadRunbookWith(src string, values map[string]string) (runbook, error) {
	return parseRunbook(src, values, false)
}

// parseRunbookValues decodes the apps' {"name":"value"}; "" is none.
func parseRunbookValues(valuesJSON string) (map[string]string, error) {
	if valuesJSON == "" {
		return nil, nil
	}

	var values map[string]string
	if err := json.Unmarshal([]byte(valuesJSON), &values); err != nil {
		return nil, errors.New("the runbook's values are not valid")
	}

	return values, nil
}

// unresolved reports a step with a value still to be given: it only exists in a draft.
func (s runbookStep) unresolved() bool {
	t := s.Target

	return strings.Contains(t.Kind+t.Namespace+t.Name+t.Node, "${")
}
