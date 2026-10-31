package helm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube/kubetest"
)

const (
	helmTestSecrets  = "/api/v1/namespaces/web/secrets"
	helmTestCM       = "/api/v1/namespaces/web/configmaps/site"
	helmTestDeploy   = "/apis/apps/v1/namespaces/web/deployments/site"
	helmTestService  = "/api/v1/namespaces/web/services/site-extra"
	helmTestKept     = "/api/v1/namespaces/web/configmaps/site-keep"
	helmTestNewPod   = "/api/v1/namespaces/web/serviceaccounts/site"
	helmTestDiscover = `{"groupVersion":"v1","resources":[
	  {"name":"configmaps","kind":"ConfigMap","namespaced":true,"verbs":["get","list"]},
	  {"name":"services","kind":"Service","namespaced":true,"verbs":["get","list"]},
	  {"name":"serviceaccounts","kind":"ServiceAccount","namespaced":true,"verbs":["get","list"]},
	  {"name":"pods/log","kind":"Pod","namespaced":true}]}`
)

// helmTestRelease is revision version of release site with manifest, as Helm stores it.
func helmTestRelease(version int, status, chartVersion, manifest string, extra map[string]any) map[string]any {
	rel := map[string]any{
		"name": "site", "namespace": "web", "version": version,
		"info":     map[string]any{"status": status, "first_deployed": "2026-09-01T10:00:00Z", "last_deployed": "2026-10-01T10:00:00Z", "description": "Upgrade complete"},
		"chart":    map[string]any{"metadata": map[string]any{"name": "site", "version": chartVersion, "appVersion": "2.0"}},
		"config":   map[string]any{"replicas": 12345678901234567},
		"manifest": manifest,
	}

	for k, v := range extra {
		rel[k] = v
	}

	return rel
}

const helmTestManifestV1 = `---
# Source: site/templates/cm.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: site
data:
  a: "1"
---
# Source: site/templates/deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: site
  labels: {app: site}
  labels: {app: site, tier: web}
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: web
        image: site:v1
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: site
`

const helmTestManifestV2 = `apiVersion: v1
kind: ConfigMap
metadata:
  name: site
data:
  a: "2"
  b: "x"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: site
  labels: {app: site, tier: web}
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: web
        image: site:v2
---
apiVersion: v1
kind: Service
metadata:
  name: site-extra
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: site-keep
  annotations: {helm.sh/resource-policy: keep}
`

func helmRollbackAPI(t *testing.T, extraV1 map[string]any) *kubetest.FakeAPI {
	t.Helper()

	// After a failed upgrade Helm leaves the previous revision deployed.
	v1 := helmSecretData(t, helmTestRelease(1, "deployed", "1.0.0", helmTestManifestV1, extraV1))
	v2 := helmSecretData(t, helmTestRelease(2, "failed", "1.1.0", helmTestManifestV2, nil))

	return kubetest.New(t, map[string]string{
		"GET " + helmTestSecrets: `{"kind":"Table","apiVersion":"meta.k8s.io/v1","columnDefinitions":[],"rows":[` +
			helmRow("sh.helm.release.v1.site.v1", "site", 1, "deployed") + "," +
			helmRow("site-v1-copy", "site", 1, "deployed") + "," +
			helmRow("sh.helm.release.v1.site.v2", "site", 2, "failed") + `]}`,
		"GET " + helmTestSecrets + "/sh.helm.release.v1.site.v1": `{"metadata":{"name":"sh.helm.release.v1.site.v1"},"data":{"release":"` + v1 + `"}}`,
		"GET " + helmTestSecrets + "/sh.helm.release.v1.site.v2": `{"metadata":{"name":"sh.helm.release.v1.site.v2","resourceVersion":"7","labels":{"owner":"helm","status":"failed"}},"data":{"release":"` + v2 + `"}}`,
		// The fake does not keep what is posted: the new revision reads back as a copy of v1.
		"GET " + helmTestSecrets + "/sh.helm.release.v1.site.v3": `{"metadata":{"name":"sh.helm.release.v1.site.v3","labels":{"owner":"helm","status":"pending-rollback"}},"data":{"release":"` + v1 + `"}}`,
		"POST " + helmTestSecrets:                                `{}`,
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v1": `{}`,
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v3": `{}`,
		"GET /api/v1":       helmTestDiscover,
		"GET /apis/apps/v1": `{"groupVersion":"apps/v1","resources":[{"name":"deployments","kind":"Deployment","namespaced":true,"verbs":["get","list"]}]}`,
		"GET " + helmTestCM: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"site","namespace":"web","resourceVersion":"5","annotations":{"meta.helm.sh/release-name":"site"}},"data":{"a":"2","b":"x"}}`,
		"GET " + helmTestDeploy: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"site","namespace":"web","labels":{"app":"site","tier":"web"}},
		  "spec":{"replicas":1,"revisionHistoryLimit":10,"template":{"spec":{"containers":[{"name":"web","image":"site:v2","imagePullPolicy":"IfNotPresent"}]}}},"status":{"replicas":1}}`,
		"GET " + helmTestService:                      `{"apiVersion":"v1","kind":"Service","metadata":{"name":"site-extra","namespace":"web"}}`,
		"PATCH " + helmTestCM:                         `{}`,
		"PATCH " + helmTestDeploy:                     `{}`,
		"DELETE " + helmTestService:                   `{}`,
		"POST /api/v1/namespaces/web/serviceaccounts": `{}`,
	})
}

