package ichorgo

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
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

func helmRollbackAPI(t *testing.T, extraV1 map[string]any) *fakeKubeAPI {
	t.Helper()

	// After a failed upgrade Helm leaves the previous revision deployed.
	v1 := helmSecretData(t, helmTestRelease(1, "deployed", "1.0.0", helmTestManifestV1, extraV1))
	v2 := helmSecretData(t, helmTestRelease(2, "failed", "1.1.0", helmTestManifestV2, nil))

	return newFakeKubeAPI(t, map[string]string{
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

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	rb, err := prepareHelmRollback(context.Background(), k, "web", "site", 0)
	if err != nil {
		t.Fatal(err)
	}

	p := rb.plan
	if p.From != 2 || p.To != 1 || p.ReleaseNamespace != "web" || p.FromChart != "site-1.1.0" || p.ToChart != "site-1.0.0" || len(p.Blockers) != 0 || p.FluxOwner != "" || p.Unchanged != 0 {
		t.Fatalf("plan %+v", p)
	}

	want := []helmChange{
		{Action: helmChangeKeep, Kind: "ConfigMap", Namespace: "web", Name: "site-keep"},
		{Action: helmChangeCreate, Kind: "ServiceAccount", Namespace: "web", Name: "site"},
		{Action: helmChangeUpdate, Kind: "ConfigMap", Namespace: "web", Name: "site"},
		{Action: helmChangeUpdate, Kind: "Deployment", Namespace: "web", Name: "site"},
		{Action: helmChangeDelete, Kind: "Service", Namespace: "web", Name: "site-extra"},
	}
	if !reflect.DeepEqual(p.Changes, want) {
		t.Fatalf("changes:\n got  %+v\n want %+v", p.Changes, want)
	}

	// The plan only dry-runs.
	for _, r := range f.recorded() {
		if r.method != "GET" && !strings.Contains(r.query, "dryRun=All") {
			t.Fatalf("plan wrote: %+v", r)
		}
	}

	f.mu.Lock()
	f.requests = nil
	f.mu.Unlock()

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if err := runHelmRollback(context.Background(), k, rb, now); err != nil {
		t.Fatal(err)
	}

	var writes []string

	bodies := map[string]string{}

	for _, r := range f.recorded() {
		if r.method != "GET" {
			writes = append(writes, r.method+" "+r.path)
			bodies[r.method+" "+r.path] = r.body
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

	payload, _, err := decodeHelmPayload(v.(string))
	if err != nil {
		t.Fatal(err)
	}

	return payload
}

func TestHelmRollbackBlockers(t *testing.T) {
	hooks := map[string]any{"hooks": []any{map[string]any{"name": "site-restore", "kind": "Job", "events": []any{"post-rollback"}}}}
	f := helmRollbackAPI(t, hooks)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	rb, err := prepareHelmRollback(context.Background(), k, "web", "site", 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(rb.plan.Blockers) != 1 || !strings.Contains(rb.plan.Blockers[0], "Job site-restore") {
		t.Fatalf("blockers %v", rb.plan.Blockers)
	}

	if err := runHelmRollback(context.Background(), k, rb, time.Now()); err == nil {
		t.Fatal("ran with a blocker")
	}

	for _, rev := range []int{2, 3} {
		if _, err := prepareHelmRollback(context.Background(), k, "web", "site", rev); err == nil {
			t.Errorf("revision %d accepted", rev)
		}
	}
}

func TestThreeWayMergePatch(t *testing.T) {
	original := map[string]any{"a": "1", "gone": "x", "m": map[string]any{"k": "v", "old": "o"}}
	modified := map[string]any{"a": "2", "m": map[string]any{"k": "v"}, "new": []any{"x"}}
	live := map[string]any{"a": "1", "gone": "x", "m": map[string]any{"k": "drift", "old": "o", "server": "s"}, "status": map[string]any{}}

	want := map[string]any{"a": "2", "gone": nil, "m": map[string]any{"k": "v", "old": nil}, "new": []any{"x"}}
	if got := threeWayMergePatch(original, modified, live); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}

	if got := threeWayMergePatch(modified, modified, map[string]any{"a": "2", "m": map[string]any{"k": "v", "x": 1}, "new": []any{"x"}}); len(got) != 0 {
		t.Fatalf("no-op patch %v", got)
	}
}

func TestHelmFluxOwner(t *testing.T) {
	objs, err := parseHelmManifest("kind: Deployment\nmetadata:\n  name: a\n  labels:\n    helm.toolkit.fluxcd.io/name: podinfo\n    helm.toolkit.fluxcd.io/namespace: flux-system\n")
	if err != nil {
		t.Fatal(err)
	}

	if got := helmFluxOwner("web", []helmObject{{Body: objs[0]}}); got != "flux-system/podinfo" {
		t.Fatalf("got %q", got)
	}
}

func TestKubeHelmRollbackDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeHelmRollbackPlan(cfg, "", "", "ingress-nginx", "ingress-nginx", 0)
	if err != nil {
		t.Fatal(err)
	}

	var p helmRollbackPlan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if p.From != 7 || p.To != 6 || len(p.Changes) == 0 || p.ToChart != "ingress-nginx-4.12.0" && p.ToChart != "ingress-nginx-4.13.0" {
		t.Fatalf("plan %+v", p)
	}

	if err := KubeHelmRollback(cfg, "", "", "ingress-nginx", "ingress-nginx", 6); err == nil {
		t.Fatal("demo rolled back")
	}

	if _, err := KubeHelmRollbackPlan(cfg, "", "", "web", "../x", 1); err == nil {
		t.Fatal("bad name accepted")
	}

	if _, err := KubeHelmRollbackPlan(cfg, "", "", "web", "site", -1); err == nil {
		t.Fatal("negative revision accepted")
	}
}

func TestHelmRollbackKeepsWhatLiveSaysToKeep(t *testing.T) {
	f := helmRollbackAPI(t, nil)

	f.mu.Lock()
	f.answers["GET "+helmTestService] = `{"apiVersion":"v1","kind":"Service","metadata":{"name":"site-extra","namespace":"web","annotations":{"helm.sh/resource-policy":"keep"}}}`
	f.mu.Unlock()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	rb, err := prepareHelmRollback(context.Background(), k, "web", "site", 1)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range rb.plan.Changes {
		if c.Name == "site-extra" && c.Action != helmChangeKeep {
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
	refs := []helmSecretRef{{revision: 5}, {revision: 3}}

	if _, err := helmTargetRef(refs, 0); err == nil {
		t.Error("revision 0 took 3, not 4")
	}

	if ref, err := helmTargetRef(refs, 3); err != nil || ref.revision != 3 {
		t.Errorf("got %+v %v", ref, err)
	}
}

func TestHelmObjectsGoToTheReleaseNamespace(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /api/v1": helmTestDiscover})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: b, namespace: other}\n"

	objs, err := helmObjects(context.Background(), newKubeKinds(k), manifest, "monitoring")
	if err != nil || len(objs) != 2 {
		t.Fatalf("%+v %v", objs, err)
	}

	if objs[0].Path != "/api/v1/namespaces/monitoring/configmaps/a" || objs[1].Path != "/api/v1/namespaces/other/configmaps/b" {
		t.Fatalf("paths %q %q", objs[0].Path, objs[1].Path)
	}
}

func TestLiveMatchesIgnoresServerDefaults(t *testing.T) {
	want := map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": 80.0}}}}
	live := map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": 80.0, "nodePort": 31000.0, "protocol": "TCP"}}, "clusterIP": "10.0.0.1"}}

	if p := threeWayMergePatch(want, want, live); len(p) != 0 {
		t.Fatalf("defaults made a patch: %v", p)
	}

	// A field the current manifest set and the target drops, still live: the list is replaced.
	was := map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": 80.0, "name": "http"}}}}
	live["spec"].(map[string]any)["ports"] = []any{map[string]any{"port": 80.0, "name": "http", "nodePort": 31000.0}}

	if p := threeWayMergePatch(was, want, live); len(p) == 0 {
		t.Fatal("a dropped field was kept")
	}
}

func TestParseHelmManifestReadsAsHelm(t *testing.T) {
	objs, err := parseHelmManifest("---\n# Source: x\nkind: A\nmetadata: {name: a}\nspec: {enabled: yes, date: 2024-01-01}\n---\n# only a comment\n---\nkind: B\nkind: C\n")
	if err != nil || len(objs) != 2 {
		t.Fatalf("%+v %v", objs, err)
	}

	if spec := objs[0]["spec"].(map[string]any); spec["date"] != "2024-01-01" || spec["enabled"] != true || objs[1]["kind"] != "C" {
		t.Fatalf("got %+v", objs)
	}
}
