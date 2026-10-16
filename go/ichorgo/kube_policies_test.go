package ichorgo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func policiesFixture(t *testing.T, name string) string {
	t.Helper()

	return string(readFixture(t, "policies/"+name))
}

// policyRead is a readPolicies over one fake API, with the paths of the whole report.
func policyRead(t *testing.T, answers map[string]string) (policiesReport, *fakeKubeAPI) {
	t.Helper()

	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	report, err := readPolicies(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	return report, f
}

func TestPoliciesReport(t *testing.T) {
	page1 := policiesFixture(t, "policyreports_page1.json")
	page2 := policiesFixture(t, "policyreports_page2.json")

	r, f := policyReadQuery(t, map[string]string{
		"GET /apis": `{"groups":[` +
			`{"name":"kyverno.io","preferredVersion":{"version":"v1"}},` +
			`{"name":"wgpolicyk8s.io","preferredVersion":{"version":"v1alpha2"}}]}`,
		"GET /apis/kyverno.io/v1/clusterpolicies":                                   policiesFixture(t, "clusterpolicies.json"),
		"GET /apis/kyverno.io/v1/policies":                                          policiesFixture(t, "policies.json"),
		"GET /apis/apps/v1/deployments":                                             policiesFixture(t, "deployments.json"),
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": policiesFixture(t, "validatingwebhookconfigurations.json"),
		"GET /apis/wgpolicyk8s.io/v1alpha2/clusterpolicyreports":                    policiesFixture(t, "clusterpolicyreports.json"),
	}, map[string]func(*http.Request) string{
		"/apis/wgpolicyk8s.io/v1alpha2/policyreports": func(r *http.Request) string {
			if strings.Contains(r.URL.RawQuery, "continue=") {
				return page2
			}

			return page1
		},
	})

	if !r.Installed || r.Version != "v1" || r.Error != "" || r.ReportsError != "" {
		t.Fatalf("installed %v version %q error %q reportsError %q", r.Installed, r.Version, r.Error, r.ReportsError)
	}

	// The rows are ordered: not ready first, then Enforce before Audit, the most failing
	// next, then by name.
	if !slices.Equal(firstNames(r.Policies), []string{
		"verify-image-signatures", "disallow-privileged", "require-labels", "audit-image-registry",
	}) {
		t.Fatalf("order %v", firstNames(r.Policies))
	}

	// Rule-level failureAction wins over the deprecated spec-level one; when the rules
	// disagree the row reads Mixed.
	require := findPolicyRow(t, r.Policies, "", "require-labels")
	if require.Action != policyActionMixed {
		t.Errorf("require-labels action %q, want Mixed", require.Action)
	}

	if disallow := findPolicyRow(t, r.Policies, "", "disallow-privileged"); disallow.Action != policyActionEnforce {
		t.Errorf("disallow-privileged action %q", disallow.Action)
	}

	// The counts, from the results of both pages and both listing kinds (22 fails on
	// require-labels; the results of another engine are not counted).
	if require.Fail != 22 || require.Pass != 3 || require.Skip != 0 || require.ErrorCount != 0 {
		t.Errorf("require-labels counts %+v", require)
	}

	if require.Rules.Validate != 2 || require.Rules.Mutate != 1 || require.Rules.VerifyImages != 0 {
		t.Errorf("require-labels rules %+v", require.Rules)
	}

	if !slices.Equal(require.RuleNames, []string{"require-team", "require-owner", "mutate-team"}) {
		t.Errorf("require-labels rule names %v", require.RuleNames)
	}

	// clipUTF8 cuts at bytes; the ellipsis says it was cut.
	if len([]rune(require.Description)) != policyDescriptionMax+1 || !strings.HasSuffix(require.Description, "…") {
		t.Errorf("description %q (%d runes), want clipped at %d bytes + ellipsis", require.Description, len([]rune(require.Description)), policyDescriptionMax)
	}

	// A policy with no Ready condition yet still answers through the deprecated bool.
	if audit := findPolicyRow(t, r.Policies, "shop", "audit-image-registry"); !audit.Ready || audit.Action != policyActionAudit {
		t.Errorf("audit-image-registry %+v", audit)
	}

	if notReady := findPolicyRow(t, r.Policies, "", "verify-image-signatures"); notReady.Ready ||
		notReady.ReadyReason != "WebhookNotConfigured" {
		t.Errorf("verify-image-signatures %+v", notReady)
	}

	// The engine: its namespace, one Deployment down (0/1), and the webhook (Fail default).
	if len(r.Engines) != 1 || r.Engines[0].Name != policyEngineKyverno || r.Engines[0].Namespace != "kyverno" || len(r.Engines[0].Controllers) != 2 {
		t.Fatalf("engine %+v", r.Engines)
	}

	if r.Engines[0].Controllers[0].Name != "admission-controller" || r.Engines[0].Controllers[0].Ready != 0 || r.Engines[0].Controllers[0].Desired != 1 {
		t.Errorf("admission controller %+v", r.Engines[0].Controllers[0])
	}

	if !r.Engines[0].Webhook.Present || r.Engines[0].Webhook.FailurePolicy != "Fail" {
		t.Errorf("webhook %+v", r.Engines[0].Webhook)
	}

	// Namespace totals come from the namespaced reports only.
	if !slices.Equal(nsNames(r.Namespaces), []string{"billing", "shop"}) {
		t.Fatalf("namespaces %+v", r.Namespaces)
	}

	if ns := r.Namespaces[1]; ns.Fail != 22 || ns.Warn != 1 || ns.Pass != 1 {
		t.Errorf("shop totals %+v", ns)
	}

	if ns := r.Namespaces[0]; ns.Pass != 2 {
		t.Errorf("billing totals %+v", ns)
	}

	if r.ReportsRead != 4 || r.ReportsTruncated {
		t.Errorf("reports read %d truncated %v", r.ReportsRead, r.ReportsTruncated)
	}

	// 22 failing resources, only the newest 20 kept, the newest first (the second page's).
	if len(require.Failing) != policyFailingMax {
		t.Fatalf("failing kept %d, want %d", len(require.Failing), policyFailingMax)
	}

	if require.Failing[0].At != 1760000099 {
		t.Errorf("newest failing at %d, want 1760000099", require.Failing[0].At)
	}

	// The cluster report's failure reached the policy row, with its first resource.
	if disallow := findPolicyRow(t, r.Policies, "", "disallow-privileged"); disallow.Fail != 1 ||
		disallow.Failing[0].Kind != "Deployment" || disallow.Failing[0].Name != "legacy" {
		t.Errorf("disallow-privileged %+v", disallow)
	}

	// The same read must not repeat a listing: every collection asked exactly once.
	rec := f.recorded()
	for _, path := range []string{
		"/apis/kyverno.io/v1/clusterpolicies", "/apis/kyverno.io/v1/policies",
		"/apis/apps/v1/deployments", "/apis/wgpolicyk8s.io/v1alpha2/policyreports",
		"/apis/wgpolicyk8s.io/v1alpha2/clusterpolicyreports",
	} {
		if n := countRequests(rec, path); n == 0 {
			t.Errorf("%s never asked", path)
		}
	}
}

// TestPoliciesWithoutKyverno answers installed false and no error: reading the report must
// not fail because a cluster runs no Kyverno.
func TestPoliciesWithoutKyverno(t *testing.T) {
	r, _ := policyRead(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"cilium.io","preferredVersion":{"version":"v2"}}]}`,
	})

	if r.Installed || len(r.Policies) != 0 || r.Error != "" || r.ReportsError != "" {
		t.Fatalf("report %+v", r)
	}
}

// TestPoliciesPreferredVersionWithoutPolicies covers Kyverno 1.15: the group's preferred
// version is v2 but it serves no ClusterPolicy or Policy (they stay under v1; v2 serves
// updaterequests, admissionreports, cleanuppolicies). The lists fall back to the storage
// version, the report keeps the preferred version, and no error surfaces.
func TestPoliciesPreferredVersionWithoutPolicies(t *testing.T) {
	r, f := policyRead(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kyverno.io","preferredVersion":{"version":"v2"}}]}`,
		"GET /apis/kyverno.io/v1/clusterpolicies":                                   policiesFixture(t, "clusterpolicies.json"),
		"GET /apis/kyverno.io/v1/policies":                                          policiesFixture(t, "policies.json"),
		"GET /apis/apps/v1/deployments":                                             policiesFixture(t, "deployments.json"),
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": policiesFixture(t, "validatingwebhookconfigurations.json"),
	})

	if !r.Installed || r.Version != "v2" || r.Error != "" {
		t.Fatalf("installed %v version %q error %q", r.Installed, r.Version, r.Error)
	}

	if len(r.Policies) != 4 {
		t.Fatalf("policies %d, want 4", len(r.Policies))
	}

	// The preferred version was asked first, then the storage version answered: one
	// request per kind and version, no retry beyond the fallback.
	rec := f.recorded()
	for _, path := range []string{
		"/apis/kyverno.io/v2/clusterpolicies", "/apis/kyverno.io/v2/policies",
		"/apis/kyverno.io/v1/clusterpolicies", "/apis/kyverno.io/v1/policies",
	} {
		if n := countRequests(rec, path); n != 1 {
			t.Errorf("%s asked %d times, want 1", path, n)
		}
	}
}