func TestHelmRollbackPlanAndRun(t *testing.T) {
	f := helmRollbackAPI(t, nil)

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, Options{})
	if err != nil {
		t.Fatal(err)
	}

	p := rb.plan
	if p.From != 2 || p.To != 1 || p.ReleaseNamespace != "web" || p.FromChart != "site-1.1.0" || p.ToChart != "site-1.0.0" || len(p.Blockers) != 0 || p.FluxOwner != "" || p.Unchanged != 0 {
		t.Fatalf("plan %+v", p)
	}

	want := []Change{
		{Action: ChangeKeep, Kind: "ConfigMap", Namespace: "web", Name: "site-keep"},
		{Action: ChangeCreate, Kind: "ServiceAccount", Namespace: "web", Name: "site"},
		{Action: ChangeUpdate, Kind: "ConfigMap", Namespace: "web", Name: "site"},
		{Action: ChangeUpdate, Kind: "Deployment", Namespace: "web", Name: "site"},
		{Action: ChangeDelete, Kind: "Service", Namespace: "web", Name: "site-extra"},
	}
	if !reflect.DeepEqual(p.Changes, want) {
		t.Fatalf("changes:\n got  %+v\n want %+v", p.Changes, want)
	}

	// The plan only dry-runs.
	for _, r := range f.Recorded() {
		if r.Method != "GET" && !strings.Contains(r.Query, "dryRun=All") {
			t.Fatalf("plan wrote: %+v", r)
		}
	}

	f.ForgetRequests()

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if _, err := runRollback(context.Background(), k, rb, now); err != nil {
		t.Fatal(err)
	}

	var writes []string

	bodies := map[string]string{}

	for _, r := range f.Recorded() {
		if r.Method != "GET" {
			writes = append(writes, r.Method+" "+r.Path)
			bodies[r.Method+" "+r.Path] = r.Body
		}
	}

	wantWrites := []string{
		"POST " + helmTestSecrets,
		"POST /api/v1/namespaces/web/serviceaccounts",
		"PATCH " + helmTestCM,
		"PATCH " + helmTestDeploy,
		"DELETE " + helmTestService,
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v1",
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v3",
	}
	if !reflect.DeepEqual(writes, wantWrites) {
		t.Fatalf("writes:\n got  %v\n want %v", writes, wantWrites)
	}

	if got := bodies["POST /api/v1/namespaces/web/serviceaccounts"]; !strings.Contains(got, `"meta.helm.sh/release-name":"site"`) || !strings.Contains(got, `"app.kubernetes.io/managed-by":"Helm"`) {
		t.Errorf("created without Helm's ownership: %s", got)
	}

	if got := bodies["PATCH "+helmTestCM]; got != `{"data":{"a":"1","b":null},"metadata":{"annotations":{"meta.helm.sh/release-namespace":"web"},"labels":{"app.kubernetes.io/managed-by":"Helm"}}}` {
		t.Errorf("configmap patch %s", got)
	}

	if got := bodies["PATCH "+helmTestDeploy]; !strings.HasPrefix(got, `{"metadata":{"annotations":{"meta.helm.sh/release-name":"site","meta.helm.sh/release-namespace":"web"},"labels":{"app.kubernetes.io/managed-by":"Helm"}},"spec":{"template":{"spec":{"containers":[{"image":"site:v1","name":"web"}]}}}}`) {
		t.Errorf("deployment patch %s", got)
	}

	newRevision := helmSecretPayload(t, bodies["POST "+helmTestSecrets], "data", "release")
	info := newRevision["info"].(map[string]any)

	if newRevision["version"] != json.Number("3") || info["status"] != "pending-rollback" || info["description"] != "Rollback to 1" ||
		info["first_deployed"] != "2026-09-01T10:00:00Z" || newRevision["manifest"] != helmTestManifestV1 {
		t.Errorf("new revision %v", newRevision)
	}

	// Values keep their exact numbers.
	if c := newRevision["config"].(map[string]any); c["replicas"] != json.Number("12345678901234567") {
		t.Errorf("config %v", c)
	}

	if !strings.Contains(bodies["POST "+helmTestSecrets], `"version":"3"`) || !strings.Contains(bodies["POST "+helmTestSecrets], `"helm.sh/release.v1"`) {
		t.Errorf("secret %s", bodies["POST "+helmTestSecrets])
	}

	// v1 was deployed: superseded now; the failed v2 is left as it is.
	if p := helmSecretPayload(t, bodies["PUT "+helmTestSecrets+"/sh.helm.release.v1.site.v1"], "data", "release"); p["info"].(map[string]any)["status"] != "superseded" {
		t.Errorf("v1 not superseded: %v", p["info"])
	}

	v3 := bodies["PUT "+helmTestSecrets+"/sh.helm.release.v1.site.v3"]
	if p := helmSecretPayload(t, v3, "data", "release"); !strings.Contains(v3, `"status":"deployed"`) || p["info"].(map[string]any)["status"] != "deployed" {
		t.Errorf("v3 %s", v3)
	}
}

