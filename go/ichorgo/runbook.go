package ichorgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// A runbook is an ordered list of the app's own actions, written once and run step by step
// (docs/runbooks.md): every step calls the exported function the matching button calls, so
// its can-I gate, demo refusal and audit entry stay where they are.
const (
	runbookSchema   = "ichor.dev/runbook/v1alpha1"
	runbookMaxSize  = 64 << 10
	runbookMaxSteps = 50
)

// runbook is the YAML document, validated.
type runbook struct {
	Name        string
	Description string
	Params      []runbookParam
	Steps       []runbookStep
}

type runbookStep struct {
	ID              string        `yaml:"id" json:"id"`
	Action          string        `yaml:"action" json:"action"`
	Target          runbookTarget `yaml:"target" json:"target"`
	Params          runbookParams `yaml:"params" json:"-"`
	ContinueOnError bool          `yaml:"continueOnError" json:"-"`
}

type runbookTarget struct {
	Kind      string `yaml:"kind" json:"kind"`
	Namespace string `yaml:"namespace" json:"namespace"`
	Name      string `yaml:"name" json:"name"`
	Node      string `yaml:"node" json:"node"`
}

// runbookParams are every action's parameters; an action refuses those it does not take.
type runbookParams struct {
	Replicas    *int   `yaml:"replicas"`
	Suspend     *bool  `yaml:"suspend"`
	Mode        string `yaml:"mode"`
	Action      string `yaml:"action"`
	IncludeBare bool   `yaml:"includeBare"`
	Acknowledge bool   `yaml:"acknowledge"`
}

// set lists the parameters given, by their YAML name.
func (p runbookParams) set() []string {
	var out []string

	add := func(name string, given bool) {
		if given {
			out = append(out, name)
		}
	}

	add("replicas", p.Replicas != nil)
	add("suspend", p.Suspend != nil)
	add("mode", p.Mode != "")
	add("action", p.Action != "")
	add("includeBare", p.IncludeBare)
	add("acknowledge", p.Acknowledge)

	return out
}

// target parts a step names.
const (
	targetWorkload = iota // namespace and name, kind optional
	targetNode
)

// runbookAction is one entry of the catalogue.
type runbookAction struct {
	target      int
	kinds       []string // accepted target kinds, the first is the default; nil: no kind
	params      []string
	destructive bool
	// roles: the Talos roles that may run it, the first is the one the plan shows.
	roles []string
	// access: the kubeActionChecks key for the target kind ("" kind = the default one).
	access map[string]string
}

var kubeAdmin = []string{adminRole}

// runbookActions is the fixed catalogue: nothing else may run.
var runbookActions = map[string]runbookAction{
	"scale": {target: targetWorkload, kinds: []string{"Deployment", "StatefulSet"}, params: []string{"replicas"}, destructive: true, roles: kubeAdmin,
		access: map[string]string{"Deployment": "scale", "StatefulSet": "scaleStatefulSet"}},
	"rolloutRestart": {target: targetWorkload, kinds: []string{"Deployment", "StatefulSet", "DaemonSet"}, destructive: true, roles: kubeAdmin,
		access: map[string]string{"Deployment": "restartWorkload", "StatefulSet": "restartStatefulSet", "DaemonSet": "restartDaemonSet"}},
	"suspendCronJob": {target: targetWorkload, params: []string{"suspend"}, roles: kubeAdmin, access: map[string]string{"": "suspendCronJob"}},
	"triggerCronJob": {target: targetWorkload, roles: kubeAdmin, access: map[string]string{"": "triggerCronJob"}},
	"argoSync":       {target: targetWorkload, destructive: true, roles: kubeAdmin, access: map[string]string{"": "argoSync"}},
	"fluxReconcile": {target: targetWorkload, kinds: []string{"Kustomization", "HelmRelease", "GitRepository", "OCIRepository", "HelmRepository", "Bucket"}, roles: kubeAdmin,
		access: map[string]string{
			"Kustomization": "fluxReconcile", "HelmRelease": "fluxReconcileHelmRelease", "GitRepository": "fluxReconcileGitRepository",
			"OCIRepository": "fluxReconcileOCIRepository", "HelmRepository": "fluxReconcileHelmRepository", "Bucket": "fluxReconcileBucket",
		}},
	"etcdSnapshot": {target: targetNode, roles: []string{"os:operator", "os:etcd:backup", adminRole}},
	"nodeReboot":   {target: targetNode, params: []string{"mode"}, destructive: true, roles: []string{"os:operator", adminRole}},
	// Drains through the Kubernetes API, then reboots or shuts down: os:admin.
	"nodeMaintenance": {target: targetNode, params: []string{"action", "includeBare", "acknowledge"}, destructive: true, roles: kubeAdmin,
		access: map[string]string{"": "drainNode"}},
}