// TestPoliciesPreferredVersionServesPolicies covers a future Kyverno whose preferred
// version serves the policies themselves: they are read from it and the storage version is
// never asked.
func TestPoliciesPreferredVersionServesPolicies(t *testing.T) {
	r, f := policyRead(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kyverno.io","preferredVersion":{"version":"v2"}}]}`,
		"GET /apis/kyverno.io/v2/clusterpolicies":                                   policiesFixture(t, "clusterpolicies.json"),
		"GET /apis/kyverno.io/v2/policies":                                          policiesFixture(t, "policies.json"),
		"GET /apis/apps/v1/deployments":                                             policiesFixture(t, "deployments.json"),
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": policiesFixture(t, "validatingwebhookconfigurations.json"),
	})

	if !r.Installed || r.Version != "v2" || r.Error != "" || len(r.Policies) != 4 {
		t.Fatalf("installed %v version %q error %q policies %d", r.Installed, r.Version, r.Error, len(r.Policies))
	}

	rec := f.recorded()
	if countRequests(rec, "/apis/kyverno.io/v1/clusterpolicies") != 0 ||
		countRequests(rec, "/apis/kyverno.io/v1/policies") != 0 {
		t.Errorf("storage version asked without a 404: %v", rec)
	}
}

// policyReadQuery is readPolicies over one query-aware fake API (see newQueryKubeAPI).
func policyReadQuery(t *testing.T, answers map[string]string, byQuery map[string]func(*http.Request) string) (policiesReport, *fakeKubeAPI) {
	t.Helper()

	f := newQueryKubeAPI(t, answers, byQuery)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	report, err := readPolicies(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	return report, f
}

// TestPoliciesReportsRefused keeps the policies when the reports are off limits (a 403): the
// counts stay at 0 and reportsError says so, the call does not fail.
func TestPoliciesReportsRefused(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[` +
			`{"name":"kyverno.io","preferredVersion":{"version":"v1"}},` +
			`{"name":"wgpolicyk8s.io","preferredVersion":{"version":"v1alpha2"}}]}`,
		"GET /apis/kyverno.io/v1/clusterpolicies":                                   policiesFixture(t, "clusterpolicies.json"),
		"GET /apis/kyverno.io/v1/policies":                                          policiesFixture(t, "policies.json"),
		"GET /apis/apps/v1/deployments":                                             policiesFixture(t, "deployments.json"),
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": policiesFixture(t, "validatingwebhookconfigurations.json"),
	})
	f.answerWith("GET /apis/wgpolicyk8s.io/v1alpha2/policyreports", http.StatusForbidden,
		`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"forbidden","code":403}`)
	f.answerWith("GET /apis/wgpolicyk8s.io/v1alpha2/clusterpolicyreports", http.StatusForbidden,
		`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"forbidden","code":403}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readPolicies(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if !r.Installed || len(r.Policies) != 4 || r.ReportsError == "" || r.Error != "" {
		t.Fatalf("installed %v, %d policies, reportsError %q, error %q", r.Installed, len(r.Policies), r.ReportsError, r.Error)
	}

	if require := findPolicyRow(t, r.Policies, "", "require-labels"); require.Fail != 0 {
		t.Errorf("require-labels fails %d, want 0", require.Fail)
	}
}