// helmSecretPayload decodes the release payload of a Secret request body.
func helmSecretPayload(t *testing.T, body string, path ...string) map[string]any {
	t.Helper()

	var secret map[string]any
	if err := json.Unmarshal([]byte(body), &secret); err != nil {
		t.Fatalf("%v: %s", err, body)
	}

	var v any = secret
	for _, key := range path {
		v = v.(map[string]any)[key]
	}

	payload, _, err := decodePayload(v.(string))
	if err != nil {
		t.Fatal(err)
	}

	return payload
}

func TestHelmRollbackBlockers(t *testing.T) {
	f := helmRollbackAPI(t, nil)

	k := Env{Client: f.KubeClient(t)}

	for _, rev := range []int{2, 3} {
		if _, err := prepareRollback(context.Background(), k, "web", "site", rev, Options{}); err == nil {
			t.Errorf("revision %d accepted", rev)
		}
	}
}

// A release Flux manages is suspended first, through go/kube/flux: a refusal stops the rollback
// before anything is recorded.
func TestRollbackSuspendsTheFluxOwnerFirst(t *testing.T) {
	const hr = "/apis/helm.toolkit.fluxcd.io/v2/namespaces/web/helmreleases/site"

	f := kubetest.New(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}}]}`,
		"GET " + hr: `{"metadata":{"name":"site","namespace":"web","resourceVersion":"7"},"spec":{}}`,
	})
	f.AnswerWith("PATCH "+hr, http.StatusForbidden, `{"kind":"Status","code":403,"reason":"Forbidden","message":"no"}`)

	_, err := runRollback(context.Background(), Env{Client: f.KubeClient(t)}, &rollback{plan: Plan{FluxOwner: "web/site"}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "suspend the HelmRelease web/site first") {
		t.Fatalf("got %v", err)
	}

	got := f.Recorded()
	if last := got[len(got)-1]; last.Method != http.MethodPatch || last.Path != hr || !strings.Contains(last.Body, `"suspend":true`) {
		t.Fatalf("last request %+v", last)
	}
}

func TestHelmFluxOwner(t *testing.T) {
	objs, err := parseManifest("kind: Deployment\nmetadata:\n  name: a\n  labels:\n    helm.toolkit.fluxcd.io/name: podinfo\n    helm.toolkit.fluxcd.io/namespace: flux-system\n")
	if err != nil {
		t.Fatal(err)
	}

	if got := fluxOwner("web", []object{{Body: objs[0]}}); got != "flux-system/podinfo" {
		t.Fatalf("got %q", got)
	}
}

func TestHelmRollbackKeepsWhatLiveSaysToKeep(t *testing.T) {
	f := helmRollbackAPI(t, nil)

	f.Mu.Lock()
	f.Answers["GET "+helmTestService] = `{"apiVersion":"v1","kind":"Service","metadata":{"name":"site-extra","namespace":"web","annotations":{"helm.sh/resource-policy":"keep"}}}`
	f.Mu.Unlock()

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 1, Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range rb.plan.Changes {
		if c.Name == "site-extra" && c.Action != ChangeKeep {
			t.Fatalf("live keep ignored: %+v", c)
		}
	}

	for _, s := range rb.steps {
		if s.obj.Name == "site-extra" {
			t.Fatal("a kept object is a step")
		}
	}
}

func TestHelmTargetRef(t *testing.T) {
	refs := []SecretRef{{Revision: 5}, {Revision: 3}}

	if _, err := pickTargetRef(refs, 0); err == nil {
		t.Error("revision 0 took 3, not 4")
	}

	if ref, err := pickTargetRef(refs, 3); err != nil || ref.Revision != 3 {
		t.Errorf("got %+v %v", ref, err)
	}
}

func TestHelmObjectsGoToTheReleaseNamespace(t *testing.T) {
	f := kubetest.New(t, map[string]string{"GET /api/v1": helmTestDiscover})

	k := Env{Client: f.KubeClient(t)}

	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: b, namespace: other}\n"

	objs, err := objects(context.Background(), newKindResolver(k.Client), manifest, "monitoring")
	if err != nil || len(objs) != 2 {
		t.Fatalf("%+v %v", objs, err)
	}

	if objs[0].Path != "/api/v1/namespaces/monitoring/configmaps/a" || objs[1].Path != "/api/v1/namespaces/other/configmaps/b" {
		t.Fatalf("paths %q %q", objs[0].Path, objs[1].Path)
	}
}

// helmRollbackWrites prepares the rollback of site to v1 with optionsJSON on f, runs it and
// returns the plan, the run's writes in order and its error.
func helmRollbackWrites(t *testing.T, f *kubetest.FakeAPI, optionsJSON string) (Plan, []kubetest.Request, error) {
	t.Helper()

	opts, err := ParseRollbackOptions(optionsJSON)
	if err != nil {
		t.Fatal(err)
	}

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, opts)
	if err != nil {
		t.Fatal(err)
	}

	f.ForgetRequests()

	pruneErr, runErr := runRollback(context.Background(), k, rb, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC))

	var writes []kubetest.Request

	for _, r := range f.Recorded() {
		if r.Method != "GET" {
			writes = append(writes, r)
		}
	}

	return rb.plan, writes, errors.Join(runErr, pruneErr)
}

func writeKeys(writes []kubetest.Request) []string {
	out := make([]string, 0, len(writes))
	for _, w := range writes {
		out = append(out, w.Method+" "+w.Path)
	}

	return out
}

func TestHelmRollbackForceReplaces(t *testing.T) {
	f := helmRollbackAPI(t, nil)
	f.AnswerWith("GET "+helmTestDeploy, 200, `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"site","namespace":"web","resourceVersion":"9","labels":{"app":"site","tier":"web"}},
	  "spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"web","image":"site:v2"}]}}}}`)
	f.AnswerWith("PUT "+helmTestCM, 200, `{}`)
	f.AnswerWith("PUT "+helmTestDeploy, 200, `{}`)

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}

	if !rb.plan.Force || rb.plan.Workloads != 1 || len(rb.plan.Prune) != 0 || len(rb.plan.Blockers) != 0 {
		t.Fatalf("plan %+v", rb.plan)
	}

	actions := map[string]string{}
	for _, c := range rb.plan.Changes {
		actions[c.Kind+"/"+c.Name] = c.Action
	}

	if actions["ConfigMap/site"] != ChangeReplace || actions["Deployment/site"] != ChangeReplace || actions["ServiceAccount/site"] != ChangeCreate {
		t.Fatalf("changes %v", actions)
	}

	dryPuts := 0

	for _, r := range f.Recorded() {
		if r.Method == "PUT" && (r.Path == helmTestCM || r.Path == helmTestDeploy) {
			if !strings.Contains(r.Query, "dryRun=All") {
				t.Fatalf("plan replaced for real: %+v", r)
			}

			dryPuts++
		}
	}

	if dryPuts != 2 {
		t.Fatalf("%d dry-run PUTs", dryPuts)
	}

	_, writes, runErr := helmRollbackWrites(t, f, `{"force":true}`)
	if runErr != nil {
		t.Fatal(runErr)
	}

	want := []string{
		"POST " + helmTestSecrets,
		"POST /api/v1/namespaces/web/serviceaccounts",
		"PUT " + helmTestCM,
		"PUT " + helmTestDeploy,
		"DELETE " + helmTestService,
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v1",
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v3",
	}
	if got := writeKeys(writes); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes:\n got  %v\n want %v", got, want)
	}

	for _, w := range writes[2:4] {
		if strings.Contains(w.Query, "dryRun") || w.ContentType != "application/json" {
			t.Errorf("replace %s?%s (%s)", w.Path, w.Query, w.ContentType)
		}

		var body map[string]any
		if err := json.Unmarshal([]byte(w.Body), &body); err != nil {
			t.Fatal(err)
		}

		meta := body["metadata"].(map[string]any)
		annotations, _ := meta["annotations"].(map[string]any)

		if meta["resourceVersion"] == nil || annotations["meta.helm.sh/release-name"] != "site" || annotations["meta.helm.sh/release-namespace"] != "web" ||
			meta["labels"].(map[string]any)["app.kubernetes.io/managed-by"] != "Helm" {
			t.Errorf("replace body %s", w.Body)
		}
	}

	if !strings.Contains(writes[2].Body, `"resourceVersion":"5"`) || !strings.Contains(writes[2].Body, `"data":{"a":"1"}`) {
		t.Errorf("configmap %s", writes[2].Body)
	}

	if !strings.Contains(writes[3].Body, `"resourceVersion":"9"`) || !strings.Contains(writes[3].Body, `"image":"site:v1"`) {
		t.Errorf("deployment %s", writes[3].Body)
	}
}

