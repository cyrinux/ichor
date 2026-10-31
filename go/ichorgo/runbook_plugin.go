package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/cyrinux/ichor/go/plugin"
)

// A plugin's action as a runbook step, action: plugin/<id>/<action> (docs/runbooks.md). It
// resolves against the installed plugins when the runbook loads, so that an unknown or turned
// off plugin is an error naming the step; the fixed runbookActions map stays closed. It runs
// through KubePluginAction, the function the card's button calls.

const runbookPluginPrefix = "plugin/"

// runbookPluginStep is what a plugin step resolved to.
type runbookPluginStep struct {
	compiled *plugin.Compiled
	actionID string
	// given are the action's params as the runbook writes them, checked by the plugin.
	given map[string]any
}

func isRunbookPluginAction(action string) bool {
	return strings.HasPrefix(action, runbookPluginPrefix)
}

// resolveRunbookPlugin finds the installed plugin and action that action names.
func resolveRunbookPlugin(action string) (*plugin.Compiled, plugin.Action, string, error) {
	rest := strings.TrimPrefix(action, runbookPluginPrefix)

	pluginID, actionID, ok := strings.Cut(rest, "/")
	if !ok || pluginID == "" || actionID == "" || strings.Contains(actionID, "/") {
		return nil, plugin.Action{}, "", errors.New("a plugin action is written plugin/<id>/<action>")
	}

	c, err := findPlugin(pluginID)
	if err != nil {
		return nil, plugin.Action{}, "", err
	}

	a, ok := c.Plugin.Actions[actionID]
	if !ok {
		return nil, plugin.Action{}, "", fmt.Errorf("plugin %s has no action %q", pluginID, actionID)
	}

	return c, a, actionID, nil
}

// bindPlugin resolves the plugin step and checks its target and params. params is the step's
// params node (nil for none), open the names a draft has no value for yet.
func (s *runbookStep) bindPlugin(params *yaml.Node, open map[string]bool, draft bool) error {
	c, a, actionID, err := resolveRunbookPlugin(s.Action)
	if err != nil {
		return err
	}

	if err := bindPluginTarget(&s.Target, c.Plugin.Resources[a.On], draft); err != nil {
		return err
	}

	given, err := decodePluginStepParams(params, a)
	if err != nil {
		return err
	}

	if _, err := c.ActionParams(actionID, withDraftValues(given, a, open)); err != nil {
		return err
	}

	s.Plugin = &runbookPluginStep{compiled: c, actionID: actionID, given: given}

	return nil
}

// bindPluginTarget checks t against the action's resource, and fills in its one kind.
func bindPluginTarget(t *runbookTarget, decl plugin.Resource, draft bool) error {
	t.Kind, t.Namespace, t.Name, t.Node = strings.TrimSpace(t.Kind), strings.TrimSpace(t.Namespace), strings.TrimSpace(t.Name), strings.TrimSpace(t.Node)
	open := func(value string) bool { return draft && strings.Contains(value, "${") }

	switch {
	case t.Node != "":
		return errors.New("a plugin action takes no target.node")
	case t.Name == "":
		return errors.New("no target name")
	case t.Kind == "":
		t.Kind = decl.Kind
	case t.Kind != decl.Kind && !open(t.Kind):
		return fmt.Errorf("target.kind %q is not %s", t.Kind, decl.Kind)
	}

	if open(t.Namespace) || open(t.Name) {
		return nil
	}

	return validatePluginTarget(decl, t.Namespace, t.Name)
}

// decodePluginStepParams reads the params node. A string or enum param keeps the text of a
// scalar: a value given for ${name} that reads as a number is still that text.
func decodePluginStepParams(node *yaml.Node, a plugin.Action) (map[string]any, error) {
	if node == nil {
		return nil, nil
	}

	var given map[string]any
	if err := node.Load(&given); err != nil {
		return nil, fmt.Errorf("params: %w", err)
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if value.Kind != yaml.ScalarNode || given[key] == nil {
			continue
		}

		for _, p := range a.Params {
			if p.Name == key && (p.Type == "string" || p.Type == "enum") {
				given[key] = value.Value
			}
		}
	}

	return given, nil
}

