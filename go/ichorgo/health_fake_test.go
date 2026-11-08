package ichorgo

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// healthRecorder is a HealthListener that keeps everything it is told.
type healthRecorder struct {
	mu       sync.Mutex
	progress []string // "node: message"
	started  chan struct{}
	done     chan string
}

func newHealthRecorder() *healthRecorder {
	return &healthRecorder{started: make(chan struct{}, 1), done: make(chan string, 1)}
}

func (r *healthRecorder) OnProgress(node, message string) {
	r.mu.Lock()
	r.progress = append(r.progress, node+": "+message)
	r.mu.Unlock()

	select {
	case r.started <- struct{}{}:
	default:
	}
}

func (r *healthRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *healthRecorder) wait(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(20 * time.Second):
		t.Fatal("OnDone not called")

		return ""
	}
}

func healthStep(host, msg string) *clusterapi.HealthCheckProgress {
	//lint:ignore SA1019 the health stream still reports the node and its error in the deprecated Metadata
	return &clusterapi.HealthCheckProgress{Metadata: &common.Metadata{Hostname: host}, Message: msg}
}

func TestStartClusterHealthFake(t *testing.T) {
	type healthScript = func(node string, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error

	tests := []struct {
		name         string
		health       healthScript
		wantErr      string
		wantProgress []string
	}{
		{
			name: "healthy",
			health: func(node string, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error {
				_ = stream.Send(healthStep(node, "waiting for etcd to be healthy: OK"))

				return stream.Send(healthStep(node, "waiting for all k8s nodes to report ready: OK"))
			},
			wantProgress: []string{
				"192.0.2.21: waiting for etcd to be healthy: OK",
				"192.0.2.21: waiting for all k8s nodes to report ready: OK",
			},
		},
		{
			name: "times out on a failing node",
			health: func(node string, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error {
				_ = stream.Send(healthStep(node, "waiting for all k8s nodes to report ready: node 192.0.2.22 not ready"))

				return status.Error(codes.DeadlineExceeded, "context deadline exceeded")
			},
			wantErr: "not healthy after 1m0s: waiting for all k8s nodes to report ready: node 192.0.2.22 not ready",
		},
		{
			name: "fails on the runner",
			health: func(node string, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error {
				//lint:ignore SA1019 the health stream still reports the node and its error in the deprecated Metadata
				return stream.Send(&clusterapi.HealthCheckProgress{Metadata: &common.Metadata{Hostname: node, Error: "etcd is not healthy"}})
			},
			wantErr: "etcd is not healthy",
		},
		{
			name: "needs os:admin",
			health: func(string, grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error {
				return status.Error(codes.PermissionDenied, "not authorized")
			},
			wantErr: "needs an os:admin talosconfig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeTalos()
			f.addNode(t, "192.0.2.21", "v1.11.0", machine.TypeControlPlane)
			f.addNode(t, "192.0.2.22", "v1.11.0", machine.TypeWorker)
			f.health = tt.health

			r := newHealthRecorder()
			StartClusterHealth(f.start(t, "192.0.2.22", "192.0.2.21"), "fake", r)

			if got := r.wait(t); !strings.Contains(got, tt.wantErr) || (tt.wantErr == "" && got != "") {
				t.Fatalf("OnDone(%q), want %q", got, tt.wantErr)
			}

			if tt.wantProgress != nil && strings.Join(r.progress, "\n") != strings.Join(tt.wantProgress, "\n") {
				t.Errorf("progress = %q", r.progress)
			}

			// The check runs on the control plane, never on the worker listed first.
			if got := f.called("HealthCheck"); len(got) != 1 || got[0] != "HealthCheck 192.0.2.21" {
				t.Errorf("health checks = %v", got)
			}
		})
	}
}

// A member whose first address is an overlay one the Talos API does not answer on (a VPN
// interface) is checked at the next address that answers. An address the talosconfig lists
// goes first, then the LAN ones, then overlay ones, even when the overlay one answers the
// endpoint too (a worker the talosconfig does not list). One that answers on none keeps its
// first address in that order, so the check still reports it down.
func TestStartClusterHealthFakeReachableMembers(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.21", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.22", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "100.64.0.4", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.24", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "100.64.0.5", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.25", "v1.11.0", machine.TypeWorker)
	f.health = func(string, grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error { return nil }

	member := func(id string, typ machine.Type, addresses ...string) *cluster.Member {
		m := cluster.NewMember(cluster.NamespaceName, id)
		m.TypedSpec().Hostname = id
		m.TypedSpec().MachineType = typ
		m.TypedSpec().Addresses = addrs(addresses...)

		return m
	}

	f.put("192.0.2.21",
		member("cp-1", machine.TypeControlPlane, "100.64.0.1", "192.0.2.21"),
		member("worker-1", machine.TypeWorker, "fe80::2", "100.64.0.2", "192.0.2.22"),
		member("worker-2", machine.TypeWorker, "100.64.0.3", "192.0.2.23"),
		member("worker-3", machine.TypeWorker),
		member("worker-4", machine.TypeWorker, "100.64.0.4", "192.0.2.24"),
		member("worker-5", machine.TypeWorker, "100.64.0.5", "192.0.2.25"),
		member("worker-6", machine.TypeWorker, "100.64.0.6"),
	)

	r := newHealthRecorder()
	StartClusterHealth(f.start(t, "192.0.2.21", "192.0.2.24"), "fake", r)

	if got := r.wait(t); got != "" {
		t.Fatalf("OnDone(%q)", got)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.healthInfos) != 1 {
		t.Fatalf("%d health checks", len(f.healthInfos))
	}

	info := f.healthInfos[0]
	if got := strings.Join(info.GetControlPlaneNodes(), ","); got != "192.0.2.21" {
		t.Errorf("control planes = %s", got)
	}

	if got := strings.Join(slices.Sorted(slices.Values(info.GetWorkerNodes())), ","); got != "100.64.0.6,192.0.2.22,192.0.2.23,192.0.2.24,192.0.2.25" {
		t.Errorf("workers = %s", got)
	}
}

// Without cluster discovery, Talos is left to find the nodes itself.
func TestStartClusterHealthFakeNoMembers(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.21", "v1.11.0", machine.TypeControlPlane)
	f.health = func(string, grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error { return nil }

	r := newHealthRecorder()
	StartClusterHealth(f.start(t, "192.0.2.21"), "fake", r)

	if got := r.wait(t); got != "" {
		t.Fatalf("OnDone(%q)", got)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.healthInfos) != 1 || len(f.healthInfos[0].GetControlPlaneNodes())+len(f.healthInfos[0].GetWorkerNodes()) != 0 {
		t.Errorf("cluster infos = %v", f.healthInfos)
	}
}

func TestStartClusterHealthFakeNoControlPlane(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.22", "v1.11.0", machine.TypeWorker)

	r := newHealthRecorder()
	StartClusterHealth(f.start(t, "192.0.2.22", "192.0.2.23"), "fake", r)

	if got := r.wait(t); got != errNoControlPlane.Error() {
		t.Fatalf("OnDone(%q)", got)
	}
}

func TestStartClusterHealthFakeCancel(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.21", "v1.11.0", machine.TypeControlPlane)
	f.health = func(node string, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error {
		_ = stream.Send(healthStep(node, "waiting for etcd to be healthy: ..."))
		<-stream.Context().Done()

		return stream.Context().Err()
	}

	r := newHealthRecorder()
	run := StartClusterHealth(f.start(t, "192.0.2.21"), "fake", r)

	select {
	case <-r.started:
	case <-time.After(20 * time.Second):
		t.Fatal("no progress before the cancel")
	}

	run.Cancel()

	if got := r.wait(t); got == "" {
		t.Fatal("a cancelled check must not report success")
	}
}

func TestClassifyNodesFake(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.21", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.22", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.23", "v1.11.0", machine.TypeInit)

	cfg := f.start(t, "192.0.2.21", "192.0.2.22", "192.0.2.23", "192.0.2.24")

	s, release, err := acquireSession(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	info := classifyNodes(t.Context(), s.client, targetNodes(s.context))

	if got := strings.Join(info.GetControlPlaneNodes(), ","); got != "192.0.2.21,192.0.2.23" {
		t.Errorf("control planes = %s", got)
	}

	// An unreachable node counts as a worker.
	if got := strings.Join(info.GetWorkerNodes(), ","); got != "192.0.2.22,192.0.2.24" {
		t.Errorf("workers = %s", got)
	}
}