func TestHelmRollbackForceWithNothingToChange(t *testing.T) {
	f := helmRollbackAPI(t, nil)
	// The ConfigMap already reads as v1 renders it: a patch drops it, a replace is sent.
	f.AnswerWith("GET "+helmTestCM, 200, `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"site","namespace":"web","resourceVersion":"5",
	  "annotations":{"meta.helm.sh/release-name":"site","meta.helm.sh/release-namespace":"web"},"labels":{"app.kubernetes.io/managed-by":"Helm"}},"data":{"a":"1"}}`)
	f.AnswerWith("PUT "+helmTestCM, 200, `{}`)
	f.AnswerWith("PUT "+helmTestDeploy, 200, `{}`)

	for opts, want := range map[string]string{"": "", `{"force":true}`: ChangeReplace} {
		plan, _, _ := helmRollbackWrites(t, f, opts)

		got := ""

		for _, c := range plan.Changes {
			if c.Kind == "ConfigMap" && c.Name == "site" {
				got = c.Action
			}
		}

		if got != want {
			t.Errorf("options %q: configmap %q, want %q", opts, got, want)
		}
	}
}

func TestHelmRollbackPrunesHistory(t *testing.T) {
	v1 := helmTestSecrets + "/sh.helm.release.v1.site.v1"

	f := helmRollbackAPI(t, nil)
	f.AnswerWith("DELETE "+v1, 200, `{}`)

	plan, writes, err := helmRollbackWrites(t, f, `{"historyMax":2}`)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(plan.Prune, []int{1}) || plan.HistoryMax != 2 {
		t.Fatalf("plan prune %v max %d", plan.Prune, plan.HistoryMax)
	}

	keys := writeKeys(writes)
	if n := len(keys); n < 2 || keys[n-2] != "PUT "+helmTestSecrets+"/sh.helm.release.v1.site.v3" || keys[n-1] != "DELETE "+v1 {
		t.Fatalf("writes %v", keys)
	}

	// Nothing pruned without a history max, or with one the history fits in.
	for _, opts := range []string{"", `{"historyMax":0}`, `{"historyMax":3}`} {
		plan, writes, err := helmRollbackWrites(t, f, opts)
		if err != nil {
			t.Fatal(err)
		}

		if len(plan.Prune) != 0 || plan.Prune == nil {
			t.Errorf("options %q: prune %v", opts, plan.Prune)
		}

		for _, w := range writes {
			if w.Method == "DELETE" && strings.Contains(w.Path, "/secrets/") {
				t.Errorf("options %q pruned %s", opts, w.Path)
			}
		}
	}
}

