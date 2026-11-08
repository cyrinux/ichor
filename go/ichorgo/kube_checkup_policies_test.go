package ichorgo

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

const (
	kyvernoDenialEvent = `admission webhook "validate.kyverno.svc-fail" denied the request: resource Pod/shop/checkout-7d9c5-abcde was blocked due to the following policies require-labels: autogen-check-for-labels: validation error: Deployment labels are missing: app, version`
	gatekeeperDenial   = `admission webhook "validation.gatekeeper.sh" denied the request: [require-labels] Container "web" has no label "app"`
	quotaFailedCreate  = `replicasets.apps "checkout-7d9c5" is forbidden: exceeded quota: compute in namespace shop`
)

func TestParsePolicyDenials(t *testing.T) {
	kyverno := parsePolicyDenials(kyvernoDenialEvent)
	if len(kyverno) != 1 || kyverno[0].Source != policySourceKyverno || kyverno[0].Policy != "require-labels" ||
		kyverno[0].Rule != "autogen-check-for-labels" || !strings.HasPrefix(kyverno[0].Message, "validation error") {
		t.Fatalf("kyverno %+v", kyverno)
	}

	several := parsePolicyDenials(`admission webhook "validate.kyverno.svc-fail" denied the request: resource Pod/shop/web was blocked due to the following policies require-labels, disallow-privileged: check-labels: nope`)
	if len(several) != 2 || several[0].Policy != "require-labels" || several[1].Policy != "disallow-privileged" || several[1].Rule != "check-labels" {
		t.Fatalf("several %+v", several)
	}

	gk := parsePolicyDenials(gatekeeperDenial)
	if len(gk) != 1 || gk[0].Source != policySourceGatekeeper || gk[0].Policy != "require-labels" ||
		gk[0].Rule != "" || !strings.Contains(gk[0].Message, `has no label`) {
		t.Fatalf("gatekeeper %+v", gk)
	}

	if got := parsePolicyDenials(quotaFailedCreate); got != nil {
		t.Fatalf("quota matched: %+v", got)
	}

	unknown := parsePolicyDenials(`admission webhook "other.example/deny" denied the request: nope`)
	if len(unknown) != 1 || unknown[0].Policy != "" || unknown[0].Source != "" || unknown[0].Message != "nope" {
		t.Fatalf("unknown %+v", unknown)
	}

	if got := parsePolicyDenials("not a denial"); got != nil {
		t.Fatalf("plain message matched: %+v", got)
	}
}

