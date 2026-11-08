package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	cpolPath = "/apis/kyverno.io/v1/clusterpolicies/require-labels"
	polPath  = "/apis/kyverno.io/v1/namespaces/shop/policies/registry"
)

// Two validate rules (one with its own failureAction) and a mutate rule.
const cpolAudit = `{"metadata":{"name":"require-labels","resourceVersion":"41"},"spec":{"validationFailureAction":"Audit","rules":[
 {"name":"require-team","validate":{"failureAction":"Audit","message":"team"}},
 {"name":"mutate-team","mutate":{}},
 {"name":"require-owner","validate":{"message":"owner"}}]},
 "status":{"conditions":[{"type":"Ready","status":"True"}]}}`

const cpolEnforce = `{"metadata":{"name":"require-labels","resourceVersion":"42"},"spec":{"validationFailureAction":"Enforce","rules":[
 {"name":"require-team","validate":{"failureAction":"Enforce","message":"team"}},
 {"name":"mutate-team","mutate":{}},
 {"name":"require-owner","validate":{"failureAction":"Enforce","message":"owner"}}]},
 "status":{"conditions":[{"type":"Ready","status":"True"}]}}`

const ssarAllowed = `{"status":{"allowed":true}}`

// policyActionAPI serves one policy at path; a PATCH there makes the next GET answer after.
func policyActionAPI(t *testing.T, path, before, after, ssar string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":        `{"groups":[{"name":"kyverno.io","preferredVersion":{"version":"v2"}}]}`,
		"POST " + ssarPath: ssar,
		"GET " + path:      before,
		"PATCH " + path:    `{}`,
	})
	f.beforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodPatch {
			f.mu.Lock()
			f.answers["GET "+path] = after
			f.mu.Unlock()
		}
	}

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func patchRequest(t *testing.T, f *fakeKubeAPI) fakeKubeRequest {
	t.Helper()

	for _, r := range f.recorded() {
		if r.method == http.MethodPatch {
			return r
		}
	}

	t.Fatal("no PATCH sent")

	return fakeKubeRequest{}
}

func TestPolicySetActionPatchesEveryValidateRule(t *testing.T) {
	f, k := policyActionAPI(t, cpolPath, cpolAudit, cpolEnforce, ssarAllowed)

	got, err := setPolicyAction(context.Background(), k, "", "require-labels", policyActionEnforce)
	if err != nil {
		t.Fatal(err)
	}

	// The re-read reports the new action: every rule now reads Enforce.
	if got != (policyActionResult{Name: "require-labels", Action: policyActionEnforce, Ready: true, RulesPatched: 2}) {
		t.Errorf("result %+v", got)
	}

	p := patchRequest(t, f)
	if p.path != cpolPath || p.contentType != "application/json-patch+json" {
		t.Fatalf("patch %+v", p)
	}

	want := `[{"op":"test","path":"/metadata/resourceVersion","value":"41"},` +
		`{"op":"add","path":"/spec/validationFailureAction","value":"Enforce"},` +
		`{"op":"add","path":"/spec/rules/0/validate/failureAction","value":"Enforce"},` +
		`{"op":"add","path":"/spec/rules/2/validate/failureAction","value":"Enforce"}]`
	if p.body != want {
		t.Errorf("patch body\n got %s\nwant %s", p.body, want)
	}

	// The access review asked for patch on that very ClusterPolicy.
	var review ssarRequest
	for _, r := range f.recorded() {
		if r.method == http.MethodPost {
			_ = json.Unmarshal([]byte(r.body), &review)
		}
	}

	if a := review.Spec.ResourceAttributes; a.Verb != "patch" || a.Group != "kyverno.io" || a.Resource != "clusterpolicies" || a.Name != "require-labels" || a.Namespace != "" {
		t.Errorf("access review %+v", a)
	}
}