func TestHelmRollbackPruneRefusedStillRollsBack(t *testing.T) {
	v1 := helmTestSecrets + "/sh.helm.release.v1.site.v1"

	f := helmRollbackAPI(t, nil)
	f.AnswerWith("DELETE "+v1, 403, `{"kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"secrets \"sh.helm.release.v1.site.v1\" is forbidden"}`)

	_, writes, err := helmRollbackWrites(t, f, `{"historyMax":2}`)
	if err == nil || !strings.HasPrefix(err.Error(), PrunePrefix) || !strings.Contains(err.Error(), "revision 1") {
		t.Fatalf("err %v", err)
	}

	deployed := false

	for _, w := range writes {
		if w.Method == "PUT" && strings.HasSuffix(w.Path, ".v3") && strings.Contains(w.Body, `"status":"deployed"`) {
			deployed = true
		}
	}

	if !deployed {
		t.Fatal("the rollback was not recorded as deployed")
	}
}

func TestHelmRollbackFailedKeepsHistory(t *testing.T) {
	f := helmRollbackAPI(t, nil)
	f.AnswerWith("DELETE "+helmTestSecrets+"/sh.helm.release.v1.site.v1", 200, `{}`)

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, Options{HistoryMax: 1})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(rb.plan.Prune, []int{1, 2}) {
		t.Fatalf("plan %+v", rb.plan)
	}

	// The change fails for real after a dry run that passed.
	f.AnswerWith("PATCH "+helmTestDeploy, 409, `{"kind":"Status","reason":"Conflict","code":409,"message":"conflict"}`)

	f.ForgetRequests()

	if _, err := runRollback(context.Background(), k, rb, time.Now()); err == nil {
		t.Fatal("no error")
	}

	for _, r := range f.Recorded() {
		if r.Method == "DELETE" && strings.Contains(r.Path, "/secrets/") {
			t.Fatalf("a failed rollback pruned %s", r.Path)
		}
	}
}