func TestPoliciesDemo(t *testing.T) {
	demo := demoPolicies()
	out, err := toJSON(demo)
	if err != nil {
		t.Fatal(err)
	}

	var got policiesReport
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if !got.Installed || got.Version != "v1" || got.Engines[0].Namespace != "kyverno" || !got.Engines[0].Webhook.Present {
		t.Fatalf("demo report %+v", got)
	}

	if len(got.Policies) != 6 || len(got.Engines) != 2 || !got.Kyverno || !got.Gatekeeper || got.Namespaces[0].Namespace != "shop" || got.ReportsRead != 6 {
		t.Errorf("demo policies %+v", got.Policies)
	}

	// The demo must be shaped the way the reader writes rows: a policy not ready first.
	if got.Policies[0].Name != "verify-image-signatures" || got.Policies[0].Ready {
		t.Errorf("demo order: %+v", firstNames(got.Policies))
	}

	// The whole list in the reader's order (not ready, Enforce, Mixed, Audit): the apps show it as is.
	if !slices.IsSortedFunc(got.Policies, policyRowLess) {
		t.Errorf("demo not in reader order: %v", firstNames(got.Policies))
	}

	// The policies' per-namespace counts add up to the namespace totals, as a live read's do.
	sums := map[string]policyNSRow{}
	for _, p := range got.Policies {
		for _, n := range p.ByNamespace {
			s := sums[n.Namespace]
			s.Namespace, s.Pass, s.Fail, s.Warn, s.ErrorCount = n.Namespace, s.Pass+n.Pass, s.Fail+n.Fail, s.Warn+n.Warn, s.ErrorCount+n.ErrorCount
			sums[n.Namespace] = s
		}
	}

	for _, ns := range got.Namespaces {
		if sums[ns.Namespace] != ns {
			t.Errorf("namespace %s: policies sum to %+v, totals say %+v", ns.Namespace, sums[ns.Namespace], ns)
		}
	}
}

