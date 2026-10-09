package ichorgo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	rolloutScalePath = "/apis/argoproj.io/v1alpha1/namespaces/shop/rollouts/web/scale"
	jobPath          = "/apis/batch/v1/namespaces/shop/jobs/import"
)

func TestDiscoveryMarksScalable(t *testing.T) {
	list := discoveryResourceList{GroupVersion: "argoproj.io/v1alpha1"}
	if err := json.Unmarshal([]byte(`{"resources":[
	  {"name":"rollouts","kind":"Rollout","verbs":["get","list","patch"]},
	  {"name":"rollouts/scale","kind":"Scale","verbs":["get","patch","update"]},
	  {"name":"rollouts/status","kind":"Rollout","verbs":["get"]},
	  {"name":"analysisruns","kind":"AnalysisRun","verbs":["get","list"]}]}`), &list); err != nil {
		t.Fatal(err)
	}

	got := listableResources(list)
	if len(got) != 2 || !got[0].Scalable || got[1].Scalable {
		t.Fatalf("resources %+v", got)
	}

	jobs := listableResources(discoveryResourceList{GroupVersion: "batch/v1", Resources: []discoveryResource{
		{Name: "jobs", Kind: "Job", Verbs: []string{"list", "patch"}},
	}})
	if len(jobs) != 1 || !jobs[0].Scalable {
		t.Errorf("a Job sets its parallelism: %+v", jobs)
	}
}

func TestKubeObjectScale(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET " + rolloutScalePath: `{"kind":"Scale","spec":{"replicas":3},"status":{"replicas":2}}`,
		"GET " + jobPath:          `{"spec":{"parallelism":4},"status":{"active":1}}`,
	})
	store := kubeStoreFor(t, f)

	cases := map[string]struct {
		group, version, resource string
		want                     kubeObjectScale
	}{
		"scale subresource": {"argoproj.io", "v1alpha1", "rollouts", kubeObjectScale{Replicas: 3, Current: 2, Field: "replicas"}},
		"job parallelism":   {"batch", "v1", "jobs", kubeObjectScale{Replicas: 4, Current: 1, Field: "parallelism"}},
	}

	for name, c := range cases {
		objName := "web"
		if c.resource == "jobs" {
			objName = "import"
		}

		out, err := KubeObjectScale(store, "admin@test", "", c.group, c.version, c.resource, "shop", objName)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		var got kubeObjectScale
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}

		if got != c.want {
			t.Errorf("%s: scale = %+v, want %+v", name, got, c.want)
		}
	}
}

func TestKubeObjectScaleJobDefaultsToOne(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET " + jobPath: `{"spec":{},"status":{}}`})

	out, err := KubeObjectScale(kubeStoreFor(t, f), "admin@test", "", "batch", "v1", "jobs", "shop", "import")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"replicas":1`) {
		t.Errorf("unset parallelism is 1: %s", out)
	}
}

func TestKubeScaleObjectSubresource(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"PATCH " + rolloutScalePath: `{}`,
		"GET /apis/autoscaling/v2/namespaces/shop/horizontalpodautoscalers": `{"items":[
			{"metadata":{"name":"keda-hpa-web"},"spec":{"scaleTargetRef":{"kind":"Rollout","name":"web"},"maxReplicas":6}}]}`,
	})

	warning, err := KubeScaleObject(kubeStoreFor(t, f), "admin@test", "", "argoproj.io", "v1alpha1", "rollouts", "Rollout", "shop", "web", 5)
	if err != nil {
		t.Fatal(err)
	}

	req := lastActionRequest(f, http.MethodPatch)
	if req.path != rolloutScalePath || req.body != `{"spec":{"replicas":5}}` || req.contentType != "application/merge-patch+json" {
		t.Errorf("scale request = %+v", req)
	}

	if !strings.Contains(warning, "keda-hpa-web") || !strings.Contains(warning, "1 to 6") {
		t.Errorf("warning = %q", warning)
	}
}

func TestKubeScaleObjectJob(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"PATCH " + jobPath: `{}`})

	warning, err := KubeScaleObject(kubeStoreFor(t, f), "admin@test", "", "batch", "v1", "jobs", "Job", "shop", "import", 3)
	if err != nil {
		t.Fatal(err)
	}

	req := lastActionRequest(f, http.MethodPatch)
	if req.path != jobPath || req.body != `{"spec":{"parallelism":3}}` {
		t.Errorf("parallelism request = %+v", req)
	}

	if warning != "" {
		t.Errorf("a Job has no autoscaler: %q", warning)
	}
}

func TestKubeScaleObjectRefusals(t *testing.T) {
	cases := map[string]struct {
		group, resource, namespace, name string
		replicas                         int
		want                             string
	}{
		"negative":      {"apps", "replicasets", "shop", "web", -1, "between 0 and"},
		"too many":      {"apps", "replicasets", "shop", "web", maxScaleReplicas + 1, "between 0 and"},
		"bad resource":  {"apps", "Replica Sets", "shop", "web", 1, "invalid resource"},
		"bad name":      {"apps", "replicasets", "shop", "../x", 1, "invalid Kubernetes name"},
		"no name":       {"apps", "replicasets", "shop", "", 1, "no object name"},
		"bad namespace": {"apps", "replicasets", "Shop!", "web", 1, "invalid Kubernetes namespace"},
	}

	for name, c := range cases {
		_, err := KubeScaleObject("", "", "", c.group, "v1", c.resource, "Kind", c.namespace, c.name, c.replicas)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