// A replace reads the object again before its PUT: the resourceVersion bumped since the
// plan is the one sent, and a conflict is retried once on another fresh read.
func TestHelmRollbackForceReadsLiveAgain(t *testing.T) {
	f := helmRollbackAPI(t, nil)
	f.AnswerWith("PUT "+helmTestCM, 200, `{}`)
	f.AnswerWith("PUT "+helmTestDeploy, 200, `{}`)

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}

	cm := func(rv string) string {
		return `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"site","namespace":"web","resourceVersion":"` + rv +
			`","annotations":{"meta.helm.sh/release-name":"site"}},"data":{"a":"2","b":"x"}}`
	}

	// Bumped between the plan and the run; the first PUT then conflicts with a write made
	// right after its read.
	f.AnswerWith("GET "+helmTestCM, 200, cm("6"))
	f.AnswerWith("PUT "+helmTestCM, 409, `{"kind":"Status","reason":"Conflict","code":409,"message":"the object has been modified"}`)
	f.BeforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == helmTestCM {
			f.AnswerWith("GET "+helmTestCM, 200, cm("7"))
			f.AnswerWith("PUT "+helmTestCM, 200, `{}`)
		}
	}

	f.ForgetRequests()

	if pruneErr, err := runRollback(context.Background(), k, rb, time.Now()); err != nil || pruneErr != nil {
		t.Fatal(err, pruneErr)
	}

	var puts []string

	for _, r := range f.Recorded() {
		if r.Method == "PUT" && r.Path == helmTestCM {
			puts = append(puts, r.Body)
		}
	}

	if len(puts) != 2 || !strings.Contains(puts[0], `"resourceVersion":"6"`) || !strings.Contains(puts[1], `"resourceVersion":"7"`) {
		t.Fatalf("configmap PUTs %v", puts)
	}
}
