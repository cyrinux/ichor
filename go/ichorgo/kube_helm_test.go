package ichorgo

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube/helm"
)

// The Helm logic is tested in go/kube/helm; here are the entry points the apps call. These
// fixtures are a copy of that package's, which cannot be imported from its tests.

// helmSecretData is a release as Helm stores it in a Secret's data (JSON-escaped for the fake).
func helmSecretData(t *testing.T, release map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()

	helm := base64.StdEncoding.EncodeToString(buf.Bytes())

	return base64.StdEncoding.EncodeToString([]byte(helm))
}

func helmRow(secret, name string, version int, status string) string {
	return `{"cells":[],"object":{"metadata":{"name":"` + secret + `","namespace":"web","labels":{"owner":"helm","name":"` + name +
		`","version":"` + itoa(version) + `","status":"` + status + `","modifiedAt":"1767225600"}}}}`
}

func itoa(i int) string { return strconv.Itoa(i) }

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

const (
	helmTestJobs    = "/apis/batch/v1/namespaces/web/jobs"
	helmTestPreJob  = helmTestJobs + "/site-migrate"
	helmTestPostJob = helmTestJobs + "/site-notify"

	helmTestJobRunning  = `{"kind":"Job","status":{"active":1}}`
	helmTestJobComplete = `{"kind":"Job","status":{"conditions":[{"type":"Complete","status":"True"}]}}`
	helmTestJobFailed   = `{"kind":"Job","status":{"conditions":[{"type":"Failed","status":"True","reason":"BackoffLimitExceeded","message":"Job has reached the specified backoff limit"}]}}`
)

func helmTestHookJob(name string) string {
	return "apiVersion: batch/v1\nkind: Job\nmetadata:\n  name: " + name + "\n  annotations:\n    helm.sh/hook: x\n" +
		"spec:\n  template:\n    spec:\n      restartPolicy: Never\n      containers:\n      - name: run\n        image: site:v1\n"
}

// helmHooksAPI is helmRollbackAPI whose target revision (1) has a pre-rollback Job (weight
// -5, no delete policy: before-hook-creation) and a post-rollback Job with postPolicies.
func helmHooksAPI(t *testing.T, postPolicies ...string) *fakeKubeAPI {
	t.Helper()

	interval := helm.HookPollInterval
	helm.HookPollInterval = time.Millisecond

	t.Cleanup(func() { helm.HookPollInterval = interval })

	post := []any{}
	for _, p := range postPolicies {
		post = append(post, p)
	}

	f := helmRollbackAPI(t, map[string]any{"hooks": []any{
		map[string]any{"name": "site-notify", "kind": "Job", "path": "site/templates/notify.yaml", "manifest": helmTestHookJob("site-notify"),
			"events": []any{"post-rollback"}, "delete_policies": post, "last_run": map[string]any{"phase": "Succeeded"}},
		map[string]any{"name": "site-migrate", "kind": "Job", "manifest": helmTestHookJob("site-migrate"), "events": []any{"pre-rollback", "pre-upgrade"}, "weight": -5},
		map[string]any{"name": "site-test", "kind": "Pod", "manifest": "kind: Pod", "events": []any{"test"}},
	}})

	f.answerWith("GET /apis/batch/v1", 200, `{"groupVersion":"batch/v1","resources":[{"name":"jobs","kind":"Job","namespaced":true,"verbs":["get","create","delete"]}]}`)
	f.answerWith("POST "+helmTestJobs, 200, `{}`)
	f.answerWith("GET "+helmTestPreJob, 200, helmTestJobRunning)
	f.answerWith("GET "+helmTestPostJob, 200, helmTestJobComplete)
	f.answerWith("DELETE "+helmTestPostJob, 200, `{}`)

	// The pre-rollback Job runs once read, then completes.
	f.beforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == helmTestPreJob {
			f.answerWith("GET "+helmTestPreJob, 200, helmTestJobComplete)
		}
	}

	return f
}

func TestKubeHelmReleases(t *testing.T) {
	rel := func(version int, chartVersion string) map[string]any {
		return map[string]any{
			"name": "site", "namespace": "web", "version": version,
			"info":     map[string]any{"status": "deployed", "description": "Upgrade complete", "notes": "visit me"},
			"chart":    map[string]any{"metadata": map[string]any{"name": "site", "version": chartVersion, "appVersion": "2.0"}},
			"config":   map[string]any{"replicas": 3},
			"manifest": "kind: Deployment\n",
		}
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/secrets": `{"kind":"Table","apiVersion":"meta.k8s.io/v1","columnDefinitions":[],"rows":[` +
			helmRow("sh.helm.release.v1.site.v1", "site", 1, "superseded") + "," +
			helmRow("sh.helm.release.v1.site.v2", "site", 2, "deployed") + `]}`,
		"GET /api/v1/namespaces/web/secrets/sh.helm.release.v1.site.v2": `{"data":{"release":"` + helmSecretData(t, rel(2, "1.2.0")) + `"}}`,
	})

	stored := kubeStoreFor(t, f)

	out, err := KubeHelmReleases(stored, "admin@test", "", "web")
	if err != nil {
		t.Fatal(err)
	}

	var list helm.ReleaseList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	if len(list.Releases) != 1 || list.Releases[0].Revision != 2 || list.Releases[0].ChartVersion != "1.2.0" || list.Releases[0].Status != "deployed" {
		t.Fatalf("releases %+v", list.Releases)
	}

	out, err = KubeHelmRelease(stored, "admin@test", "", "web", "site")
	if err != nil {
		t.Fatal(err)
	}

	var detail helm.ReleaseDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatal(err)
	}

	if detail.Values != "replicas: 3\n" || detail.Notes != "visit me" || detail.Manifest != "kind: Deployment\n" ||
		len(detail.History) != 2 || detail.History[0].Revision != 2 || detail.Updated != 1767225600 {
		t.Fatalf("detail %+v", detail)
	}

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, ".v1") {
			t.Error("the superseded revision's payload was read")
		}

		if r.path == "/api/v1/namespaces/web/secrets" && !strings.Contains(r.query, "labelSelector=owner%3Dhelm") {
			t.Errorf("list query %q", r.query)
		}
	}
}

func TestKubeHelmRollbackDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeHelmRollbackPlan(cfg, "", "", "ingress-nginx", "ingress-nginx", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	var p helm.Plan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if p.From != 7 || p.To != 6 || len(p.Changes) == 0 || p.ToChart != "ingress-nginx-4.12.0" && p.ToChart != "ingress-nginx-4.13.0" {
		t.Fatalf("plan %+v", p)
	}

	if err := KubeHelmRollback(cfg, "", "", "ingress-nginx", "ingress-nginx", 6, ""); err == nil {
		t.Fatal("demo rolled back")
	}

	if _, err := KubeHelmRollbackPlan(cfg, "", "", "web", "../x", 1, ""); err == nil {
		t.Fatal("bad name accepted")
	}

	if _, err := KubeHelmRollbackPlan(cfg, "", "", "web", "site", -1, ""); err == nil {
		t.Fatal("negative revision accepted")
	}
}

func TestKubeHelmRollbackDemoHook(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeHelmRollbackPlan(cfg, "", "", "ingress-nginx", "ingress-nginx", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	var p helm.Plan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if len(p.Hooks) != 1 || p.Hooks[0].Event != helm.HookPreRollback || p.Hooks[0].Kind != "Job" || p.Hooks[0].Weight != -5 {
		t.Fatalf("demo hooks %+v", p.Hooks)
	}

	if err := KubeHelmRollback(cfg, "", "", "ingress-nginx", "ingress-nginx", 6, ""); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Fatalf("demo rollback: %v", err)
	}
}

func TestKubeHelmRollbackDemoOptions(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeHelmRollbackPlan(cfg, "", "", "ingress-nginx", "ingress-nginx", 0, `{"force":true,"historyMax":2}`)
	if err != nil {
		t.Fatal(err)
	}

	var p helm.Plan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if !p.Force || p.HistoryMax != 2 || !reflect.DeepEqual(p.Prune, []int{5, 6}) || p.Workloads != 2 || p.Changes[0].Action != helm.ChangeReplace {
		t.Fatalf("plan %+v", p)
	}

	if _, err := KubeHelmRollbackPlan(cfg, "", "", "ingress-nginx", "ingress-nginx", 0, `{"historyMax":-2}`); err == nil {
		t.Fatal("negative history max accepted")
	}

	if err := KubeHelmRollback(cfg, "", "", "ingress-nginx", "ingress-nginx", 6, `{"historyMax":2}`); err == nil {
		t.Fatal("demo rolled back")
	}
}

// Through the exported call, the hook's failure keeps its message past the Kubernetes call
// wrapper.
func TestKubeHelmRollbackHookFailureNamesTheHook(t *testing.T) {
	f := helmHooksAPI(t)
	f.beforeAnswer = nil
	f.answerWith("GET "+helmTestPreJob, 200, helmTestJobFailed)

	err := KubeHelmRollback(f.kubeconfigFor(f.URL), "admin@test", "", "web", "site", 0, "")
	if err == nil || !strings.HasPrefix(err.Error(), "rollback stopped before any change") || !strings.Contains(err.Error(), "Job site-migrate failed") {
		t.Fatalf("err %v", err)
	}
}

// Through the exported call: kubeDo unwraps an error to the API's, so a prune refused must
// keep its prefix past it, and the rollback is still deployed.
func TestKubeHelmRollbackPruneRefusedKeepsPrefix(t *testing.T) {
	v1 := helmTestSecrets + "/sh.helm.release.v1.site.v1"

	f := helmRollbackAPI(t, nil)
	f.answerWith("DELETE "+v1, 403, `{"kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"secrets \"sh.helm.release.v1.site.v1\" is forbidden"}`)

	err := KubeHelmRollback(f.kubeconfigFor(f.URL), "admin@test", "", "web", "site", 0, `{"historyMax":2}`)
	if err == nil || !strings.HasPrefix(err.Error(), helm.PrunePrefix) || !strings.Contains(err.Error(), "revision 1") {
		t.Fatalf("err %v", err)
	}

	deployed := false

	for _, r := range f.recorded() {
		if r.method == "PUT" && strings.HasSuffix(r.path, ".v3") && strings.Contains(r.body, `"status":"deployed"`) {
			deployed = true
		}
	}

	if !deployed {
		t.Fatal("the rollback was not recorded as deployed")
	}
}
