package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/cyrinux/ichor/go/plugin"
)

// From the diagnosis answer to a runbook: the model is asked once more, with the runbook
// format and the closed list of the app's actions, to write the answer's fix as a runbook.
// Its YAML then goes through the runbook loader step by step: a step the loader refuses, a
// target the report and the answer never named, or a plugin the cluster does not run is
// dropped and listed with its reason. What remains is planned (roles, RBAC, destructive
// flags) and handed back. Nothing is stored or run here: the app shows the plan, and the
// operator keeps it or runs it through the usual confirmations.

const (
	aiRunbookTimeout = 60 * time.Second
	// aiRunbookName names a proposal the model left unnamed.
	aiRunbookName       = "Fix from the AI diagnosis"
	aiRunbookMaxNameLen = 80
)

// runbookProposalPlugins are the plugins with actions the cluster runs; the tests replace it.
var runbookProposalPlugins = activeActionPlugins

type runbookRejected struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type runbookProposal struct {
	RunbookYAML string            `json:"runbookYAML"`
	Plan        json.RawMessage   `json:"plan"`
	Rejected    []runbookRejected `json:"rejected"`
	Empty       bool              `json:"empty"`
}

// runbookDoc is a runbook as the proposal writes it back, keys in the documented order.
type runbookDoc struct {
	SchemaVersion string       `yaml:"schemaVersion"`
	Name          string       `yaml:"name"`
	Description   string       `yaml:"description,omitempty"`
	Steps         []*yaml.Node `yaml:"steps"`
}

// ProposeRunbook asks the model to write answer (the diagnosis answer as shown, real names)
// as a runbook of the app's own actions, and plans what it wrote without running anything:
// {"runbookYAML","plan":<RunbookPlan's JSON>,"rejected":[{"id","reason"}],"empty"}. A step
// the app cannot run is left out and listed in rejected; empty is true when no step is left
// (runbookYAML is then "" and plan null). The model gets the report and the answer masked
// as the diagnosis was; language is the app's language tag. Synchronous: call it off the
// main thread. A function rather than a method of Diagnosis: both apps call it like every
// other (string, error) call of the core.
func ProposeRunbook(d *Diagnosis, provider, apiKey, model, baseURL, language, answer string) (out string, err error) {
	defer maskErr(&err)

	answer = strings.TrimSpace(answer)

	switch {
	case d == nil:
		return "", errors.New("no report")
	case d.cluster.config == "":
		return "", errors.New("this report cannot be turned into a runbook")
	case answer == "":
		return "", errors.New("there is no answer to turn into a runbook")
	case d.outOfDate():
		return "", errors.New(reportOutOfDate)
	}

	plugins := runbookProposalPlugins(d.cluster)

	req, err := newAIRequest(provider, apiKey, model, baseURL)
	if err != nil {
		return "", err
	}

	req.system = runbookProposalPrompt(req.provider.ID == aiDevice, language, plugins)
	req.user = runbookProposalMessage(d.report, d.maskNote(answer))

	ctx, cancel := context.WithTimeout(context.Background(), aiRunbookTimeout)
	defer cancel()

	text, err := collectTurn(ctx, req)
	if err != nil {
		return "", err
	}

	p := d.checkProposal(text, d.reveal(d.report)+"\n"+answer, plugins)

	if !p.Empty {
		rb, err := loadRunbook(p.RunbookYAML)
		if err != nil {
			return "", fmt.Errorf("the proposed runbook: %w", err)
		}

		plan, err := planRunbook(d.cluster.config, d.cluster.context, d.cluster.server, language, rb)
		if err != nil {
			return "", err
		}

		// What is shown is masked as every screen is; the YAML stays the user's own file.
		p.Plan = json.RawMessage(privacy.mask(plan))
	}

	for i := range p.Rejected {
		p.Rejected[i].Reason = privacy.mask(p.Rejected[i].Reason)
	}

	return toJSON(p)
}

// collectTurn is one model turn without tools, its text gathered.
func collectTurn(ctx context.Context, req aiRequest) (string, error) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		text    strings.Builder
		tooLong bool
	)

	_, err := streamTurn(ctx, req, func(piece string) {
		if tooLong {
			return
		}

		text.WriteString(piece)

		if text.Len() > maxAnswerBytes {
			tooLong = true

			stop()
		}
	})

	switch {
	case tooLong:
		return "", errors.New("the answer is far too long, it was stopped")
	case err != nil:
		return "", err
	}

	return strings.ToValidUTF8(text.String(), "�"), nil
}

