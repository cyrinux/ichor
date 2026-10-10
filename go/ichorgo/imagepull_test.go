package ichorgo

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

const prepullImage = "registry.example.com/sample/installer:v1.11.1"

// pullRecorder is an ImagePullListener that keeps every progress event.
type pullRecorder struct {
	mu     sync.Mutex
	events []imagePullProgress
	states chan imagePullProgress
	done   chan string
}

func newPullRecorder() *pullRecorder {
	return &pullRecorder{states: make(chan imagePullProgress, 64), done: make(chan string, 1)}
}

func (r *pullRecorder) OnProgress(s string) {
	var p imagePullProgress
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	r.mu.Lock()
	r.events = append(r.events, p)
	r.mu.Unlock()

	r.states <- p
}

func (r *pullRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *pullRecorder) wait(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(30 * time.Second):
		t.Fatal("OnDone not called")

		return ""
	}
}

func (r *pullRecorder) last() imagePullProgress {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.events[len(r.events)-1]
}

// nodeStates is "hostname=state" per node of p, in order.
func nodeStates(p imagePullProgress) string {
	out := make([]string, len(p.Nodes))
	for i, n := range p.Nodes {
		out[i] = n.Hostname + "=" + n.State
	}

	return strings.Join(out, ",")
}

func pullCluster(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, "192.0.2.61", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.62", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.63", "v1.11.0", machine.TypeWorker)

	return f, f.start(t, "192.0.2.61", "192.0.2.62", "192.0.2.63")
}

func TestStartImagePullEveryNode(t *testing.T) {
	withDataDir(t)

	f, cfg := pullCluster(t)

	var (
		mu   sync.Mutex
		reqs []*machineapi.ImagePullRequest
	)

	f.imagePull = func(_ string, req *machineapi.ImagePullRequest) error {
		mu.Lock()
		reqs = append(reqs, req)
		mu.Unlock()

		return nil
	}

	r := newPullRecorder()
	StartImagePull(cfg, "fake", "", " "+prepullImage+" ", imagePullSystem, r)

	if msg := r.wait(t); msg != "" {
		t.Fatalf("pull failed: %s", msg)
	}

	if first := r.events[0]; nodeStates(first) != "host-192-0-2-61=pending,host-192-0-2-62=pending,host-192-0-2-63=pending" {
		t.Errorf("first event = %s", nodeStates(first))
	}

	last := r.last()
	if nodeStates(last) != "host-192-0-2-61=done,host-192-0-2-62=done,host-192-0-2-63=done" || last.Done != 3 || last.Total != 3 {
		t.Errorf("last event = %s (%d/%d)", nodeStates(last), last.Done, last.Total)
	}

	if len(reqs) != 3 {
		t.Fatalf("pulls = %d", len(reqs))
	}

	for _, req := range reqs {
		if req.GetReference() != prepullImage || req.GetNamespace() != common.ContainerdNamespace_NS_SYSTEM {
			t.Errorf("request = %v", req)
		}
	}

	entries := readAudit(t, "fake", "image-pull")
	if len(entries) != 1 || entries[0].Params != "image="+prepullImage+" namespace=system nodes=3" || entries[0].Outcome != auditOK {
		t.Errorf("audit = %+v", entries)
	}
}

func TestStartImagePullOneNodeFails(t *testing.T) {
	withDataDir(t)

	f, cfg := pullCluster(t)
	f.imagePull = func(node string, _ *machineapi.ImagePullRequest) error {
		if node == "192.0.2.62" {
			return errors.New("failed to resolve reference: not found")
		}

		return nil
	}

	r := newPullRecorder()
	StartImagePull(cfg, "fake", "192.0.2.61, 192.0.2.62,192.0.2.63", prepullImage, imagePullCRI, r)

	msg := r.wait(t)
	if !strings.Contains(msg, "host-192-0-2-62") || !strings.Contains(msg, "not found") {
		t.Errorf("OnDone = %q", msg)
	}

	last := r.last()
	if nodeStates(last) != "host-192-0-2-61=done,host-192-0-2-62=failed,host-192-0-2-63=done" || last.Done != 2 {
		t.Errorf("last event = %s (%d done)", nodeStates(last), last.Done)
	}

	if !strings.Contains(last.Nodes[1].Error, "not found") {
		t.Errorf("node error = %q", last.Nodes[1].Error)
	}

	if entries := readAudit(t, "fake", "image-pull"); len(entries) != 1 || entries[0].Outcome != auditFailed {
		t.Errorf("audit = %+v", entries)
	}
}

func TestStartImagePullImageServiceFallback(t *testing.T) {
	withDataDir(t)

	f, cfg := pullCluster(t)

	r := newPullRecorder()
	StartImagePull(cfg, "fake", "192.0.2.63", prepullImage, imagePullCRI, r)

	if msg := r.wait(t); msg != "" {
		t.Fatalf("pull failed: %s", msg)
	}

	if got := f.called("Pull"); len(got) != 1 || got[0] != "Pull 192.0.2.63" {
		t.Errorf("ImageService pulls = %v", got)
	}
}

func TestStartImagePullCancel(t *testing.T) {
	withDataDir(t)

	f, cfg := pullCluster(t)

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	f.imagePull = func(string, *machineapi.ImagePullRequest) error {
		<-release

		return nil
	}

	r := newPullRecorder()
	run := StartImagePull(cfg, "fake", "192.0.2.61", prepullImage, imagePullSystem, r)

	for p := range r.states {
		if nodeStates(p) == "host-192-0-2-61=pulling" {
			break
		}
	}

	run.Cancel()

	if msg := r.wait(t); msg != "image pull cancelled" {
		t.Errorf("OnDone = %q", msg)
	}
}

func TestStartImagePullRefused(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	_, cfg := pullCluster(t)

	tests := []struct {
		name, cfg, context, image, namespace, want string
	}{
		{"demo", demo, "Demo cluster", prepullImage, imagePullSystem, errDemoUnavailable.Error()},
		{"no image", cfg, "fake", " ", imagePullSystem, "no image given"},
		{"bad image", cfg, "fake", "-rm", imagePullSystem, `invalid image reference "-rm"`},
		{"bad namespace", cfg, "fake", prepullImage, "k8s.io", `unknown image namespace "k8s.io" (system or cri)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newPullRecorder()
			StartImagePull(tt.cfg, tt.context, "", tt.image, tt.namespace, r)

			if msg := r.wait(t); msg != tt.want {
				t.Errorf("OnDone = %q, want %q", msg, tt.want)
			}
		})
	}
}
