package ichorgo

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRemoveContextNodes(t *testing.T) {
	withDataDir(t)

	stored, err := AddContextNodes(mergeStored, "prod", "10.0.0.3,[fd00::5],node-4.example.com")
	if err != nil {
		t.Fatal(err)
	}

	out, err := RemoveContextNodes(stored, "prod", "10.0.0.3, [FD00::5], NODE-4.example.com")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)
	if got, want := cfg.Contexts["prod"].Nodes, []string{"10.0.0.1"}; !slices.Equal(got, want) {
		t.Errorf("prod nodes = %v, want %v", got, want)
	}

	if got := cfg.Contexts["prod"].Endpoints; !slices.Equal(got, []string{"10.0.0.1", "10.0.0.2"}) {
		t.Errorf("prod endpoints changed: %v", got)
	}

	if cfg.Contexts["prod"].Crt != "b2xkLWNydA==" || cfg.Context != "prod" || len(cfg.Contexts["lab"].Nodes) != 0 {
		t.Error("credentials, current context or another context changed")
	}

	if got := readAudit(t, "", "forget"); len(got) != 1 || got[0].Outcome != auditOK {
		t.Errorf("audit %+v", got)
	}
}

func TestRemoveContextNodesEndpointOnlyContext(t *testing.T) {
	withDataDir(t)

	stored := strings.Replace(mergeStored, "endpoints: [10.1.0.1]", "endpoints: [10.1.0.1, 10.1.0.2]", 1)

	out, err := RemoveContextNodes(stored, "lab", "10.1.0.2")
	if err != nil {
		t.Fatal(err)
	}

	lab := mustConfig(t, out).Contexts["lab"]
	if !slices.Equal(lab.Nodes, []string{"10.1.0.1"}) || !slices.Equal(lab.Endpoints, []string{"10.1.0.1", "10.1.0.2"}) {
		t.Errorf("lab nodes %v, endpoints %v", lab.Nodes, lab.Endpoints)
	}
}

func TestRemoveContextNodesRefuses(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, stored, context, nodes string }{
		{"last node", mergeStored, "prod", "10.0.0.1"},
		{"last endpoint target", mergeStored, "lab", "10.1.0.1"},
		{"missing node", mergeStored, "prod", "10.0.0.99"},
		{"one of two missing", mergeStored, "prod", "10.0.0.1,10.0.0.99"},
		{"missing context", mergeStored, "missing", "10.0.0.1"},
		{"bad yaml", "not: [yaml", "prod", "10.0.0.1"},
		{"demo", demo, "Demo cluster", "192.0.2.10"},
	}

	for _, c := range cases {
		if _, err := RemoveContextNodes(c.stored, c.context, c.nodes); err == nil {
			t.Errorf("%s: want an error", c.name)
		}
	}
}

func TestContextNodeRemovalPlan(t *testing.T) {
	cases := []struct {
		context, node     string
		allowed, endpoint bool
	}{
		{"prod", "10.0.0.1", false, true}, // the last node, also an endpoint
		{"prod", "10.0.0.9", false, false},
	}

	for _, c := range cases {
		out, err := ContextNodeRemovalPlan(mergeStored, c.context, c.node)
		if err != nil {
			t.Fatal(err)
		}

		var got contextNodeRemoval
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}

		if got.Allowed != c.allowed || got.Endpoint != c.endpoint || got.Reason == "" {
			t.Errorf("%s: %+v", c.node, got)
		}
	}

	stored, err := AddContextNodes(mergeStored, "prod", "10.0.0.3")
	if err != nil {
		t.Fatal(err)
	}

	out, err := ContextNodeRemovalPlan(stored, "prod", "10.0.0.3")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"allowed":true`) || !strings.Contains(out, `"endpoint":false`) {
		t.Errorf("plan %s", out)
	}
}

func TestIsForgettable(t *testing.T) {
	members := []clusterMember{
		{hostname: "cp-1", role: "controlplane", addresses: addrs("10.0.0.1")},
		{hostname: "worker-1", role: "worker", addresses: addrs("2001:db8::11", "10.0.0.11")},
	}
	targets := []string{"10.0.0.1", "10.0.0.11", "10.0.0.12", "worker-1"}

	cases := []struct {
		name, node string
		hostnames  []string
		members    []clusterMember
		want       bool
	}{
		{"gone from the cluster", "10.0.0.12", []string{"cp-1", "worker-1", "", ""}, members, true},
		{"rebooting: still a member", "10.0.0.11", []string{"cp-1", "", "", ""}, members, false},
		{"still a member by hostname", "worker-1", []string{"cp-1", "", "", ""}, members, false},
		{"it answers", "10.0.0.12", []string{"cp-1", "worker-1", "worker-2", ""}, members, false},
		{"no member read", "10.0.0.12", []string{"cp-1", "", "", ""}, nil, false},
		{"nothing answers", "10.0.0.12", []string{"", "", "", ""}, members, false},
		{"list misses a live node", "10.0.0.12", []string{"cp-1", "new-node", "", ""}, members, false},
	}

	for _, c := range cases {
		if got := isForgettable(c.node, targets, c.hostnames, c.members); got != c.want {
			t.Errorf("%s: got %t", c.name, got)
		}
	}
}

func TestNodeForgettableDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := NodeForgettable(demo, "", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"forgettable":false`) {
		t.Errorf("demo: %s", out)
	}
}

