package ichorgo

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// overviewRecorder is an OverviewListener that hands each call to the test.
type overviewRecorder struct {
	nodes chan nodeOverview
	done  chan [2]string // overviewJSON, errMessage
}

func newOverviewRecorder() *overviewRecorder {
	return &overviewRecorder{nodes: make(chan nodeOverview, 16), done: make(chan [2]string, 2)}
}

func (r *overviewRecorder) OnNode(js string) {
	var n nodeOverview
	if err := json.Unmarshal([]byte(js), &n); err != nil {
		n.Error = "bad json: " + js
	}

	r.nodes <- n
}

func (r *overviewRecorder) OnDone(overviewJSON, errMessage string) {
	r.done <- [2]string{overviewJSON, errMessage}
}

func (r *overviewRecorder) nextNode(t *testing.T, within time.Duration) nodeOverview {
	t.Helper()

	select {
	case n := <-r.nodes:
		return n
	case <-time.After(within):
		t.Fatalf("no node within %s", within)

		return nodeOverview{}
	}
}

func (r *overviewRecorder) waitDone(t *testing.T) (string, string) {
	t.Helper()

	select {
	case d := <-r.done:
		return d[0], d[1]
	case <-time.After(callTimeout):
		t.Fatal("no OnDone")

		return "", ""
	}
}

// heldFake is a fake cluster of three nodes, the last one's Version held until release.
func heldFake(t *testing.T) (f *fakeTalos, cfg string, release func()) {
	t.Helper()

	f = newFakeTalos()
	f.addNode(t, "192.0.2.81", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.82", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.83", "v1.11.0", machine.TypeWorker)

	hold := make(chan struct{})
	f.versionHold = map[string]chan struct{}{"192.0.2.83": hold}

	var once sync.Once

	release = func() { once.Do(func() { close(hold) }) }

	t.Cleanup(release)

	return f, f.start(t, "192.0.2.81", "192.0.2.82", "192.0.2.83"), release
}

// The two quick nodes arrive while the slow one is still held; it comes last, then OnDone.
func TestStartClusterOverviewSlowNodeDoesNotDelayOthers(t *testing.T) {
	_, cfg, release := heldFake(t)
	r := newOverviewRecorder()

	StartClusterOverview(cfg, "fake", r)

	seen := map[string]bool{}
	for range 2 {
		n := r.nextNode(t, 2*time.Second)
		if !n.Reachable || n.Hostname == n.Node {
			t.Errorf("streamed node = %+v", n)
		}

		seen[n.Node] = true
	}

	if !seen["192.0.2.81"] || !seen["192.0.2.82"] {
		t.Fatalf("first nodes = %v, want the two quick ones", seen)
	}

	select {
	case d := <-r.done:
		t.Fatalf("OnDone before the slow node: %v", d)
	default:
	}

	release()

	if n := r.nextNode(t, 2*time.Second); n.Node != "192.0.2.83" || !n.Reachable {
		t.Errorf("last node = %+v", n)
	}

	out, errMessage := r.waitDone(t)
	overview := decodeJSON[clusterOverview](t, out, nil)

	if errMessage != "" || len(overview.Nodes) != 3 {
		t.Errorf("done = %d nodes, %q", len(overview.Nodes), errMessage)
	}
}

// The complete overview is ClusterOverview's, byte for byte, down node included.
func TestStartClusterOverviewDoneEqualsClusterOverview(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.81", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.82", "v1.11.0", machine.TypeWorker)

	cfg := f.start(t, "192.0.2.81", "192.0.2.82", "192.0.2.89") // .89 is down

	want, err := ClusterOverview(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}

	r := newOverviewRecorder()
	StartClusterOverview(cfg, "fake", r)

	got, errMessage := r.waitDone(t)
	if errMessage != "" || got != want {
		t.Errorf("stream done = %s (%q)\nClusterOverview = %s", got, errMessage, want)
	}

	if len(r.nodes) != 3 {
		t.Errorf("streamed %d nodes, want 3", len(r.nodes))
	}

	for range len(r.nodes) {
		if n := <-r.nodes; n.Node == "192.0.2.89" && (n.Reachable || n.ErrorKind != errorKindNetwork) {
			t.Errorf("down node = %+v", n)
		}
	}
}

func TestStartClusterOverviewStopSendsNoDone(t *testing.T) {
	_, cfg, release := heldFake(t)
	r := newOverviewRecorder()

	run := StartClusterOverview(cfg, "fake", r)
	r.nextNode(t, 2*time.Second)
	run.Stop()
	release()

	select {
	case d := <-r.done:
		t.Fatalf("OnDone after Stop: %v", d)
	case <-time.After(500 * time.Millisecond):
	}
}

// slowVersion is how long the slow node takes to answer Version: past nodeDialTimeout,
// within nodeTimeout.
const slowVersion = 4 * time.Second

// A node whose Version takes 4 s is reported down by the stream after nodeDialTimeout, but
// ClusterOverview (the monitor, the widgets) still waits for it and sees it reachable.
func TestOverviewSlowVersionStreamDownClusterOverviewReachable(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for a slow node")
	}

	byNode := func(nodes []nodeOverview) nodeOverview {
		for _, n := range nodes {
			if n.Node == "192.0.2.83" {
				return n
			}
		}

		return nodeOverview{}
	}

	t.Run("stream", func(t *testing.T) {
		_, cfg, release := heldFake(t)
		time.AfterFunc(slowVersion, release)

		r := newOverviewRecorder()
		begin := time.Now()

		StartClusterOverview(cfg, "fake", r)

		var streamed []nodeOverview
		for range 3 {
			streamed = append(streamed, r.nextNode(t, nodeTimeout))
		}

		elapsed := time.Since(begin)
		if n := byNode(streamed); n.Reachable || n.ErrorKind != errorKindNetwork || elapsed >= slowVersion {
			t.Errorf("slow node streamed after %s = %+v", elapsed, n)
		}

		out, errMessage := r.waitDone(t)
		if n := byNode(decodeJSON[clusterOverview](t, out, nil).Nodes); n.Reachable || errMessage != "" {
			t.Errorf("slow node in the streamed overview = %+v (%q)", n, errMessage)
		}
	})

	t.Run("ClusterOverview", func(t *testing.T) {
		_, cfg, release := heldFake(t)
		time.AfterFunc(slowVersion, release)

		out, err := ClusterOverview(cfg, "fake")
		if n := byNode(decodeJSON[clusterOverview](t, out, err).Nodes); !n.Reachable {
			t.Errorf("slow node in ClusterOverview = %+v", n)
		}
	})
}

func TestStartClusterOverviewDemoStreamsFiveNodes(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	want, err := ClusterOverview(demo, "Demo cluster")
	if err != nil {
		t.Fatal(err)
	}

	r := newOverviewRecorder()
	begin := time.Now()

	StartClusterOverview(demo, "Demo cluster", r)

	for i, d := range demoNodes() {
		if n := r.nextNode(t, time.Second); n.Node != d.Node {
			t.Errorf("demo node %d = %s, want %s", i, n.Node, d.Node)
		}
	}

	got, errMessage := r.waitDone(t)
	if errMessage != "" || got != want {
		t.Errorf("demo done = %s (%q)\nwant %s", got, errMessage, want)
	}

	if elapsed := time.Since(begin); elapsed < 4*demoNodeGap {
		t.Errorf("demo streamed in %s, want the nodes %s apart", elapsed, demoNodeGap)
	}
}