// TestKyvernoGroupsSupported keeps the Integration-request demo honest: Kyverno is listed as
// read, and only MetallB (the next family to plan) stays as a request.
func TestKyvernoGroupsSupported(t *testing.T) {
	d := demoIntegrations()

	if !slices.Contains(d.Supported, groupKyverno) || !slices.Contains(d.Supported, groupPolicyReport) {
		t.Errorf("supported %v", d.Supported)
	}

	if !slices.IsSorted(d.Supported) {
		t.Errorf("supported not sorted: %v", d.Supported)
	}

	var ids []string
	for _, fam := range d.Families {
		ids = append(ids, fam.ID)
		if fam.ID == "kyverno.io" {
			t.Errorf("kyverno still an unsupported family: %v", fam.Groups)
		}
	}

	// MetalLB, KEDA, KubeVirt, Tailscale and Cluster API are supported too: Kueue is the family
	// the demo still asks for.
	if !slices.Equal(ids, []string{"kueue.x-k8s.io"}) {
		t.Errorf("families %v", ids)
	}
}

func findPolicyRow(t *testing.T, rows []policyRow, namespace, name string) policyRow {
	t.Helper()

	for _, p := range rows {
		if p.Namespace == namespace && p.Name == name {
			return p
		}
	}

	t.Fatalf("no policy %s/%s", namespace, name)

	return policyRow{}
}

func nsNames(rows []policyNSRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Namespace)
	}

	return out
}

