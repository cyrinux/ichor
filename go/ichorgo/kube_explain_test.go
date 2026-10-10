package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

const explainIndexBody = `{"paths":{
	"api/v1":{"serverRelativeURL":"/openapi/v3/api/v1?hash=AAA111"},
	"apis/apps/v1":{"serverRelativeURL":"/openapi/v3/apis/apps/v1?hash=BBB222"}}}`

// freshExplainCache gives the test an empty document cache: it is keyed by the kubeconfig,
// whose server is a test server's loopback port, which a later test may get again.
func freshExplainCache(t *testing.T) {
	t.Helper()

	saved := explainDocs
	explainDocs = newExplainCache()

	t.Cleanup(func() { explainDocs = saved })
}

func explainFake(t *testing.T) (*fakeKubeAPI, string) {
	t.Helper()

	freshExplainCache(t)

	f := newFakeKubeAPI(t, map[string]string{
		"GET /openapi/v3":              explainIndexBody,
		"GET /openapi/v3/apis/apps/v1": string(demoExplainJSON),
	})

	return f, kubeStoreFor(t, f)
}

func explainCall(t *testing.T, stored, group, version, kind, path string) explainField {
	t.Helper()

	out, err := KubeExplain(stored, "admin@test", "", group, version, kind, path)
	if err != nil {
		t.Fatal(err)
	}

	var field explainField
	if err := json.Unmarshal([]byte(out), &field); err != nil {
		t.Fatal(err)
	}

	return field
}

func childNamed(t *testing.T, field explainField, name string) explainChild {
	t.Helper()

	i := slices.IndexFunc(field.Children, func(c explainChild) bool { return c.Name == name })
	if i < 0 {
		t.Fatalf("no child %s in %+v", name, field.Children)
	}

	return field.Children[i]
}

func TestKubeExplainResolvesRefsAndEnums(t *testing.T) {
	f, stored := explainFake(t)

	strategy := explainCall(t, stored, "apps", "v1", "Deployment", "spec.strategy")
	if strategy.Type != "DeploymentStrategy" || strategy.Required ||
		strategy.Description != "The deployment strategy to use to replace existing pods with new ones." {
		t.Errorf("strategy %+v", strategy)
	}

	kind := childNamed(t, strategy, "type")
	if kind.Type != "string" || !slices.Equal(kind.Enum, []string{"Recreate", "RollingUpdate"}) ||
		kind.Description != "Type of deployment." {
		t.Errorf("strategy.type %+v", kind)
	}

	if surge := explainCall(t, stored, "apps", "v1", "Deployment", "spec.strategy.rollingUpdate.maxSurge"); surge.Type != "int-or-string" || len(surge.Children) != 0 {
		t.Errorf("maxSurge %+v", surge)
	}

	// The request went to the URL the index gave, its hash included.
	reqs := f.recorded()
	if i := slices.IndexFunc(reqs, func(r fakeKubeRequest) bool { return r.path == "/openapi/v3/apis/apps/v1" }); i < 0 || reqs[i].query != "hash=BBB222" {
		t.Errorf("requests %+v", reqs)
	}
}

func TestKubeExplainWalksArraysAndMaps(t *testing.T) {
	_, stored := explainFake(t)

	containers := explainCall(t, stored, "apps", "v1", "Deployment", "spec.template.spec.containers")
	if containers.Type != "[]Container" || !containers.Required {
		t.Errorf("containers %+v", containers)
	}

	if name := childNamed(t, containers, "name"); !name.Required || name.Type != "string" {
		t.Errorf("containers.name %+v", name)
	}

	if image := childNamed(t, containers, "image"); image.Required {
		t.Errorf("containers.image %+v", image)
	}

	// Through the array's items (a direct $ref for initContainers) and an index in the path.
	for _, path := range []string{"spec.template.spec.containers.ports.protocol", "spec.template.spec.initContainers[0].ports[1].protocol"} {
		protocol := explainCall(t, stored, "apps", "v1", "Deployment", path)
		if !slices.Equal(protocol.Enum, []string{"SCTP", "TCP", "UDP"}) {
			t.Errorf("%s %+v", path, protocol)
		}
	}

	limits := explainCall(t, stored, "apps", "v1", "Deployment", "spec.template.spec.containers.resources.limits")
	if limits.Type != "map[string]Quantity" || len(limits.Children) != 0 {
		t.Errorf("limits %+v", limits)
	}

	// A map's key, dots included, then its value.
	label := explainCall(t, stored, "apps", "v1", "Deployment", "spec.selector.matchLabels.app.kubernetes.io/name")
	if label.Type != "string" {
		t.Errorf("label %+v", label)
	}

	root := explainCall(t, stored, "apps", "v1", "Deployment", "")
	if root.Type != "Deployment" || len(root.Children) != 5 || root.Children[0].Name != "apiVersion" {
		t.Errorf("root %+v", root)
	}

	if spec := childNamed(t, explainCall(t, stored, "apps", "v1", "Deployment", "spec"), "template"); !spec.Required || spec.Type != "PodTemplateSpec" {
		t.Errorf("spec.template %+v", spec)
	}
}

