package ichorgo

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func deleteRequests(f *fakeKubeAPI) []fakeKubeRequest {
	var out []fakeKubeRequest

	for _, r := range f.recorded() {
		if r.method == http.MethodDelete {
			out = append(out, r)
		}
	}

	return out
}

func TestKubeObjectDeleteSendsOptions(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeKubeAPI(t, map[string]string{
		"DELETE /apis/cert-manager.io/v1/namespaces/shop/certificates/web-tls": `{"kind":"Status","status":"Success"}`,
	})
	stored := kubeStoreFor(t, f)

	if err := KubeObjectDelete(stored, "admin@test", "", "cert-manager.io", "v1", "certificates", "shop", "web-tls", "foreground", "42", 30, false); err != nil {
		t.Fatal(err)
	}

	dels := deleteRequests(f)
	if len(dels) != 1 {
		t.Fatalf("deletes %+v", dels)
	}

	var opts map[string]any
	if err := json.Unmarshal([]byte(dels[0].body), &opts); err != nil {
		t.Fatalf("%v: %s", err, dels[0].body)
	}

	pre, _ := opts["preconditions"].(map[string]any)
	if opts["kind"] != "DeleteOptions" || opts["propagationPolicy"] != "Foreground" || opts["gracePeriodSeconds"] != float64(30) ||
		pre["resourceVersion"] != "42" || dels[0].contentType != "application/json" {
		t.Errorf("options %s (%s)", dels[0].body, dels[0].contentType)
	}

	got := readAudit(t, "", "delete")
	if len(got) != 1 || got[0].Outcome != auditOK || got[0].Namespace != "shop" || got[0].Object != "certificates/web-tls" ||
		got[0].Params != "propagation=Foreground" {
		t.Errorf("audit %+v", got)
	}
}

func TestKubeObjectDeleteDefaults(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"DELETE /api/v1/namespaces/shop/configmaps/settings": `{}`,
	})

	if err := KubeObjectDelete(kubeStoreFor(t, f), "admin@test", "", "", "v1", "configmaps", "shop", "settings", "", "", -1, false); err != nil {
		t.Fatal(err)
	}

	body := deleteRequests(f)[0].body
	if !strings.Contains(body, `"propagationPolicy":"Background"`) || strings.Contains(body, "gracePeriodSeconds") || strings.Contains(body, "preconditions") {
		t.Errorf("body %s", body)
	}
}

func TestKubeObjectDeleteConflict(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	f.answerWith("DELETE /api/v1/namespaces/shop/configmaps/settings", http.StatusConflict,
		`{"kind":"Status","reason":"Conflict","message":"Precondition failed: ResourceVersion in precondition: 41, ResourceVersion in object meta: 42"}`)

	err := KubeObjectDelete(kubeStoreFor(t, f), "admin@test", "", "", "v1", "configmaps", "shop", "settings", "Orphan", "41", -1, false)
	if err == nil || !strings.Contains(err.Error(), "changed since you looked") {
		t.Fatalf("err = %v", err)
	}
}

func TestKubeObjectDeleteProtected(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeKubeAPI(t, map[string]string{
		"DELETE /api/v1/namespaces/kube-system":                                              `{}`,
		"DELETE /api/v1/nodes/worker-1":                                                      `{}`,
		"DELETE /apis/apiextensions.k8s.io/v1/customresourcedefinitions/widgets.example.com": `{}`,
		"DELETE /api/v1/namespaces/kube-system/secrets/bootstrap-token-abc123":               `{}`,
	})
	stored := kubeStoreFor(t, f)

	cases := []struct{ group, resource, namespace, name string }{
		{"", "namespaces", "", "kube-system"},
		{"", "nodes", "", "worker-1"},
		{"apiextensions.k8s.io", "customresourcedefinitions", "", "widgets.example.com"},
		{"", "secrets", "kube-system", "bootstrap-token-abc123"},
	}

	for _, c := range cases {
		err := KubeObjectDelete(stored, "admin@test", "", c.group, "v1", c.resource, c.namespace, c.name, "", "", -1, false)
		if err == nil || !strings.HasPrefix(err.Error(), "protected: ") {
			t.Errorf("%s/%s: err = %v", c.resource, c.name, err)
		}
	}

	if n := len(deleteRequests(f)); n != 0 {
		t.Fatalf("%d deletes sent without force", n)
	}

	for _, c := range cases {
		if err := KubeObjectDelete(stored, "admin@test", "", c.group, "v1", c.resource, c.namespace, c.name, "", "", -1, true); err != nil {
			t.Errorf("%s/%s forced: %v", c.resource, c.name, err)
		}
	}

	if n := len(deleteRequests(f)); n != len(cases) {
		t.Fatalf("%d deletes sent with force, want %d", n, len(cases))
	}

	got := readAudit(t, "", "delete")
	if len(got) != 2*len(cases) || got[0].Params != "propagation=Background, force" || got[len(got)-1].Outcome != auditFailed {
		t.Errorf("audit %+v", got)
	}

	// Not protected: another namespace, a Secret elsewhere.
	if deleteProtection(resourceRef{"", "v1", "namespaces"}, "", "shop") != "" ||
		deleteProtection(resourceRef{"", "v1", "secrets"}, "shop", "bootstrap-token-abc123") != "" {
		t.Error("an ordinary object is protected")
	}
}