const departedNodes = `{"items":[{"metadata":{"name":"worker-1"},"status":{"conditions":[{"type":"Ready","status":"%READY%"}],"addresses":[{"type":"InternalIP","address":"10.0.0.11"}]}}]}`

func withFastDepartedPoll(t *testing.T) {
	t.Helper()

	before := departedNodePoll
	departedNodePoll = time.Millisecond

	t.Cleanup(func() { departedNodePoll = before })
}

func TestKubeDeleteDepartedNodeWaitsForTheKubelet(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())
	withFastDepartedPoll(t)

	f := newFakeKubeAPI(t, map[string]string{"DELETE /api/v1/nodes/worker-1": `{"kind":"Status","status":"Success"}`})
	lists := 0
	f.answerFunc = func(method, path, _ string) (string, bool) {
		if method != http.MethodGet || path != "/api/v1/nodes" {
			return "", false
		}

		lists++
		if lists == 1 {
			return strings.ReplaceAll(departedNodes, "%READY%", "True"), true
		}

		return strings.ReplaceAll(departedNodes, "%READY%", "Unknown"), true
	}

	// Found by address: the hostname Talos gave may not be the Node's name.
	if err := KubeDeleteDepartedNode(kubeStoreFor(t, f), "admin@test", "", "worker-1.example.com", "10.0.0.11", 30); err != nil {
		t.Fatal(err)
	}

	if lists != 2 || len(deleteRequests(f)) != 1 {
		t.Errorf("lists %d, deletes %+v", lists, deleteRequests(f))
	}
}

func TestKubeDeleteDepartedNodeTimesOutWhileReady(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())
	withFastDepartedPoll(t)

	f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": strings.ReplaceAll(departedNodes, "%READY%", "True")})

	err := KubeDeleteDepartedNode(kubeStoreFor(t, f), "admin@test", "", "worker-1", "", 0)
	if err == nil || !strings.Contains(err.Error(), "still answers") {
		t.Fatalf("err = %v", err)
	}

	if len(deleteRequests(f)) != 0 {
		t.Errorf("a Ready node was deleted: %+v", deleteRequests(f))
	}
}

func TestKubeDeleteDepartedNodeAlreadyGone(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": strings.ReplaceAll(departedNodes, "%READY%", "False")})

	if err := KubeDeleteDepartedNode(kubeStoreFor(t, f), "admin@test", "", "worker-9", "10.0.0.99", 30); err != nil {
		t.Fatal(err)
	}

	if len(deleteRequests(f)) != 0 {
		t.Errorf("another node was deleted: %+v", deleteRequests(f))
	}
}

func TestKubeDeleteDepartedNodeDemo(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	err = KubeDeleteDepartedNode(demo, "Demo cluster", "", "demo-worker-1", "192.0.2.20", 30)
	if !errors.Is(err, errDemoUnavailable) && (err == nil || !strings.Contains(err.Error(), errDemoUnavailable.Error())) {
		t.Errorf("err = %v", err)
	}
}