// checkProposal keeps the steps of the model's runbook the app can run. known is the text
// a target must appear in (the report and the answer, real names).
func (d *Diagnosis) checkProposal(text, known string, plugins []*plugin.Compiled) runbookProposal {
	p := runbookProposal{Rejected: []runbookRejected{}, Empty: true}

	block := runbookBlock(text)
	if block == "" {
		return p
	}

	var top struct {
		Name        string      `yaml:"name"`
		Description string      `yaml:"description"`
		Steps       []yaml.Node `yaml:"steps"`
	}

	if err := yaml.Load([]byte(d.reveal(block)), &top); err != nil {
		p.Rejected = append(p.Rejected, runbookRejected{Reason: "the model's runbook is not valid YAML: " + err.Error()})

		return p
	}

	active := map[string]bool{}
	for _, c := range plugins {
		active[c.Plugin.ID] = true
	}

	var (
		kept []*yaml.Node
		seen = map[string]bool{}
	)

	for i := range top.Steps {
		node := &top.Steps[i]

		id, reason := checkProposedStep(node, i, known, active)

		switch {
		case reason != "":
		case seen[id]:
			reason = "the id is used twice"
		case len(kept) == runbookMaxSteps:
			reason = fmt.Sprintf("a runbook has at most %d steps", runbookMaxSteps)
		}

		if reason != "" {
			p.Rejected = append(p.Rejected, runbookRejected{ID: id, Reason: reason})

			continue
		}

		seen[id] = true
		kept = append(kept, node)
	}

	if len(kept) == 0 {
		return p
	}

	src, err := yaml.Marshal(runbookDoc{SchemaVersion: runbookSchema, Name: proposalName(top.Name), Description: strings.TrimSpace(top.Description), Steps: kept})
	if err != nil {
		p.Rejected = append(p.Rejected, runbookRejected{Reason: err.Error()})

		return p
	}

	p.RunbookYAML, p.Empty = string(src), false

	return p
}

// checkProposedStep loads one step alone: its id (or #n), and why it cannot run ("" if it can).
func checkProposedStep(node *yaml.Node, index int, known string, active map[string]bool) (string, string) {
	id := fmt.Sprintf("#%d", index+1)

	var named struct {
		ID     string `yaml:"id"`
		Action string `yaml:"action"`
	}

	if err := node.Load(&named); err == nil && strings.TrimSpace(named.ID) != "" {
		id = strings.TrimSpace(named.ID)
	}

	// Before the loader, which would first trip on the params an invented action takes.
	if _, ok := runbookActions[named.Action]; !ok && !isRunbookPluginAction(named.Action) {
		return id, fmt.Sprintf("unknown action %q", named.Action)
	}

	src, err := yaml.Marshal(runbookDoc{SchemaVersion: runbookSchema, Name: "step", Steps: []*yaml.Node{node}})
	if err != nil {
		return id, err.Error()
	}

	rb, err := loadRunbook(string(src))
	if err != nil {
		return id, strings.TrimPrefix(err.Error(), "step "+id+": ")
	}

	s := rb.Steps[0]

	if s.Plugin != nil && !active[s.Plugin.compiled.Plugin.ID] {
		return id, "the cluster does not run the plugin " + s.Plugin.compiled.Plugin.ID
	}

	for _, name := range []string{s.Target.Name, s.Target.Node} {
		if name != "" && !strings.Contains(known, name) {
			return id, name + " is not named in the report or the answer"
		}
	}

	return id, ""
}

func proposalName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return aiRunbookName
	}

	if r := []rune(name); len(r) > aiRunbookMaxNameLen {
		name = string(r[:aiRunbookMaxNameLen])
	}

	return name
}

var runbookFence = regexp.MustCompile("(?s)```(?:ya?ml)?[ \t]*\n(.*?)```")

// runbookBlock is the YAML of the model's answer: its first fenced block, or the whole
// answer when it is a bare runbook; "" when there is none.
func runbookBlock(text string) string {
	if m := runbookFence.FindStringSubmatch(text); m != nil {
		return m[1]
	}

	if strings.HasPrefix(strings.TrimSpace(text), "schemaVersion:") {
		return strings.TrimSpace(text)
	}

	return ""
}

// activeActionPlugins are the plugins with actions the cluster runs, as Settings detects
// them; none when that cannot be read (the proposal then offers the built-in actions only).
func activeActionPlugins(target kubeTarget) []*plugin.Compiled {
	var candidates []*plugin.Compiled

	for _, c := range knownPlugins() {
		if len(c.Plugin.Actions) > 0 {
			candidates = append(candidates, c)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if isDemoContext(target.config, target.context) {
		var out []*plugin.Compiled

		for _, c := range candidates {
			if detectItem(c, demoPluginInput(c, "", "")).Active {
				out = append(out, c)
			}
		}

		return out
	}

	out, err := withKube(target, func(ctx context.Context, k *kubeClient) ([]*plugin.Compiled, error) {
		probe, err := newPluginProbe(ctx, k, candidates, hintSet{})
		if err != nil {
			return nil, err
		}

		var active []*plugin.Compiled

		for _, c := range candidates {
			if probe.detect(ctx, k, c).Active {
				active = append(active, c)
			}
		}

		return active, nil
	})
	if err != nil {
		return nil
	}

	return out
}
