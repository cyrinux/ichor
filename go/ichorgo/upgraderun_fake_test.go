package ichorgo

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// upgradeRecorder is an UpgradeListener that keeps everything it is told.
type upgradeRecorder struct {
	mu       sync.Mutex
	progress []upgradeProgress
	events   chan upgradeProgress
	done     chan [2]string // new version, error
}

func newUpgradeRecorder() *upgradeRecorder {
	return &upgradeRecorder{events: make(chan upgradeProgress, 64), done: make(chan [2]string, 1)}
}

func (r *upgradeRecorder) OnProgress(s string) {
	var p upgradeProgress
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	r.mu.Lock()
	r.progress = append(r.progress, p)
	r.mu.Unlock()

	r.events <- p
}

func (r *upgradeRecorder) OnDone(version, errMessage string) {
	r.done <- [2]string{version, errMessage}
}

func (r *upgradeRecorder) wait(t *testing.T) [2]string {
	t.Helper()

	select {
	case d := <-r.done:
		return d
	case <-time.After(30 * time.Second):
		t.Fatal("OnDone not called")

		return [2]string{}
	}
}

func (r *upgradeRecorder) phases() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.progress))
	for _, p := range r.progress {
		if len(out) == 0 || out[len(out)-1] != p.Phase {
			out = append(out, p.Phase)
		}
	}

	return strings.Join(out, ",")
}

const (
	upgradeNode  = "192.0.2.41"
	upgradeImage = "ghcr.io/siderolabs/installer:v1.11.1"
)

// upgradeCluster is a worker running v1.11.0 next to the control plane serving the endpoint.
func upgradeCluster(t *testing.T) *fakeTalos {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, upgradeNode, "v1.11.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.42", "v1.11.0", machine.TypeWorker)

	return f
}

// lifecycleInstall answers LifecycleService.Upgrade with messages then the exit code, and
// boots the new version when it succeeded.
func lifecycleInstall(f *fakeTalos, code int32, messages ...string) func(grpc.ServerStreamingServer[machineapi.LifecycleServiceUpgradeResponse]) error {
	return func(stream grpc.ServerStreamingServer[machineapi.LifecycleServiceUpgradeResponse]) error {
		for _, m := range messages {
			if err := stream.Send(installProgress(m, 0, false)); err != nil {
				return err
			}
		}

		if code == 0 {
			f.setVersion(upgradeNode, "v1.11.1")
		}

		return stream.Send(installProgress("", code, true))
	}
}

func TestStartUpgradeFakeLegacy(t *testing.T) {
	f := upgradeCluster(t)

	var got *machineapi.UpgradeRequest

	f.upgrade = func(node string, req *machineapi.UpgradeRequest) error {
		got = req
		f.setVersion(node, "v1.11.1")

		return nil
	}

	r := newUpgradeRecorder()
	StartUpgrade(f.start(t, upgradeNode, "192.0.2.42"), "fake", "", upgradeNode, " "+upgradeImage+" ", false, true, true, r)

	if d := r.wait(t); d != [2]string{"v1.11.1", ""} {
		t.Fatalf("OnDone = %q", d)
	}

	if got.GetImage() != upgradeImage || got.GetStage() || !got.GetForce() {
		t.Errorf("upgrade request = %v", got)
	}

	if p := r.phases(); p != "requested,installing,done" {
		t.Errorf("phases = %s", p)
	}

	if len(f.called("LifecycleUpgrade")) != 0 || len(f.called("Reboot")) != 0 {
		t.Error("the legacy API reboots on its own: no lifecycle call expected")
	}
}

