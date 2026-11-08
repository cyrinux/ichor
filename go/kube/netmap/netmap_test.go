package netmap

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/kube"
)

func decode[T any](t *testing.T, js string) T {
	t.Helper()

	var out T
	if err := json.Unmarshal([]byte(js), &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func pod(ns, name, app, node, status string, healthy bool) Pod {
	return Pod{Pod: kube.Pod{Namespace: ns, Name: name, Node: node, Status: status, Healthy: healthy, Ready: 1, Containers: 1}, Labels: map[string]string{"app": app}}
}

func box(t *testing.T, m Map, id string) Node {
	t.Helper()

	for _, n := range m.Nodes {
		if n.ID == id {
			return n
		}
	}

	t.Fatalf("no box %s in %+v", id, m.Nodes)

	return Node{}
}

func TestPodHealth(t *testing.T) {
	cases := map[string]string{
		"Running": kube.HealthWarning, "Pending": kube.HealthWarning, "Init:0/1": kube.HealthWarning, "Completed": kube.HealthIdle,
		"CrashLoopBackOff": kube.HealthCritical, "ImagePullBackOff": kube.HealthCritical, "Init:CrashLoopBackOff": kube.HealthCritical, "Error": kube.HealthCritical,
	}

	for status, want := range cases {
		if got := PodHealth(kube.Pod{Status: status}); got != want {
			t.Errorf("%s: %s, want %s", status, got, want)
		}
	}

	if PodHealth(kube.Pod{Status: "Running", Healthy: true}) != kube.HealthOK {
		t.Error("healthy pod")
	}
}

// One entry layer in front: a host → the Service → two pods on two nodes, one of them down.
func TestBackendHealthAndRootCause(t *testing.T) {
	g := NewGraph(1)
	svc := decode[Service](t, `{"metadata":{"name":"web","namespace":"shop"},"spec":{"selector":{"app":"web"},"ports":[{"port":80,"targetPort":8080}]}}`)
	pods := []Pod{
		pod("shop", "web-a", "web", "node-1", "Running", true),
		pod("shop", "web-b", "web", "node-2", "Running", true),
		pod("shop", "other", "db", "node-1", "Running", true),   // not selected
		pod("other", "web-c", "web", "node-1", "Running", true), // another namespace
	}

	sid := AddBackend(g, svc, pods)
	if sid != ServiceID("shop", "web") {
		t.Fatalf("service id %s", sid)
	}

	host := g.Add(Node{ID: "host/web", Layer: 0, Kind: "Host", Name: "web.example"})
	g.Link(host, sid)

	nodes := []NodeObject{
		decode[NodeObject](t, `{"metadata":{"name":"node-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`),
		decode[NodeObject](t, `{"metadata":{"name":"node-2"},"status":{"conditions":[{"type":"Ready","status":"False"}]}}`),
	}
	m := Finish(g, nodes)

	if len(m.Nodes) != 6 {
		t.Fatalf("boxes: %+v", m.Nodes)
	}

	if n := box(t, m, "node/node-2"); n.Health != kube.HealthCritical || n.Detail != "NotReady" || n.Layer != 3 {
		t.Errorf("node-2: %+v", n)
	}

	// The pods are ready (their node's state is its own box), so the Service and the host are ok.
	if h := box(t, m, sid).Health; h != kube.HealthOK {
		t.Errorf("service: %s", h)
	}

	if h := box(t, m, "host/web").Health; h != kube.HealthOK {
		t.Errorf("host: %s", h)
	}

	if m.Problem == nil || m.Problem.Kind != KindNode || m.Problem.Name != "node-2" {
		t.Errorf("problem: %+v", m.Problem)
	}

	for _, e := range m.Edges {
		if e.Health != box(t, m, e.To).Health {
			t.Errorf("edge %s→%s carries %s", e.From, e.To, e.Health)
		}
	}
}

// A Service without a selector is idle; with front layers, a broken entry box is never the
// root cause, but a Service of a map without front layers is.
func TestRootCauseSkipsEntryPoints(t *testing.T) {
	g := NewGraph(1)
	g.Add(Node{ID: "host/lost", Layer: 0, Kind: "Host"}) // nothing behind it: critical

	if m := Finish(g, nil); box(t, m, "host/lost").Health != kube.HealthCritical || m.Problem != nil {
		t.Errorf("entry box: %+v", m)
	}

	g = NewGraph(0)
	sid := AddBackend(g, decode[Service](t, `{"metadata":{"name":"web","namespace":"shop"},"spec":{"selector":{"app":"web"}}}`), nil)

	if m := Finish(g, nil); m.Problem == nil || m.Problem.Kind != KindService || box(t, m, sid).Layer != 0 {
		t.Errorf("no front layers: %+v", m)
	}

	g = NewGraph(0)
	sid = AddBackend(g, decode[Service](t, `{"metadata":{"name":"ext","namespace":"shop"},"spec":{"type":"ExternalName"}}`), nil)

	if m := Finish(g, nil); box(t, m, sid).Health != kube.HealthIdle || m.Problem != nil {
		t.Errorf("selectorless Service: %+v", m)
	}
}

func TestGraphAddsOnce(t *testing.T) {
	g := NewGraph(0)
	g.Add(Node{ID: "a"})
	g.Add(Node{ID: "a", Managed: true})
	g.Add(Node{ID: "b"})
	g.Link("a", "b")
	g.Link("a", "b")
	g.Link("a", "")

	m := Finish(g, nil)
	if len(m.Nodes) != 2 || len(m.Edges) != 1 || !box(t, m, "a").Managed {
		t.Errorf("%+v", m)
	}

	if g.Node("missing") != nil {
		t.Error("missing box found")
	}
}

func TestServiceAndPodDetail(t *testing.T) {
	svc := decode[Service](t, `{"spec":{"type":"NodePort","clusterIP":"10.96.0.12","ports":[{"port":80,"targetPort":8080,"nodePort":30080},{"port":443,"targetPort":"https"},{"port":53,"targetPort":53}]}}`)
	if got, want := ServiceDetail(svc), "NodePort 10.96.0.12 · 80→8080 (node 30080), 443→https, 53"; got != want {
		t.Errorf("service: %q, want %q", got, want)
	}

	headless := decode[Service](t, `{"spec":{"clusterIP":"None"}}`)
	if got := ServiceDetail(headless); got != "ClusterIP headless" {
		t.Errorf("headless: %q", got)
	}

	if got := PodDetail(kube.Pod{Status: "Running", Ready: 1, Containers: 2}); got != "Running · 1/2 ready" {
		t.Errorf("pod: %q", got)
	}

	if got := PodDetail(kube.Pod{Status: "CrashLoopBackOff", Restarts: 3}); !strings.HasSuffix(got, "3 restarts") {
		t.Errorf("pod: %q", got)
	}
}