// runbookActionNames is the catalogue in order, for messages.
func runbookActionNames() []string {
	names := make([]string, 0, len(runbookActions))
	for name := range runbookActions {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// loadRunbook parses and validates src as a draft: a parameter without a value or default
// stays ${name} in the step (what a list row and the import show); it never runs like that.
// Every error names the step it is about.
func loadRunbook(src string) (runbook, error) {
	return parseRunbook(src, nil, true)
}

func parseRunbook(src string, given map[string]string, draft bool) (runbook, error) {
	if len(src) > runbookMaxSize {
		return runbook{}, fmt.Errorf("the runbook is larger than %d KiB", runbookMaxSize>>10)
	}

	var top struct {
		SchemaVersion string         `yaml:"schemaVersion"`
		Name          string         `yaml:"name"`
		Description   string         `yaml:"description"`
		Params        []runbookParam `yaml:"params"`
		Steps         []yaml.Node    `yaml:"steps"`
	}

	if err := yaml.Load([]byte(src), &top, yaml.WithKnownFields()); err != nil {
		return runbook{}, fmt.Errorf("invalid runbook: %w", err)
	}

	rb := runbook{Name: strings.TrimSpace(top.Name), Description: strings.TrimSpace(top.Description)}

	switch {
	case top.SchemaVersion != runbookSchema:
		return runbook{}, fmt.Errorf("schemaVersion must be %s", runbookSchema)
	case rb.Name == "":
		return runbook{}, errors.New("the runbook has no name")
	case len(top.Steps) == 0:
		return runbook{}, errors.New("the runbook has no steps")
	case len(top.Steps) > runbookMaxSteps:
		return runbook{}, fmt.Errorf("the runbook has %d steps (at most %d)", len(top.Steps), runbookMaxSteps)
	}

	var err error
	if rb.Params, err = checkRunbookParams(top.Params); err != nil {
		return runbook{}, err
	}

	values, err := runbookParamValues(rb.Params, given, draft)
	if err != nil {
		return runbook{}, err
	}

	seen := map[string]bool{}

	for i := range top.Steps {
		step, err := loadRunbookStep(&top.Steps[i], i, values, draft)
		if err != nil {
			return runbook{}, err
		}

		if seen[step.ID] {
			return runbook{}, fmt.Errorf("step %s: the id is used twice", step.ID)
		}

		seen[step.ID] = true
		rb.Steps = append(rb.Steps, step)
	}

	return rb, nil
}

func loadRunbookStep(node *yaml.Node, index int, values map[string]string, draft bool) (runbookStep, error) {
	// The id first, so a step that fails to decode is still named.
	label := fmt.Sprintf("#%d", index+1)

	var named struct {
		ID string `yaml:"id"`
	}

	if node.Load(&named) == nil && strings.TrimSpace(named.ID) != "" {
		label = strings.TrimSpace(named.ID)
	}

	if err := substituteRunbookStep(node, values); err != nil {
		return runbookStep{}, fmt.Errorf("step %s: %w", label, err)
	}

	var step runbookStep
	if err := node.Load(&step, yaml.WithKnownFields()); err != nil {
		return runbookStep{}, fmt.Errorf("step %s: %w", label, err)
	}

	step.ID = strings.TrimSpace(step.ID)
	if step.ID == "" {
		return runbookStep{}, fmt.Errorf("step %s: no id", label)
	}

	if err := step.validate(draft); err != nil {
		return runbookStep{}, fmt.Errorf("step %s: %w", step.ID, err)
	}

	return step, nil
}

// validate checks the step; a draft does not hold a ${name} to the values it may become.
func (s *runbookStep) validate(draft bool) error {
	a, ok := runbookActions[s.Action]
	if !ok {
		return fmt.Errorf("unknown action %q (%s)", s.Action, strings.Join(runbookActionNames(), ", "))
	}

	t := &s.Target
	t.Kind, t.Namespace, t.Name, t.Node = strings.TrimSpace(t.Kind), strings.TrimSpace(t.Namespace), strings.TrimSpace(t.Name), strings.TrimSpace(t.Node)

	if err := a.validateTarget(t, draft); err != nil {
		return err
	}

	for _, p := range s.Params.set() {
		if !slices.Contains(a.params, p) {
			return fmt.Errorf("%s takes no parameter %q", s.Action, p)
		}
	}

	return s.validateParams(draft)
}

func (a runbookAction) validateTarget(t *runbookTarget, draft bool) error {
	if a.target == targetNode {
		if t.Node == "" {
			return errors.New("no target node")
		}

		if t.Kind != "" || t.Namespace != "" || t.Name != "" {
			return errors.New("a node action takes target.node only")
		}

		return nil
	}

	if t.Node != "" {
		return errors.New("a Kubernetes action takes no target.node")
	}

	if t.Namespace == "" || t.Name == "" {
		return errors.New("no target namespace or name")
	}

	switch {
	case a.kinds == nil && t.Kind != "":
		return fmt.Errorf("target.kind is not used by this action")
	case a.kinds != nil && t.Kind == "":
		t.Kind = a.kinds[0]
	case a.kinds != nil && !slices.Contains(a.kinds, t.Kind) && !(draft && strings.Contains(t.Kind, "${")):
		return fmt.Errorf("target.kind %q is not one of %s", t.Kind, strings.Join(a.kinds, ", "))
	}

	return nil
}

func (s *runbookStep) validateParams(draft bool) error {
	p := &s.Params
	open := func(value string) bool { return draft && strings.Contains(value, "${") }

	switch s.Action {
	case "scale":
		if p.Replicas == nil {
			return errors.New("scale needs params.replicas")
		}
	case "suspendCronJob":
		if p.Suspend == nil {
			suspend := true
			p.Suspend = &suspend
		}
	case "nodeReboot":
		if p.Mode == "" {
			p.Mode = "default"
		}

		if _, err := parseRebootMode(p.Mode); err != nil && !open(p.Mode) {
			return err
		}
	case "nodeMaintenance":
		if p.Action == "" {
			p.Action = maintenanceReboot
		}

		if !slices.Contains([]string{maintenanceReboot, maintenanceShutdown, maintenanceNone}, p.Action) && !open(p.Action) {
			return fmt.Errorf("params.action must be %s, %s or %s", maintenanceReboot, maintenanceShutdown, maintenanceNone)
		}
	}

	return nil
}

// continuingAt is rb with continueOnError set on the steps ids names.
func (rb runbook) continuingAt(ids []string) runbook {
	steps := slices.Clone(rb.Steps)
	for i := range steps {
		if slices.Contains(ids, steps[i].ID) {
			steps[i].ContinueOnError = true
		}
	}

	rb.Steps = steps

	return rb
}

// runbookPlanStep is one step of RunbookPlan.
type runbookPlanStep struct {
	ID              string        `json:"id"`
	Action          string        `json:"action"`
	Target          runbookTarget `json:"target"`
	Destructive     bool          `json:"destructive"`
	ContinueOnError bool          `json:"continueOnError"`
	Role            string        `json:"role"`
	Allowed         bool          `json:"allowed"`
	Denied          string        `json:"denied"`
}

type runbookPlanResult struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Steps       []runbookPlanStep `json:"steps"`
	Errors      []string          `json:"errors"`
}

// RunbookPlan is RunbookPlanWith before the values are known, for what the import shows: a
// parameter without a default stays ${name} in its step, which the cluster is not asked about.
// RunbookPlanWith is the plan of a run.
func RunbookPlan(configYAML, contextName, kubeServer, runbookYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	rb, err := loadRunbook(runbookYAML)
	if err != nil {
		return "", err
	}

	return planRunbook(configYAML, contextName, kubeServer, rb)
}

// RunbookPlanWith validates runbookYAML (docs/runbooks.md) with valuesJSON ({"name":"value"},
// "" for none: its parameters, see RunbookSummary) in place, and says what each step will do,
// without running anything: {"name","description","steps":[{"id","action","target":{"kind",
// "namespace","name","node"},"destructive","continueOnError","role","allowed","denied"}],
// "errors":[]}. role is
// the Talos role the step needs; allowed false with denied the reason when the credentials'
// role or the cluster's RBAC refuses it. A runbook that does not load, or misses a required
// value, is an error naming the step or the parameters. In the demo every step is allowed
// (StartRunbookWith refuses the run). kubeServer: see KubePods.
func RunbookPlanWith(configYAML, contextName, kubeServer, runbookYAML, valuesJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	values, err := parseRunbookValues(valuesJSON)
	if err != nil {
		return "", err
	}

	rb, err := loadRunbookWith(runbookYAML, values)
	if err != nil {
		return "", err
	}

	return planRunbook(configYAML, contextName, kubeServer, rb)
}

func planRunbook(configYAML, contextName, kubeServer string, rb runbook) (string, error) {
	gate := newRunbookGate(configYAML, contextName, kubeServer)
	plan := runbookPlanResult{Name: rb.Name, Description: rb.Description, Steps: make([]runbookPlanStep, 0, len(rb.Steps)), Errors: []string{}}

	for _, s := range rb.Steps {
		a := runbookActions[s.Action]
		ps := runbookPlanStep{ID: s.ID, Action: s.Action, Target: s.Target, Destructive: a.destructive, ContinueOnError: s.ContinueOnError, Role: a.roles[0]}
		if !s.unresolved() {
			ps.Denied = gate.denied(s, a)
		}

		ps.Allowed = ps.Denied == ""
		plan.Steps = append(plan.Steps, ps)
	}

	return toJSON(plan)
}

type runbookSummary struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Steps       int            `json:"steps"`
	Destructive int            `json:"destructive"`
	Params      []runbookParam `json:"params"`
}