// The section: a Kyverno denial event names the workload's Deployment, a Gatekeeper denial
// its constraint, a quota FailedCreate matches nothing, a not-ready Enforce policy and a
// 0/1 controller are findings, an Audit policy with fails is a warning.
func TestCheckupPolicies(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kyverno.io","preferredVersion":{"version":"v1"}},{"name":"wgpolicyk8s.io","preferredVersion":{"version":"v1alpha2"}}]}`,
		"GET /apis/kyverno.io/v1/clusterpolicies": `{"items":[
		  {"metadata":{"name":"require-labels"},"spec":{"validationFailureAction":"Audit","rules":[{"name":"check","validate":{}}]},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
		  {"metadata":{"name":"disallow-privileged"},"spec":{"validationFailureAction":"Enforce","rules":[{"name":"check","validate":{}}]},"status":{"conditions":[{"type":"Ready","status":"False","reason":"WebhookNotConfigured","message":"no webhook"}]}}]}`,
		"GET /apis/kyverno.io/v1/policies": `{"items":[]}`,
		"GET /apis/wgpolicyk8s.io/v1alpha2/policyreports": `{"items":[
		  {"metadata":{"namespace":"shop"},"results":[{"source":"kyverno","policy":"require-labels","rule":"check","result":"fail","message":"missing labels","resources":[{"kind":"Deployment","namespace":"shop","name":"web"}]}]}]}`,
		"GET /apis/wgpolicyk8s.io/v1alpha2/clusterpolicyreports":                    `{"items":[]}`,
		"GET /apis/apps/v1/deployments":                                             `{"items":[{"metadata":{"name":"kyverno-admission-controller","namespace":"kyverno","labels":{"app.kubernetes.io/component":"admission-controller"}},"spec":{"replicas":1},"status":{"readyReplicas":0}}]}`,
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": `{"items":[]}`,
		"GET /apis/apps/v1/replicasets":                                             `{"items":[]}`,
		"GET /api/v1/events":                                                        `{"items":[` + denialEvent("ReplicaSet", "shop", "checkout-7d9c5-abcde", kyvernoDenialEvent, 3) + "," + denialEvent("ReplicaSet", "shop", "web-5d-abc", gatekeeperDenial, 1) + "," + denialEvent("ReplicaSet", "shop", "quota-abc", quotaFailedCreate, 2) + `]}`,
		"GET /apis/apps/v1/namespaces/shop/replicasets/checkout-7d9c5-abcde":        `{"metadata":{"ownerReferences":[{"kind":"Deployment","name":"checkout"}]}}`,
		"GET /apis/apps/v1/namespaces/shop/replicasets/web-5d-abc":                  `{"metadata":{"ownerReferences":[{"kind":"Deployment","name":"web"}]}}`,
		"GET /apis/apps/v1/namespaces/shop/replicasets/quota-abc":                   `{"metadata":{}}`,
		"GET /apis/discovery.k8s.io/v1/namespaces/kyverno/endpointslices":           `{"items":[{"endpoints":[{"conditions":{"ready":true}}]}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	in := checkupInput{now: checkupNow}
	s := checkupPolicies(context.Background(), k, in)

	want := []string{
		"policyEngineDown:critical:admission-controller",
		"policyNotReady:critical:disallow-privileged",
		"policyBlocks:critical:checkout",
		"policyBlocks:critical:web",
		"policyFailures:warning:require-labels",
	}
	if got, wantJoin := strings.Join(findingKinds(s), "|"), strings.Join(want, "|"); got != wantJoin {
		t.Fatalf("got\n%v\nwant\n%v", got, wantJoin)
	}

	if down := s.Findings[0]; down.Extra != "kyverno" || down.Message != "every create/update it matches is refused" {
		t.Fatalf("engine down %+v", down)
	}

	if notReady := s.Findings[1]; notReady.Reason != "WebhookNotConfigured" || notReady.Message != "no webhook" {
		t.Fatalf("not ready %+v", notReady)
	}

	blocked := s.Findings[2]
	if blocked.Extra != "require-labels" || blocked.Reason != "autogen-check-for-labels" || blocked.Count != 3 || blocked.Since == 0 {
		t.Fatalf("blocked %+v", blocked)
	}

	if gk := s.Findings[3]; gk.Extra != "require-labels" || gk.Reason != "" || gk.Count != 1 {
		t.Fatalf("gatekeeper %+v", gk)
	}

	if failures := s.Findings[4]; failures.Count != 1 || failures.Extra != "shop" || failures.Message != "would be refused in Enforce mode" {
		t.Fatalf("failures %+v", failures)
	}
}

// denialEvent is one FailedCreate Warning event fixture, recent enough for the window.
func denialEvent(kind, namespace, name, message string, count int) string {
	message = strings.ReplaceAll(message, `"`, `\"`)

	return `{"metadata":{"creationTimestamp":"2026-10-06T11:00:00Z"},"involvedObject":{"kind":"` + kind + `","namespace":"` + namespace + `","name":"` + name + `"},` +
		`"type":"Warning","reason":"FailedCreate","message":"` + message + `","count":` + strconv.Itoa(count) + `,"firstTimestamp":"2026-10-06T11:00:00Z","lastTimestamp":"2026-10-06T11:50:00Z"}`
}