// Image checks carry their own failureAction: they are switched too, and read back.
func TestPolicySetActionSwitchesImageChecks(t *testing.T) {
	before := `{"metadata":{"name":"verify-images","resourceVersion":"3"},"spec":{"rules":[
	 {"name":"sig","verifyImages":[{"imageReferences":["*"],"failureAction":"Enforce"},{"imageReferences":["ghcr.io/*"]}]}]}}`
	after := `{"metadata":{"name":"verify-images","resourceVersion":"4"},"spec":{"rules":[
	 {"name":"sig","verifyImages":[{"imageReferences":["*"],"failureAction":"Audit"},{"imageReferences":["ghcr.io/*"],"failureAction":"Audit"}]}]}}`
	path := "/apis/kyverno.io/v1/clusterpolicies/verify-images"
	f, k := policyActionAPI(t, path, before, after, ssarAllowed)

	got, err := setPolicyAction(context.Background(), k, "", "verify-images", policyActionAudit)
	if err != nil || got.RulesPatched != 1 || got.Action != policyActionAudit {
		t.Fatalf("result %+v, %v", got, err)
	}

	body := patchRequest(t, f).body
	for _, want := range []string{`"/spec/rules/0/verifyImages/0/failureAction","value":"Audit"`, `"/spec/rules/0/verifyImages/1/failureAction","value":"Audit"`} {
		if !strings.Contains(body, want) {
			t.Errorf("patch lacks %s: %s", want, body)
		}
	}

	// Read as Kyverno writes it, an image check's Enforce wins over a policy-level Audit.
	var o kyvernoPolicyObject
	if err := json.Unmarshal([]byte(before), &o); err != nil {
		t.Fatal(err)
	}

	if row := kyvernoPolicyOf(o); row.Action != policyActionMixed || row.Rules.VerifyImages != 1 {
		t.Errorf("before %+v", row)
	}
}

func TestPolicySetActionNamespacedPolicy(t *testing.T) {
	pol := `{"metadata":{"name":"registry","namespace":"shop","resourceVersion":"7"},"spec":{"rules":[{"name":"r","validate":{}}]}}`
	f, k := policyActionAPI(t, polPath, pol, pol, ssarAllowed)

	got, err := setPolicyAction(context.Background(), k, "shop", "registry", policyActionAudit)
	if err != nil || got.Namespace != "shop" || got.RulesPatched != 1 {
		t.Fatalf("result %+v, %v", got, err)
	}

	if p := patchRequest(t, f); p.path != polPath || !strings.Contains(p.body, `"/spec/rules/0/validate/failureAction","value":"Audit"`) {
		t.Errorf("patch %+v", p)
	}
}

func TestPolicySetActionRefusedByRBAC(t *testing.T) {
	f, k := policyActionAPI(t, cpolPath, cpolAudit, cpolEnforce, `{"status":{"allowed":false,"reason":"no RBAC policy matched"}}`)

	_, err := setPolicyAction(context.Background(), k, "", "require-labels", policyActionEnforce)

	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != 403 || !strings.Contains(err.Error(), "no RBAC policy matched") {
		t.Fatalf("got %v", err)
	}

	for _, r := range f.recorded() {
		if r.method == http.MethodPatch {
			t.Fatal("a refused switch must not patch")
		}
	}
}

func TestKubePolicySetActionRefusals(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubePolicySetAction(demo, "", "", "", "require-labels", "Warn"); err == nil || !strings.Contains(err.Error(), "Audit or Enforce") {
		t.Errorf("invalid action: %v", err)
	}

	if _, err := KubePolicySetAction(demo, "", "", "", "require-labels", policyActionAudit); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: %v", err)
	}

	entries := readAudit(t, "", "")
	if len(entries) != 2 || entries[0].Action != "policy-set-action" {
		t.Fatalf("audit %+v", entries)
	}
}

func TestPolicyRowsSayWhoAppliesThem(t *testing.T) {
	for _, c := range []struct {
		labels, annotations map[string]string
		want                string
	}{
		{map[string]string{"argocd.argoproj.io/instance": "policies"}, nil, ownerViaArgo},
		{nil, map[string]string{"argocd.argoproj.io/tracking-id": "policies:kyverno.io/ClusterPolicy:/require-labels"}, ownerViaArgo},
		{map[string]string{"kustomize.toolkit.fluxcd.io/name": "policies", "kustomize.toolkit.fluxcd.io/namespace": "flux-system"}, nil, ownerViaFlux},
		{nil, map[string]string{"meta.helm.sh/release-name": "kyverno-policies", "meta.helm.sh/release-namespace": "kyverno"}, ""},
		{nil, nil, ""},
	} {
		if got := gitopsManager(c.labels, c.annotations); got != c.want {
			t.Errorf("%v %v → %q, want %q", c.labels, c.annotations, got, c.want)
		}
	}
}