// RunbookSummary is what a list row shows of runbookYAML, without asking the cluster:
// {"name","description","steps","destructive","params":[{"name","prompt","default","kind"}]}
// (the number of destructive steps; what to ask before the plan). A runbook that does not
// load is an error naming the step, as in RunbookPlan.
func RunbookSummary(runbookYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	rb, err := loadRunbook(runbookYAML)
	if err != nil {
		return "", err
	}

	sum := runbookSummary{Name: rb.Name, Description: rb.Description, Steps: len(rb.Steps), Params: rb.Params}
	for _, s := range rb.Steps {
		if runbookActions[s.Action].destructive {
			sum.Destructive++
		}
	}

	return toJSON(sum)
}

// runbookGate answers whether a step may run: the Talos role first, then the cluster's RBAC.
type runbookGate struct {
	target kubeTarget
	demo   bool
	kube   bool     // a kubeconfig cluster: no Talos role, no Talos API
	roles  []string // the talosconfig's client roles
	// rolesKnown: roles come from the client certificate; Omni and an unreadable context
	// leave them to the cluster, which refuses what the role may not do.
	rolesKnown bool
	// access: KubeActionAccess per namespace.
	access map[string]kubeActionAccess
}

func newRunbookGate(configYAML, contextName, kubeServer string) *runbookGate {
	g := &runbookGate{target: kubeTarget{configYAML, contextName, kubeServer}, access: map[string]kubeActionAccess{}}
	g.demo = isDemoContext(configYAML, contextName)
	g.kube = isKubeconfig(configYAML)

	if !g.demo && !g.kube {
		if name, ctx, err := resolveContext(configYAML, contextName); err == nil {
			if summary, err := summarizeContext(name, ctx); err == nil && !summary.Omni {
				g.roles, g.rolesKnown = summary.Roles, true
			}
		}
	}

	return g
}