func firstNames(rows []policyRow) []string {
	out := make([]string, 0, len(rows))
	for _, p := range rows {
		out = append(out, p.Name)
	}

	return out
}

func countRequests(rec []fakeKubeRequest, path string) int {
	n := 0
	for _, r := range rec {
		if r.path == path {
			n++
		}
	}

	return n
}

// newQueryKubeAPI is the fake API when one path needs answers by query: the shared fake
// matches "method path" alone, which cannot page (every page of a list has the same path).
// The byQuery functions are asked after the plain answers miss; a miss anywhere answers 404.
func newQueryKubeAPI(t *testing.T, answers map[string]string, byQuery map[string]func(*http.Request) string) *fakeKubeAPI {
	t.Helper()

	f := &fakeKubeAPI{answers: answers}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer, ok := answers[r.Method+" "+r.URL.Path]
		if !ok {
			// has, not ok: shadowing the outer ok would answer 404 after a hit.
			if fn, has := byQuery[r.URL.Path]; has {
				answer, ok = fn(r), true
			}
		}

		if !ok && r.URL.Path == "/version" {
			answer, ok = `{"gitVersion":"v1.34.0"}`, true
		}

		f.mu.Lock()
		f.requests = append(f.requests, fakeKubeRequest{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), "", r.URL.RawQuery})
		f.mu.Unlock()

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"NotFound","message":"`+r.URL.Path+` not found"}`)

			return
		}

		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(f.Close)

	return f
}

// TestPolicyCountsByNamespace: a policy's results are also counted per namespace, the
// namespaced report's own or, in a ClusterPolicyReport, the resource's; a cluster-scoped
// resource counts in the totals only.
func TestPolicyCountsByNamespace(t *testing.T) {
	decode := func(s string) []policyReportObject {
		var out []policyReportObject
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			t.Fatal(err)
		}

		return out
	}

	reports := decode(`[
		{"metadata":{"namespace":"shop"},"results":[
			{"source":"kyverno","policy":"require-labels","result":"fail","resources":[{"kind":"Pod","namespace":"shop","name":"a"}]},
			{"source":"kyverno","policy":"require-labels","result":"fail","resources":[{"kind":"Pod","namespace":"shop","name":"b"}]},
			{"source":"kyverno","policy":"shop/local","result":"pass","resources":[{"kind":"Pod","namespace":"shop","name":"a"}]}]},
		{"metadata":{"namespace":"media"},"results":[
			{"source":"kyverno","policy":"require-labels","result":"pass","resources":[{"kind":"Pod","namespace":"media","name":"c"}]}]}]`)
	cluster := decode(`[
		{"metadata":{},"results":[
			{"source":"kyverno","policy":"require-labels","result":"warn","resources":[{"kind":"Deployment","namespace":"billing","name":"d"}]},
			{"source":"kyverno","policy":"require-labels","result":"fail","resources":[{"kind":"Namespace","name":"billing"}]},
			{"source":"other","policy":"require-labels","result":"fail","resources":[{"kind":"Pod","namespace":"billing","name":"e"}]}]}]`)

	rows := []policyRow{{Name: "require-labels"}, {Namespace: "shop", Name: "local"}}

	var out policiesReport

	aggregatePolicyReports(rows, reports, cluster, &out)

	labels := rows[0]
	if labels.Fail != 3 || labels.Pass != 1 || labels.Warn != 1 {
		t.Fatalf("require-labels totals fail %d pass %d warn %d", labels.Fail, labels.Pass, labels.Warn)
	}

	want := []policyNSRow{{Namespace: "billing", Warn: 1}, {Namespace: "media", Pass: 1}, {Namespace: "shop", Fail: 2}}
	if !slices.Equal(labels.ByNamespace, want) {
		t.Errorf("require-labels by namespace %+v, want %+v", labels.ByNamespace, want)
	}

	if got := rows[1].ByNamespace; !slices.Equal(got, []policyNSRow{{Namespace: "shop", Pass: 1}}) {
		t.Errorf("local by namespace %+v", got)
	}
}
