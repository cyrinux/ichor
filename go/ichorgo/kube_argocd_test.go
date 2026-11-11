package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func readArgoFixture[T any](t *testing.T, name string) []T {
	t.Helper()

	data, err := os.ReadFile("../kube/argocd/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}

	var list kubeList[T]
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}

	return list.Items
}

func argoAppByName(t *testing.T, s argoStatus, name string) argoApp {
	t.Helper()

	for _, a := range s.Apps {
		if a.Name == name {
			return a
		}
	}

	t.Fatalf("no app %q", name)

	return argoApp{}
}

func argoObjectByName(t *testing.T, name string) argoObject {
	t.Helper()

	for _, o := range readArgoFixture[argoObject](t, "applications.json") {
		if o.Metadata.Name == name {
			return o
		}
	}

	t.Fatalf("no app %q", name)

	return argoObject{}
}

func TestReadArgoCD(t *testing.T) {
	apps, _ := os.ReadFile("../kube/argocd/testdata/applications.json")
	sets, _ := os.ReadFile("../kube/argocd/testdata/applicationsets.json")

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/argoproj.io/v1alpha1/applications":    string(apps),
		"GET /apis/argoproj.io/v1alpha1/applicationsets": string(sets),
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"argocd-application-controller-0","namespace":"argocd","labels":{"app.kubernetes.io/name":"argocd-application-controller"}},
		   "spec":{"nodeName":"node-1","containers":[{"name":"c","image":"quay.io/argoproj/argocd:v3.4.5"}]},"status":{"phase":"Running"}}]}`,
		// Only the namespaces of apps in trouble are read, one by one.
		"GET /api/v1/namespaces/web/pods": `{"items":[
		  {"metadata":{"name":"web-1","namespace":"web"},"spec":{"nodeName":"node-3","containers":[{"name":"web","image":"web:1"}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"web","ready":false,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s, err := readArgoCD(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	// The projects listing is missing (404): no error, just none.
	if !s.Installed || s.Version != "v3.4.5" || len(s.Apps) != 4 || len(s.AppSets) != 2 || s.AppSetsError != "" || len(s.Projects) != 0 {
		t.Fatalf("status %+v", s)
	}

	web := argoAppByName(t, s, "web")
	if len(web.UnhealthyPods) != 1 || web.UnhealthyPods[0].Status != "CrashLoopBackOff" || web.UnhealthyPods[0].Node != "node-3" {
		t.Fatalf("unhealthy pods %+v", web.UnhealthyPods)
	}

	// The icon comes from the catalog, by chart.
	if cilium := argoAppByName(t, s, "cilium"); cilium.Icon != "cilium" {
		t.Fatalf("icon %q", cilium.Icon)
	}
}

func TestKubeArgoFreezeValidates(t *testing.T) {
	if err := KubeArgoFreeze("", "", "", "argocd", "infra", "delete", ""); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("got %v", err)
	}

	if err := KubeArgoFreeze("", "", "", "argocd", "infra", "freeze", "{"); err == nil || !strings.Contains(err.Error(), "bad freeze options") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeArgoFreezeDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeArgoCD(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var s argoStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}

	// The demo freezes the demo namespace and keeps an expired freeze and a Git window.
	var frozen, expired, git bool
	for _, a := range s.Apps {
		frozen = frozen || a.Name == "demo-worker" && a.Freeze != nil && a.Freeze.ByIchor
	}

	for _, p := range s.Projects {
		for _, w := range p.Windows {
			expired = expired || w.Ichor != nil && w.Ichor.Expired
			git = git || w.Ichor == nil && w.Kind == "deny"
		}
	}

	if !frozen || !expired || !git {
		t.Fatalf("demo: frozen %v expired %v git %v", frozen, expired, git)
	}

	if err := KubeArgoFreeze(cfg, "", "", "argocd", "apps", "clearExpired", ""); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestKubeArgoActionValidates(t *testing.T) {
	if err := KubeArgoAction("", "", "", "argocd", "web", "delete", ""); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("got %v", err)
	}

	if err := KubeArgoAction("", "", "", "argocd", "web", argoActionSync, "{"); err == nil || !strings.Contains(err.Error(), "bad sync options") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeArgoDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeArgoCD(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var s argoStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil || !s.Installed || len(s.Apps) < 5 {
		t.Fatalf("demo: %v %s", err, out)
	}

	// The demo shows a running sync, a failure and a degraded app with its crashing pod.
	var running, failed, crashing bool
	for _, a := range s.Apps {
		running = running || a.Operation != nil && a.Operation.Phase == "Running"
		failed = failed || a.Operation != nil && a.Operation.Phase == "Failed"
		crashing = crashing || len(a.UnhealthyPods) > 0
	}

	if !running || !failed || !crashing {
		t.Fatalf("demo states: running %v failed %v crashing %v", running, failed, crashing)
	}

	if err := KubeArgoAction(cfg, "", "", "argocd", "cilium", argoActionSync, ""); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}
}