func (g *runbookGate) denied(s runbookStep, a runbookAction) string {
	if g.demo {
		return ""
	}

	if a.target == targetNode && g.kube {
		return "this cluster is reached through a kubeconfig: node actions need its talosconfig"
	}

	if g.rolesKnown && !slices.ContainsFunc(a.roles, func(r string) bool { return slices.Contains(g.roles, r) }) {
		return "needs the " + strings.Join(a.roles, " or ") + " role"
	}

	key := a.access[s.Target.Kind]
	if key == "" {
		key = a.access[""]
	}

	if key == "" {
		return ""
	}

	namespace := s.Target.Namespace

	access, ok := g.access[namespace]
	if !ok {
		out, err := KubeActionAccess(g.target.config, g.target.context, g.target.server, namespace)
		if err == nil {
			err = json.NewDecoder(bytes.NewReader([]byte(out))).Decode(&access)
		}

		if err != nil {
			// The review could not be asked: the step stays offered, as the buttons do.
			return ""
		}

		g.access[namespace] = access
	}

	if answer, ok := access.Actions[key]; ok && !answer.Allowed {
		return accessReason(answer)
	}

	return ""
}

// accessReason explains a refused review: what was refused, and the API server's reason.
func accessReason(a kubeAccess) string {
	what := a.Verb + " " + a.Resource
	if a.Group != "" {
		what += "." + a.Group
	}

	where := "every namespace"
	if a.Namespace != "" {
		where = "namespace " + a.Namespace
	}

	msg := "the cluster's RBAC refuses " + strings.TrimSpace(what) + " in " + where
	if a.Reason != "" {
		msg += ": " + a.Reason
	}

	return msg
}
