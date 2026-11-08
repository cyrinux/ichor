package ichorgo

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const gkTemplates = `{"items":[
 {"metadata":{"name":"k8srequiredlabels","annotations":{"description":"Requires resources to contain specified labels"}},
  "spec":{"crd":{"spec":{"names":{"kind":"K8sRequiredLabels"}}}},"status":{"created":true}},
 {"metadata":{"name":"k8sallowedrepos"},"spec":{"crd":{"spec":{"names":{"kind":"K8sAllowedRepos"}}}},"status":{"created":true}},
 {"metadata":{"name":"k8sbroken"},"spec":{"crd":{"spec":{"names":{"kind":"K8sBroken"}}}},"status":{"created":false}}
]}`

// Two constraints of one kind: one denying with violations past the limit and a pod that does
// not enforce it, one in dryrun without any status yet.
const gkRequiredLabels = `{"items":[
 {"metadata":{"name":"must-have-team"},"spec":{"enforcementAction":"deny"},
  "status":{"auditTimestamp":"2026-10-16T08:00:00Z","totalViolations":25,
   "violations":[
    {"kind":"Deployment","namespace":"shop","name":"cart","message":"you must provide labels: {\"team\"}","enforcementAction":"deny"},
    {"kind":"Namespace","name":"sandbox","message":"you must provide labels: {\"team\"}","enforcementAction":"deny"}],
   "byPod":[{"id":"gatekeeper-audit-1","enforced":true},
            {"id":"gatekeeper-controller-manager-2","enforced":false,"errors":[{"message":"template not ingested"}]}]}},
 {"metadata":{"name":"must-have-owner"},"spec":{"enforcementAction":"dryrun"}}
]}`

const gkDeployments = `{"items":[
 {"metadata":{"namespace":"gatekeeper-system","name":"gatekeeper-controller-manager","labels":{"gatekeeper.sh/operation":"webhook"}},"spec":{"replicas":3},"status":{"readyReplicas":2}},
 {"metadata":{"namespace":"gatekeeper-system","name":"gatekeeper-audit","labels":{"gatekeeper.sh/operation":"audit"}},"spec":{"replicas":1},"status":{"readyReplicas":1}}
]}`

// The chart's defaults: the constraints' webhook Ignore, the ignore-label one Fail.
const gkWebhooks = `{"items":[{"metadata":{"name":"gatekeeper-validating-webhook-configuration"},
 "webhooks":[
  {"name":"validation.gatekeeper.sh","failurePolicy":"Ignore","clientConfig":{"service":{"namespace":"gatekeeper-system","name":"gatekeeper-webhook-service"}}},
  {"name":"check-ignore-label.gatekeeper.sh","failurePolicy":"Fail","clientConfig":{"service":{"namespace":"gatekeeper-system","name":"gatekeeper-webhook-service"}}}]}]}`

func TestGatekeeperOnly(t *testing.T) {
	r, _ := policyReadQuery(t, map[string]string{
		"GET /apis": `{"groups":[` +
			`{"name":"templates.gatekeeper.sh","preferredVersion":{"version":"v1"}},` +
			`{"name":"constraints.gatekeeper.sh","preferredVersion":{"version":"v1beta1"}}]}`,
		"GET /apis/templates.gatekeeper.sh/v1/constrainttemplates":                  gkTemplates,
		"GET /apis/constraints.gatekeeper.sh/v1beta1/k8srequiredlabels":             gkRequiredLabels,
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": gkWebhooks,
		"GET /apis/apps/v1/deployments":                                             gkDeployments,
		// k8sallowedrepos is not served: a template deleted while the read ran.
	}, nil)

	if !r.Installed || r.Kyverno || !r.Gatekeeper || r.Version != "" || r.Error != "" || r.GatekeeperError != "" {
		t.Fatalf("report flags %+v", r)
	}

	if len(r.Policies) != 2 {
		t.Fatalf("rows %+v", r.Policies)
	}

	// The constraint a pod does not enforce comes first; the dryrun one is Audit and ready.
	deny, dry := r.Policies[0], r.Policies[1]

	if deny.Name != "must-have-team" || deny.Engine != policyEngineGatekeeper || deny.Kind != "K8sRequiredLabels" ||
		deny.Action != policyActionEnforce || deny.Ready || deny.ReadyReason != "NotEnforced" || deny.ReadyMessage != "template not ingested" {
		t.Errorf("deny row %+v", deny)
	}

	if deny.Fail != 25 || !deny.FailingCapped || len(deny.Failing) != 2 || deny.AuditAt != 1792137600 ||
		deny.Failing[0].Namespace != "shop" || deny.Failing[0].Name != "cart" || deny.Failing[0].At != deny.AuditAt {
		t.Errorf("deny violations %+v", deny)
	}

	if deny.Description != "Requires resources to contain specified labels" {
		t.Errorf("description %q", deny.Description)
	}

	if len(deny.ByNamespace) != 1 || deny.ByNamespace[0] != (policyNSRow{Namespace: "shop", Fail: 1}) {
		t.Errorf("by namespace %+v", deny.ByNamespace)
	}

	if dry.Name != "must-have-owner" || dry.Action != policyActionAudit || !dry.Ready || dry.Fail != 0 || dry.FailingCapped || dry.AuditAt != 0 {
		t.Errorf("dryrun row %+v", dry)
	}

	if len(r.Namespaces) != 1 || r.Namespaces[0] != (policyNSRow{Namespace: "shop", Fail: 1}) {
		t.Errorf("namespaces %+v", r.Namespaces)
	}

	e, ok := r.engine(policyEngineGatekeeper)
	if !ok || e.Namespace != "gatekeeper-system" || len(e.Controllers) != 2 ||
		e.Controllers[0] != (policyController{Name: "gatekeeper-audit", Ready: 1, Desired: 1}) ||
		e.Controllers[1] != (policyController{Name: "gatekeeper-controller-manager", Ready: 2, Desired: 3}) {
		t.Errorf("engine %+v", e)
	}

	if e.Webhook != (policyWebhook{Name: gatekeeperWebhook, Present: true, FailurePolicy: "Ignore", Service: "gatekeeper-system/gatekeeper-webhook-service"}) {
		t.Errorf("webhook %+v", e.Webhook)
	}
}