func TestKubeExplainErrors(t *testing.T) {
	_, stored := explainFake(t)

	if _, err := KubeExplain(stored, "admin@test", "", "apps", "v1", "Deployment", "spec.template.spec.nope"); err == nil ||
		err.Error() != "Deployment has no field spec.template.spec.nope" {
		t.Errorf("unknown field: %v", err)
	}

	if _, err := KubeExplain(stored, "admin@test", "", "apps", "v1", "StatefulSet", ""); err == nil ||
		!strings.HasPrefix(err.Error(), explainUnavailablePrefix) {
		t.Errorf("unknown kind: %v", err)
	}

	if _, err := KubeExplain(stored, "admin@test", "", "batch", "v1", "Job", ""); err == nil ||
		!strings.HasPrefix(err.Error(), explainUnavailablePrefix) {
		t.Errorf("group-version not published: %v", err)
	}

	if _, err := KubeExplain(stored, "admin@test", "", "apps", "v1", "Deploy ment", ""); err == nil {
		t.Error("an invalid kind must be refused")
	}
}

func TestKubeExplainWithoutOpenAPIV3(t *testing.T) {
	freshExplainCache(t)

	f := newFakeKubeAPI(t, map[string]string{})

	_, err := KubeExplain(kubeStoreFor(t, f), "admin@test", "", "apps", "v1", "Deployment", "spec")
	if err == nil || !strings.HasPrefix(err.Error(), explainUnavailablePrefix+": the API server does not publish OpenAPI v3") {
		t.Fatalf("err %v", err)
	}
}

func TestKubeExplainCachesDocuments(t *testing.T) {
	f, stored := explainFake(t)

	explainCall(t, stored, "apps", "v1", "Deployment", "spec")
	explainCall(t, stored, "apps", "v1", "Deployment", "spec.replicas")

	fetches := map[string]int{}
	for _, r := range f.recorded() {
		fetches[r.path]++
	}

	if fetches["/openapi/v3"] != 1 || fetches["/openapi/v3/apis/apps/v1"] != 1 {
		t.Errorf("fetches %v", fetches)
	}
}

func TestExplainCacheBoundsDocuments(t *testing.T) {
	c := newExplainCache()
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	for i := range explainMaxDocs + 3 {
		now = now.Add(time.Second)

		c.mu.Lock()
		c.evictLocked()
		c.docs[string(rune('a'+i))] = explainDocEntry{doc: &explainDoc{}, at: now}
		c.mu.Unlock()
	}

	if len(c.docs) != explainMaxDocs {
		t.Fatalf("%d documents kept", len(c.docs))
	}

	if _, ok := c.docs[string(rune('a'+explainMaxDocs+2))]; !ok {
		t.Error("the newest document must stay")
	}

	now = now.Add(explainDocTTL)

	c.mu.Lock()
	c.evictLocked()
	c.mu.Unlock()

	if len(c.docs) != 0 {
		t.Errorf("expired documents kept: %d", len(c.docs))
	}
}

func TestKubeExplainDemo(t *testing.T) {
	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeExplain(demo, demoKubeContext, "", "", "v1", "ConfigMap", "data")
	if err != nil || !strings.Contains(out, `"type":"map[string]string"`) {
		t.Fatalf("%s %v", out, err)
	}

	if _, err := KubeExplain(demo, demoKubeContext, "", "batch", "v1", "CronJob", ""); err == nil ||
		!strings.HasPrefix(err.Error(), explainUnavailablePrefix) {
		t.Errorf("demo CronJob: %v", err)
	}
}

func TestFirstSentence(t *testing.T) {
	for in, want := range map[string]string{
		"One. Two.":                   "One.",
		"Spans\nlines. Then more":     "Spans lines.",
		"Para one\n\nPara two. Three": "Para one",
		"ex: 1.5 cores":               "ex: 1.5 cores",
	} {
		if got := firstSentence(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