func TestStartUpgradeFakeLifecycle(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(f *fakeTalos)
		stage     bool
		wantErr   string
		wantCalls []string // methods that must have been called on the node
	}{
		{
			name:      "installs then reboots",
			setup:     func(f *fakeTalos) { f.lifecycleUpgrade = lifecycleInstall(f, 0, "pulling", "  ", "installing") },
			wantCalls: []string{"Pull", "LifecycleUpgrade", "Reboot"},
		},
		{
			name:    "no staged upgrade",
			stage:   true,
			wantErr: "no staged upgrade",
		},
		{
			name: "pull fails",
			setup: func(f *fakeTalos) {
				f.pull = func(grpc.ServerStreamingServer[machineapi.ImageServicePullResponse]) error {
					return status.Error(codes.NotFound, "image not found")
				}
			},
			wantErr: "pulling the installer image failed",
		},
		{
			name:      "installer fails",
			setup:     func(f *fakeTalos) { f.lifecycleUpgrade = lifecycleInstall(f, 2, "disk full") },
			wantErr:   "the installer failed (exit code 2): disk full",
			wantCalls: []string{"LifecycleUpgrade"},
		},
		{
			name: "stream ends without a result",
			setup: func(f *fakeTalos) {
				f.lifecycleUpgrade = func(grpc.ServerStreamingServer[machineapi.LifecycleServiceUpgradeResponse]) error { return nil }
			},
			wantErr: "upgrade failed: the node closed the upgrade stream without a result",
		},
		{
			name: "reboot refused",
			setup: func(f *fakeTalos) {
				f.lifecycleUpgrade = lifecycleInstall(f, 0)
				f.rebootErr = status.Error(codes.PermissionDenied, "not authorized")
			},
			wantErr: "the reboot request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := upgradeCluster(t) // f.upgrade nil: the node answers Unimplemented, like Talos 1.18
			if tt.setup != nil {
				tt.setup(f)
			}

			r := newUpgradeRecorder()
			StartUpgrade(f.start(t, upgradeNode, "192.0.2.42"), "fake", "", upgradeNode, upgradeImage, tt.stage, false, true, r)

			d := r.wait(t)
			if tt.wantErr == "" && d != [2]string{"v1.11.1", ""} || !strings.Contains(d[1], tt.wantErr) {
				t.Fatalf("OnDone = %q, want error %q", d, tt.wantErr)
			}

			for _, m := range tt.wantCalls {
				if len(f.called(m+" "+upgradeNode)) != 1 {
					t.Errorf("%s not called once on the node: %v", m, f.called(m))
				}
			}
		})
	}
}

func TestStartUpgradeFakeRefusals(t *testing.T) {
	tests := []struct {
		name         string
		node, image  string
		acknowledged bool
		setup        func(f *fakeTalos)
		wantErr      string
	}{
		{name: "invalid image", node: upgradeNode, image: "nope", acknowledged: true, wantErr: "invalid installer image"},
		{name: "node outside the context", node: "192.0.2.49", image: upgradeImage, acknowledged: true, wantErr: "not part of this context"},
		{
			name: "node busy", node: upgradeNode, image: upgradeImage, acknowledged: true,
			setup:   func(f *fakeTalos) { f.setStage(upgradeNode, runtime.MachineStageUpgrading, false) },
			wantErr: "already in progress",
		},
		{
			name: "risk not acknowledged", node: upgradeNode, image: "ghcr.io/siderolabs/installer:v1.13.0",
			wantErr: "refused until confirmed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := upgradeCluster(t)
			if tt.setup != nil {
				tt.setup(f)
			}

			r := newUpgradeRecorder()
			StartUpgrade(f.start(t, upgradeNode, "192.0.2.42"), "fake", "", tt.node, tt.image, false, false, tt.acknowledged, r)

			if d := r.wait(t); d[1] == "" || !strings.Contains(d[1], tt.wantErr) {
				t.Fatalf("OnDone = %q, want %q", d, tt.wantErr)
			}

			if len(f.called("Upgrade")) != 0 || len(f.called("Pull")) != 0 {
				t.Error("a refused upgrade reached the node")
			}
		})
	}
}

// Cancel stops following, never the upgrade the node already started.
func TestStartUpgradeFakeCancel(t *testing.T) {
	f := upgradeCluster(t)
	f.upgrade = func(string, *machineapi.UpgradeRequest) error { return nil } // still installing

	r := newUpgradeRecorder()
	run := StartUpgrade(f.start(t, upgradeNode, "192.0.2.42"), "fake", "", upgradeNode, upgradeImage, false, false, true, r)

	for p := range r.events {
		if p.Phase == phaseInstall && p.Message == "draining and installing" {
			break
		}
	}

	run.Cancel()

	if d := r.wait(t); d[1] != errStoppedFollowing.Error() {
		t.Fatalf("OnDone = %q", d)
	}
}

func TestObserveNodeFake(t *testing.T) {
	f := upgradeCluster(t)
	f.setStage(upgradeNode, runtime.MachineStageRebooting, false)

	cfg := f.start(t, upgradeNode)

	s, release, err := acquireSession(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	got := observeNode(t.Context(), s.client, upgradeNode)
	if want := (upgradeObservation{reachable: true, version: "v1.11.0", stage: runtime.MachineStageRebooting.String()}); got != want {
		t.Errorf("observation = %+v, want %+v", got, want)
	}

	if got := observeNode(t.Context(), s.client, "192.0.2.49"); got.reachable {
		t.Errorf("a node that does not answer is unreachable: %+v", got)
	}
}