func TestGatekeeperNextToKyverno(t *testing.T) {
	r, _ := policyReadQuery(t, map[string]string{
		"GET /apis": `{"groups":[` +
			`{"name":"kyverno.io","preferredVersion":{"version":"v1"}},` +
			`{"name":"templates.gatekeeper.sh","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/kyverno.io/v1/clusterpolicies":                                   policiesFixture(t, "clusterpolicies.json"),
		"GET /apis/kyverno.io/v1/policies":                                          policiesFixture(t, "policies.json"),
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": policiesFixture(t, "validatingwebhookconfigurations.json"),
		// No template created yet: the constraints group is not served.
		"GET /apis/templates.gatekeeper.sh/v1/constrainttemplates": `{"items":[]}`,
	}, map[string]func(*http.Request) string{
		"/apis/apps/v1/deployments": func(r *http.Request) string {
			if strings.Contains(r.URL.Query().Get("labelSelector"), "gatekeeper") {
				return gkDeployments
			}

			return policiesFixture(t, "deployments.json")
		},
	})

	if !r.Kyverno || !r.Gatekeeper || len(r.Engines) != 2 || r.Engines[0].Name != policyEngineKyverno || r.Engines[1].Name != policyEngineGatekeeper {
		t.Fatalf("engines %+v", r.Engines)
	}

	if r.GatekeeperError != "" || r.Error != "" {
		t.Errorf("errors %q / %q", r.Error, r.GatekeeperError)
	}

	for _, p := range r.Policies {
		if p.Engine != policyEngineKyverno {
			t.Errorf("row %s engine %q", p.Name, p.Engine)
		}
	}

	// The Kyverno webhook is not mistaken for Gatekeeper's, nor the other way round.
	if gk, _ := r.engine(policyEngineGatekeeper); gk.Webhook.Present || len(gk.Controllers) != 2 {
		t.Errorf("gatekeeper engine %+v", gk)
	}
}

// A constraint kind refused (a 403, unlike the 404 of a deleted template) is an error of the
// Gatekeeper part only; the engine still shows.
func TestGatekeeperConstraintsRefused(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[` +
			`{"name":"templates.gatekeeper.sh","preferredVersion":{"version":"v1"}},` +
			`{"name":"constraints.gatekeeper.sh","preferredVersion":{"version":"v1beta1"}}]}`,
		"GET /apis/templates.gatekeeper.sh/v1/constrainttemplates":                  gkTemplates,
		"GET /apis/apps/v1/deployments":                                             gkDeployments,
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": gkWebhooks,
	})
	f.answerWith("GET /apis/constraints.gatekeeper.sh/v1beta1/k8srequiredlabels", http.StatusForbidden,
		`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"forbidden","code":403}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readPolicies(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if !r.Gatekeeper || !strings.Contains(r.GatekeeperError, "forbidden") || r.Error != "" || len(r.Policies) != 0 {
		t.Fatalf("report %+v", r)
	}

	if e, ok := r.engine(policyEngineGatekeeper); !ok || len(e.Controllers) != 2 {
		t.Errorf("engine %+v", e)
	}
}

func TestGatekeeperAction(t *testing.T) {
	for in, want := range map[string]string{
		"": policyActionEnforce, "deny": policyActionEnforce, "dryrun": policyActionAudit,
		"warn": policyActionWarn, "scoped": policyActionMixed, "Deny": policyActionEnforce,
	} {
		if got := gatekeeperAction(in); got != want {
			t.Errorf("%q → %s, want %s", in, got, want)
		}
	}
}