func TestKubeObjectDeleteRefuses(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeObjectDelete(demo, "Demo cluster", "", "", "v1", "configmaps", "web", "settings", "", "", -1, false); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("demo: err = %v", err)
	}

	if e := readAudit(t, "", "delete"); len(e) != 1 || !e[0].Demo || e[0].Outcome != auditFailed {
		t.Errorf("demo audit %+v", e)
	}

	for name, args := range map[string][]string{
		"policy":    {"", "configmaps", "web", "settings", "cascade"},
		"name":      {"", "configmaps", "web", "../x", ""},
		"namespace": {"", "configmaps", "Web!", "settings", ""},
		"resource":  {"", "Config Maps", "web", "settings", ""},
	} {
		if err := KubeObjectDelete(demo, "Demo cluster", "", args[0], "v1", args[1], args[2], args[3], args[4], "", -1, false); err == nil || errors.Is(err, errDemoUnavailable) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestKubeObjectDeletePreview(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/namespaces/shop/deployments/api": `{"kind":"Deployment","metadata":{"name":"api","namespace":"shop","uid":"d1",
			"resourceVersion":"9","finalizers":["example.com/protect"]},"spec":{"selector":{"matchLabels":{"app":"api"}}}}`,
		"GET /apis/apps/v1/namespaces/shop/replicasets": `{"items":[
			{"metadata":{"name":"api-7f","uid":"r1","ownerReferences":[{"uid":"d1"}]}},
			{"metadata":{"name":"other-1","uid":"r2","ownerReferences":[{"uid":"zz"}]}}]}`,
		"GET /api/v1/namespaces/shop/pods": `{"items":[
			{"metadata":{"name":"api-7f-a","uid":"p1","ownerReferences":[{"uid":"r1"}]}},
			{"metadata":{"name":"lonely","uid":"p2"}}]}`,
	})
	stored := kubeStoreFor(t, f)

	out, err := KubeObjectDeletePreview(stored, "admin@test", "", "apps", "v1", "deployments", "shop", "api")
	if err != nil {
		t.Fatal(err)
	}

	var p kubeDeletePreview
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	want := []kubeDeleteDependent{{"ReplicaSet", "shop", "api-7f"}, {"Pod", "shop", "api-7f-a"}}
	if p.Protected || p.ClusterScoped || p.ResourceVersion != "9" || len(p.Finalizers) != 1 || p.Finalizers[0] != "example.com/protect" ||
		len(p.Dependents) != 2 || p.Dependents[0] != want[0] || p.Dependents[1] != want[1] {
		t.Errorf("preview %s", out)
	}

	for _, r := range f.recorded() {
		if r.path == "/api/v1/namespaces/shop/pods" && !strings.Contains(r.query, "labelSelector=app%3Dapi") {
			t.Errorf("pods listed without the selector: %q", r.query)
		}
	}
}

func TestKubeObjectDeletePreviewProtectedAndPlain(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/kube-system": `{"kind":"Namespace","metadata":{"name":"kube-system","uid":"n1","resourceVersion":"3","finalizers":[]}}`,
		"GET /api/v1/namespaces/shop/configmaps/settings": `{"kind":"ConfigMap","metadata":{"name":"settings","namespace":"shop","uid":"c1",
			"resourceVersion":"5","deletionTimestamp":"2026-01-01T00:00:00Z","finalizers":["example.com/hold"]}}`,
	})
	stored := kubeStoreFor(t, f)

	out, err := KubeObjectDeletePreview(stored, "admin@test", "", "", "v1", "namespaces", "", "kube-system")
	if err != nil {
		t.Fatal(err)
	}

	var ns kubeDeletePreview
	if err := json.Unmarshal([]byte(out), &ns); err != nil {
		t.Fatal(err)
	}

	if !ns.Protected || ns.Reason == "" || !ns.ClusterScoped || ns.Finalizers == nil || ns.Dependents == nil {
		t.Errorf("namespace preview %s", out)
	}

	out, err = KubeObjectDeletePreview(stored, "admin@test", "", "", "v1", "configmaps", "shop", "settings")
	if err != nil {
		t.Fatal(err)
	}

	var cm kubeDeletePreview
	if err := json.Unmarshal([]byte(out), &cm); err != nil {
		t.Fatal(err)
	}

	if cm.Protected || !cm.Deleting || len(cm.Finalizers) != 1 || len(cm.Dependents) != 0 {
		t.Errorf("configmap preview %s", out)
	}

	if _, err := KubeObjectDeletePreview(stored, "admin@test", "", "", "v1", "configmaps", "shop", "gone"); err == nil ||
		!strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("missing object: err = %v", err)
	}
}

func TestKubeObjectDeletePreviewDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeObjectDeletePreview(demo, "Demo cluster", "", "", "v1", "nodes", "", "worker-1")
	if err != nil {
		t.Fatal(err)
	}

	var p kubeDeletePreview
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if !p.Protected || !p.ClusterScoped {
		t.Errorf("demo preview %s", out)
	}
}
