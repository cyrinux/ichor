package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// proposalReport names what the sample answers may target.
const proposalReport = `WORKLOADS NOT READY
  Deployment demo-shop/ingest-api: 0/2 ready (CrashLoopBackOff)
  CronJob demo-shop/report-nightly: last run failed
NODES
  cp-1 Ready`

// proposalDiagnosis is a report on the demo kubeconfig, as CollectDiagnosis keeps it.
func proposalDiagnosis(t *testing.T) *Diagnosis {
	t.Helper()

	return &Diagnosis{report: proposalReport, cluster: kubeTarget{config: demoKubeconfigForTest(t)}}
}

// noProposalPlugins makes the cluster run no plugin with actions, unless a test says so.
func withProposalPlugins(t *testing.T, plugins ...*plugin.Compiled) {
	t.Helper()

	was := runbookProposalPlugins
	runbookProposalPlugins = func(kubeTarget) []*plugin.Compiled { return plugins }

	t.Cleanup(func() { runbookProposalPlugins = was })
}

type proposalResult struct {
	RunbookYAML string            `json:"runbookYAML"`
	Plan        runbookPlanResult `json:"plan"`
	Rejected    []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"rejected"`
	Empty bool `json:"empty"`
}

func propose(t *testing.T, d *Diagnosis, modelAnswer, answer string) (proposalResult, []map[string]any) {
	t.Helper()
	noKubeDial(t)

	srv, bodies := scriptedProvider(t, anthropicStream("end_turn", modelAnswer))

	out, err := ProposeRunbook(d, "anthropic", "", "", srv.URL, "en", answer)
	if err != nil {
		t.Fatal(err)
	}

	var r proposalResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return r, bodies()
}

const threeSteps = "Here it is.\n\n```yaml\nschemaVersion: ichor.dev/runbook/v1alpha1\nname: Restart ingestion\nsteps:\n" +
	"  - {id: scale-down, action: scale, target: {namespace: demo-shop, name: ingest-api}, params: {replicas: 0}}\n" +
	"  - {id: snapshot, action: etcdSnapshot, target: {node: cp-1}}\n" +
	"  - {id: scale-up, action: scale, target: {namespace: demo-shop, name: ingest-api}, params: {replicas: 2}}\n" +
	"```\n"

func TestAIRunbookProposalIsPlanned(t *testing.T) {
	withProposalPlugins(t)

	r, bodies := propose(t, proposalDiagnosis(t), threeSteps, "Scale ingest-api to 0, snapshot etcd on cp-1, scale back to 2.")

	if r.Empty || len(r.Rejected) != 0 || len(r.Plan.Steps) != 3 {
		t.Fatalf("result = %+v", r)
	}

	if s := r.Plan.Steps[0]; s.Action != "scale" || !s.Destructive || s.Target.Name != "ingest-api" || s.Role != adminRole {
		t.Errorf("first step = %+v", s)
	}

	if r.Plan.Steps[1].Destructive {
		t.Errorf("etcdSnapshot is not destructive: %+v", r.Plan.Steps[1])
	}

	// What the app stores loads on its own.
	if _, err := RunbookSummary(r.RunbookYAML); err != nil || !strings.Contains(r.RunbookYAML, runbookSchema) {
		t.Errorf("runbookYAML %q: %v", r.RunbookYAML, err)
	}

	// One plain turn: no tools, the action table in the system prompt.
	if len(bodies) != 1 || bodies[0]["tools"] != nil {
		t.Fatalf("calls = %+v", bodies)
	}

	system, _ := bodies[0]["system"].(string)
	for _, want := range []string{"rolloutRestart", "nodeMaintenance", "until", "available", "replicas"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
}

func TestAIRunbookDropsWhatTheAppCannotDo(t *testing.T) {
	withProposalPlugins(t)

	answer := "```yaml\nschemaVersion: ichor.dev/runbook/v1alpha1\nname: Fix\nsteps:\n" +
		"  - {id: restart, action: rolloutRestart, target: {namespace: demo-shop, name: ingest-api}}\n" +
		"  - {id: shell, action: kubectl, target: {namespace: demo-shop, name: ingest-api}, params: {command: delete pod}}\n" +
		"  - {id: invented, action: scale, target: {namespace: demo-shop, name: payments-api}, params: {replicas: 1}}\n" +
		"  - {id: no-replicas, action: scale, target: {namespace: demo-shop, name: ingest-api}}\n" +
		"  - {id: restart, action: triggerCronJob, target: {namespace: demo-shop, name: report-nightly}}\n" +
		"  - {id: templated, action: scale, target: {namespace: \"${ns}\", name: ingest-api}, params: {replicas: 1}}\n" +
		"```"

	r, _ := propose(t, proposalDiagnosis(t), answer, "Restart ingest-api.")

	if r.Empty || len(r.Plan.Steps) != 1 || r.Plan.Steps[0].ID != "restart" {
		t.Fatalf("kept = %+v", r.Plan.Steps)
	}

	reasons := map[string]string{}
	for _, rej := range r.Rejected {
		reasons[rej.ID] += rej.Reason
	}

	for id, want := range map[string]string{
		"shell":       "unknown action",
		"invented":    "not named in the report",
		"no-replicas": "replicas",
		"restart":     "used twice",
		"templated":   "${ns}",
	} {
		if !strings.Contains(reasons[id], want) {
			t.Errorf("rejected %s: %q, want %q", id, reasons[id], want)
		}
	}

	if strings.Contains(r.RunbookYAML, "kubectl") || strings.Contains(r.RunbookYAML, "payments-api") {
		t.Errorf("a rejected step is still in the runbook:\n%s", r.RunbookYAML)
	}
}

func TestAIRunbookRevealsMaskedNames(t *testing.T) {
	withProposalPlugins(t)

	d := proposalDiagnosis(t)
	d.names = &diagnosisNames{reals: map[string]string{"ingest-api": "workload-1", "demo-shop": "namespace-1"}, counts: map[string]int{}}
	d.report = d.names.mask(d.report)

	model := "```yaml\nschemaVersion: ichor.dev/runbook/v1alpha1\nname: Restart\nsteps:\n" +
		"  - {id: restart, action: rolloutRestart, target: {namespace: namespace-1, name: workload-1}}\n```"

	r, bodies := propose(t, d, model, "Restart ingest-api in demo-shop.")

	if len(r.Plan.Steps) != 1 || r.Plan.Steps[0].Target.Name != "ingest-api" || r.Plan.Steps[0].Target.Namespace != "demo-shop" {
		t.Fatalf("plan = %+v, rejected %+v", r.Plan.Steps, r.Rejected)
	}

	if !strings.Contains(r.RunbookYAML, "ingest-api") || strings.Contains(r.RunbookYAML, "workload-1") {
		t.Errorf("runbookYAML keeps a placeholder:\n%s", r.RunbookYAML)
	}

	// The model only ever saw the placeholders, in the report and in the answer.
	sent, _ := json.Marshal(bodies[0]["messages"])
	if strings.Contains(string(sent), "ingest-api") || !strings.Contains(string(sent), "workload-1") {
		t.Errorf("request = %s", sent)
	}
}

func TestAIRunbookWithoutYAMLIsEmpty(t *testing.T) {
	withProposalPlugins(t)

	r, _ := propose(t, proposalDiagnosis(t), "Nothing in the app can do this: edit the Deployment's image by hand.", "Change the image.")

	if !r.Empty || r.RunbookYAML != "" || len(r.Plan.Steps) != 0 {
		t.Errorf("result = %+v", r)
	}
}

func TestAIRunbookAllStepsRejectedIsEmpty(t *testing.T) {
	withProposalPlugins(t)

	model := "```yaml\nschemaVersion: ichor.dev/runbook/v1alpha1\nname: Fix\nsteps:\n" +
		"  - {id: shell, action: talosctl, target: {node: cp-1}}\n```"

	r, _ := propose(t, proposalDiagnosis(t), model, "Reset cp-1.")

	if !r.Empty || r.RunbookYAML != "" || len(r.Rejected) != 1 {
		t.Errorf("result = %+v", r)
	}
}

func TestAIRunbookPluginActionsOnlyWhenTheClusterRunsThem(t *testing.T) {
	es, err := findPlugin("external-secrets")
	if err != nil {
		t.Fatal(err)
	}

	model := "```yaml\nschemaVersion: ichor.dev/runbook/v1alpha1\nname: Refresh\nsteps:\n" +
		"  - {id: sync, action: plugin/external-secrets/forceSync, target: {namespace: demo-shop, name: ingest-api}}\n```"

	withProposalPlugins(t)

	r, bodies := propose(t, proposalDiagnosis(t), model, "Refresh the ingest-api secret.")
	if system, _ := bodies[0]["system"].(string); strings.Contains(system, "plugin/external-secrets/") {
		t.Error("the prompt lists a plugin the cluster does not run")
	}

	if len(r.Rejected) != 1 || !strings.Contains(r.Rejected[0].Reason, "does not run") {
		t.Errorf("without the plugin: %+v", r)
	}

	withProposalPlugins(t, es)

	r, bodies = propose(t, proposalDiagnosis(t), model, "Refresh the ingest-api secret.")
	if system, _ := bodies[0]["system"].(string); !strings.Contains(system, "plugin/external-secrets/forceSync") {
		t.Error("the prompt lacks the cluster's plugin action")
	}

	if len(r.Plan.Steps) != 1 || r.Plan.Steps[0].PluginID != "external-secrets" {
		t.Errorf("with the plugin: %+v", r)
	}
}

func TestAIRunbookNeedsACluster(t *testing.T) {
	d := &Diagnosis{report: proposalReport}

	if _, err := ProposeRunbook(d, "anthropic", "", "", "https://example.invalid", "en", "x"); err == nil {
		t.Error("a report without a cluster made a proposal")
	}
}

func TestAIRunbookExtractsTheBlock(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"text\n```yaml\na: 1\n```\nmore", "a: 1\n"},
		{"```yml\na: 1\n```", "a: 1\n"},
		{"```\nschemaVersion: x\n```", "schemaVersion: x\n"},
		{"schemaVersion: x\nsteps: []", "schemaVersion: x\nsteps: []"},
		{"no runbook here", ""},
	} {
		if got := runbookBlock(c.in); got != c.want {
			t.Errorf("runbookBlock(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