// Without Kyverno and without denial events the section is absent.
func TestCheckupPoliciesAbsent(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":          `{"groups":[]}`,
		"GET /api/v1/events": `{"items":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s := checkupPolicies(context.Background(), k, checkupInput{now: checkupNow})
	if s.Status != checkAbsent {
		t.Fatalf("section %+v", s)
	}
}

// A Gatekeeper-only cluster still gets its denial events.
func TestCheckupPoliciesGatekeeperOnly(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":          `{"groups":[]}`,
		"GET /api/v1/events": `{"items":[` + denialEvent("ReplicaSet", "shop", "web-5d-abc", gatekeeperDenial, 1) + `]}`,
		"GET /apis/apps/v1/namespaces/shop/replicasets/web-5d-abc": `{"metadata":{"ownerReferences":[{"kind":"Deployment","name":"web"}]}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s := checkupPolicies(context.Background(), k, checkupInput{now: checkupNow})
	if got := findingKinds(s); len(got) != 1 || got[0] != "policyBlocks:critical:web" {
		t.Fatalf("section %v", got)
	}
}

// The engine's webhook refusing with no endpoint behind it is its own finding.
func TestCheckupPoliciesWebhookDown(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kyverno.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/kyverno.io/v1/clusterpolicies":                                   `{"items":[{"metadata":{"name":"require-labels"},"spec":{"validationFailureAction":"Enforce","rules":[{"name":"check","validate":{}}]},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`,
		"GET /apis/kyverno.io/v1/policies":                                          `{"items":[]}`,
		"GET /apis/apps/v1/deployments":                                             `{"items":[{"metadata":{"name":"kyverno-admission-controller","namespace":"kyverno","labels":{"app.kubernetes.io/component":"admission-controller"}},"spec":{"replicas":1},"status":{"readyReplicas":1}}]}`,
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": `{"items":[{"metadata":{"name":"kyverno-resource-validating-webhook-cfg"},"webhooks":[{"name":"validate.kyverno.svc-fail","clientConfig":{"service":{"namespace":"kyverno","name":"kyverno-svc"}}}]}]}`,
		"GET /apis/discovery.k8s.io/v1/namespaces/kyverno/endpointslices":           `{"items":[{"endpoints":[{"conditions":{"ready":false}}]}]}`,
		"GET /api/v1/events":                                                        `{"items":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s := checkupPolicies(context.Background(), k, checkupInput{now: checkupNow})
	if got := findingKinds(s); len(got) != 1 || got[0] != "policyEngineDown:critical:kyverno-resource-validating-webhook-cfg" {
		t.Fatalf("section %v", got)
	}

	if w := s.Findings[0]; w.Extra != "kyverno/kyverno-svc" {
		t.Fatalf("webhook %+v", w)
	}
}

// KubeEvents carries the parsed policy on a denial event, nothing on the others.
func TestKubeEventsPolicyField(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/events": `{"items":[` +
			denialEvent("ReplicaSet", "shop", "checkout-7d9c5", kyvernoDenialEvent, 3) + "," +
			denialEvent("ReplicaSet", "shop", "quota-abc", quotaFailedCreate, 1) + `]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := readEvents(context.Background(), k, "shop", "", "")
	if err != nil || len(got.Events) != 2 {
		t.Fatalf("%+v %v", got, err)
	}

	if p := got.Events[0].Policy; p == nil || p.Source != policySourceKyverno || p.Policy != "require-labels" || p.Rule != "autogen-check-for-labels" {
		t.Fatalf("policy %+v", p)
	}

	if got.Events[1].Policy != nil {
		t.Fatalf("quota event got a policy %+v", got.Events[1].Policy)
	}
}

// Gatekeeper's dryrun constraints are measured on purpose: their violations stay on the
// Policies screen, not in the checkup. A constraint no pod enforces is still a finding.
func TestPolicyFindingsLeaveGatekeeperDryrunOut(t *testing.T) {
	report := policiesReport{Policies: []policyRow{
		{Name: "require-labels", Engine: policyEngineKyverno, Action: policyActionAudit, Ready: true, Fail: 4},
		{Name: "must-have-team", Engine: policyEngineGatekeeper, Action: policyActionAudit, Ready: true, Fail: 300},
		{Name: "psp-privileged", Engine: policyEngineGatekeeper, Action: policyActionEnforce, Ready: false, ReadyReason: "NotEnforced"},
	}}

	failures := policyFailureFindings(report)
	if len(failures) != 1 || failures[0].Name != "require-labels" {
		t.Errorf("failures %+v", failures)
	}

	notReady := policyNotReadyFindings(report)
	if len(notReady) != 1 || notReady[0].Name != "psp-privileged" || notReady[0].Severity != sevCritical {
		t.Errorf("not ready %+v", notReady)
	}
}
