package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Shaped like the Dragonfly operator's objects on a real cluster: one CR per instance, its pods
// labelled app.kubernetes.io/name=dragonfly, app=<instance> and role=master|replica.
const dragonflyFixture = `{"items":[
  {"metadata":{"name":"cache","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}},
  {"metadata":{"name":"queue","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}},
  {"metadata":{"name":"down","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}},
  {"metadata":{"name":"orphan","namespace":"app"},"spec":{"replicas":1},"status":{"phase":"Configuring-replication"}},
  {"metadata":{"name":"rolling","namespace":"app"},"spec":{"replicas":1},"status":{"phase":"Rolling-update"}}]}`

func dragonflyPods() []dsPod {
	pod := func(instance, name, node, role string, ready bool) dsPod {
		labels := map[string]string{"app.kubernetes.io/name": "dragonfly", "app": instance, "role": role}
		p := fakePod("app", name, node, ready, labels, "dragonfly", "docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.0")
		if !ready {
			p.Status.Phase, p.Spec.NodeName = "Pending", ""
		}

		return p
	}

	return []dsPod{
		pod("cache", "cache-1", "node-2", "replica", true),
		pod("cache", "cache-0", "node-1", "master", true),
		pod("queue", "queue-0", "node-1", "master", true),
		pod("queue", "queue-1", "", "replica", false),
		pod("down", "down-0", "", "master", false),
		pod("down", "down-1", "", "replica", false),
		pod("orphan", "orphan-0", "node-3", "replica", true),
		pod("rolling", "rolling-0", "node-3", "master", true),
	}
}

func TestMapDragonfly(t *testing.T) {
	var list kubeList[dragonflyObject]
	if err := json.Unmarshal([]byte(dragonflyFixture), &list); err != nil {
		t.Fatal(err)
	}

	out := mapDragonfly(list.Items, dragonflyPods())

	type summary struct {
		Name, Health, Master string
		Ready                int
		Reasons              []string
	}

	var got []summary
	for _, inst := range out.Instances {
		got = append(got, summary{inst.Name, inst.Health, inst.Master, inst.ReadyPods, inst.Reasons})
	}

	want := []summary{
		{"down", healthCritical, "down-0", 0, []string{dragonflyReasonNoReady}},
		{"orphan", healthCritical, "", 1, []string{dragonflyReasonNoMaster}},
		{"queue", healthWarning, "queue-0", 1, []string{dragonflyReasonPods}},
		{"rolling", healthWarning, "rolling-0", 1, []string{dragonflyReasonNotReady}},
		{"cache", healthOK, "cache-0", 2, []string{}},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("instances differ")
	}

	// The master first, with the role label as the operator set it.
	cache := out.Instances[4]
	if cache.Pods[0].Name != "cache-0" || cache.Pods[0].Role != "master" || cache.Pods[1].Role != "replica" || cache.Pods[0].Node != "node-1" {
		t.Errorf("pods: %+v", cache.Pods)
	}
}

func TestDragonflyTwoMasters(t *testing.T) {
	inst := mapDragonflyInstance(dragonflyObject{}, []dragonflyPod{
		{Name: "a", Role: "master", Ready: true}, {Name: "b", Role: "master", Ready: true},
	})
	if inst.Health != healthWarning || !slices.Equal(inst.Reasons, []string{dragonflyReasonMasters}) {
		t.Fatalf("two masters: %+v", inst)
	}
}

func TestReadDataServicesDragonfly(t *testing.T) {
	podJSON, err := json.Marshal(map[string]any{"items": dragonflyPods()})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"dragonflydb.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/dragonflydb.io/v1alpha1/dragonflies": dragonflyFixture,
		"GET /api/v1/pods": string(podJSON),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints("dragonfly"), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.Dragonfly == nil || res.Dragonfly.Error != "" || len(res.Dragonfly.Instances) != 5 || res.Dragonfly.Version != "v1alpha1" {
		t.Fatalf("dragonfly: %+v", res.Dragonfly)
	}

	if res.Longhorn != nil || res.CNPG != nil || res.Garage != nil {
		t.Errorf("only Dragonfly is installed: %+v", res)
	}

	// Its pods come from a label-selected listing, not the full one.
	for _, r := range f.recorded() {
		if r.path == "/api/v1/pods" {
			return
		}
	}

	t.Error("pods not listed")
}

func TestDragonflyDemoHasEveryState(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.Dragonfly == nil || len(demo.Dragonfly.Instances) < 2 {
		t.Fatal("demo misses Dragonfly")
	}

	var states []string
	for _, inst := range demo.Dragonfly.Instances {
		states = append(states, inst.Health)
	}

	if !strings.Contains(strings.Join(states, ","), healthWarning) || !strings.Contains(strings.Join(states, ","), healthOK) {
		t.Errorf("demo states: %v", states)
	}
}