// withDraftValues stands a valid value in for each param a draft cannot know yet: it is
// checked for real when the values are given. A name the action does not declare stays, and
// is refused.
func withDraftValues(given map[string]any, a plugin.Action, open map[string]bool) map[string]any {
	if len(open) == 0 {
		return given
	}

	out := maps.Clone(given)

	for _, p := range a.Params {
		if !open[p.Name] {
			continue
		}

		switch p.Type {
		case "int":
			out[p.Name] = int64(0)
		case "bool":
			out[p.Name] = false
		case "enum":
			if len(p.Values) > 0 {
				out[p.Name] = p.Values[0]
			}
		default:
			out[p.Name] = ""
		}
	}

	return out
}

// cutPluginStepParams takes the params out of a plugin step's node: they are the action's own,
// not the fixed runbookParams.
func cutPluginStepParams(step *yaml.Node) *yaml.Node {
	if step.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(step.Content); i += 2 {
		if step.Content[i].Value != "params" {
			continue
		}

		params := step.Content[i+1]
		step.Content = append(step.Content[:i:i], step.Content[i+2:]...)

		return params
	}

	return nil
}

// openPluginParams are the names of the params that hold a ${name}.
func openPluginParams(params *yaml.Node) map[string]bool {
	if params.Kind != yaml.MappingNode {
		return nil
	}

	open := map[string]bool{}

	for i := 0; i+1 < len(params.Content); i += 2 {
		if value := params.Content[i+1]; value.Kind == yaml.ScalarNode && strings.Contains(value.Value, "${") {
			open[params.Content[i].Value] = true
		}
	}

	return open
}

// action is the catalogue entry step s runs under. A plugin step's is made from its plugin:
// it is destructive unless the action says confirm: none, and its access row is the can-I row
// of the action's rbac (KubeActionAccess).
func (s runbookStep) action() runbookAction {
	if s.Plugin == nil {
		return runbookActions[s.Action].of(s)
	}

	return runbookAction{
		target: targetWorkload, destructive: s.Plugin.confirm() != "none", roles: kubeAdmin,
		access: map[string]string{"": pluginActionKey(s.Plugin.compiled.Plugin.ID, s.Plugin.actionID)},
	}
}

// confirm is the confirmation the card asks for: none, plain or typedName.
func (p *runbookPluginStep) confirm() string {
	return orDefault(p.compiled.Plugin.Actions[p.actionID].Confirm, "plain")
}

// describePluginStep fills in what the plan's row shows of a plugin step.
func (s runbookStep) describePluginStep(ps *runbookPlanStep, locale string) {
	if s.Plugin == nil {
		return
	}

	c := s.Plugin.compiled
	ps.PluginID, ps.PluginName, ps.PluginIcon = c.Plugin.ID, c.Plugin.Name, c.Plugin.Icon
	ps.Label = c.Translate(locale, c.Plugin.Actions[s.Plugin.actionID].Label)
	ps.Confirm = s.Plugin.confirm()
}

// pluginParamsJSON is the step's params as KubePluginAction takes them, "" for none.
func (s runbookStep) pluginParamsJSON() (string, error) {
	if len(s.Plugin.given) == 0 {
		return "", nil
	}

	js, err := json.Marshal(s.Plugin.given)
	if err != nil {
		return "", fmt.Errorf("the step's parameters: %w", err)
	}

	return string(js), nil
}

// pluginAction runs the step through the card button's function.
func (r runbookExec) pluginAction(s runbookStep) (string, error) {
	params, err := s.pluginParamsJSON()
	if err != nil {
		return "", err
	}

	p := s.Plugin
	id := p.compiled.Plugin.ID
	err = KubePluginAction(r.target.config, r.target.context, r.target.server, id, p.actionID, s.Target.Namespace, s.Target.Name, params)

	return "ran " + pluginActionKey(id, p.actionID), err
}
