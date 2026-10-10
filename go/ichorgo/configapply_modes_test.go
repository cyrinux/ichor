package ichorgo

import (
	"context"
	"errors"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// applyHarness runs runConfigApply against a fake node; waitBack is scripted.
type applyHarness struct {
	node    *fakeConfigNode
	phases  []string
	waited  int
	backErr error
}

func (h *applyHarness) run(ctx context.Context, mode string) error {
	base := h.node.base()

	return runConfigApply(ctx, h.node, configApply{
		base: base, draft: withLabel(h.node.t, base, "applied"), mode: mode,
		emit: func(phase, _ string) { h.phases = append(h.phases, phase) },
		waitBack: func(ctx context.Context) error {
			h.waited++
			if ctx.Err() != nil {
				return errors.New("stopped while waiting for the node")
			}

			return h.backErr
		},
	})
}

func TestConfigApplyModes(t *testing.T) {
	//lint:ignore SA1019 the mode that applies and reboots
	reboot := machineapi.ApplyConfigurationRequest_REBOOT

	for _, tt := range []struct {
		mode   string
		want   machineapi.ApplyConfigurationRequest_Mode
		phases string
		waited int
	}{
		{applyModeAuto, machineapi.ApplyConfigurationRequest_AUTO, "applying,done", 0},
		{applyModeStaged, machineapi.ApplyConfigurationRequest_STAGED, "applying,done", 0},
		{applyModeReboot, reboot, "applying,rebooting,done", 1},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			h := &applyHarness{node: newFakeConfigNode(t)}

			if err := h.run(context.Background(), tt.mode); err != nil {
				t.Fatal(err)
			}

			calls := h.node.calls
			if len(calls) != 2 || !calls[0].dryRun || calls[1].dryRun || calls[1].mode != tt.want || calls[1].tryFor != 0 {
				t.Fatalf("calls = %+v", calls)
			}

			if !strings.Contains(calls[1].data, "applied") {
				t.Error("the edited config must be sent")
			}

			if got := strings.Join(h.phases, ","); got != tt.phases || h.waited != tt.waited {
				t.Errorf("phases = %s, waited %d", got, h.waited)
			}
		})
	}
}

func TestConfigApplyAutoRefusedWhenARebootIsNeeded(t *testing.T) {
	h := &applyHarness{node: newFakeConfigNode(t)}
	h.node.reboot = true

	if err := h.run(context.Background(), applyModeAuto); !errors.Is(err, errConfigApplyNeedsReboot) {
		t.Fatalf("err = %v", err)
	}

	if len(h.node.calls) != 1 || !h.node.calls[0].dryRun {
		t.Errorf("only the dry run may reach the node: %+v", h.node.calls)
	}

	// Staged and reboot are what such a change is for.
	h = &applyHarness{node: newFakeConfigNode(t)}
	h.node.reboot = true

	if err := h.run(context.Background(), applyModeStaged); err != nil {
		t.Fatal(err)
	}
}

func TestConfigApplyRebootWaitFailure(t *testing.T) {
	h := &applyHarness{node: newFakeConfigNode(t), backErr: errors.New("the node is not back within 20m0s")}

	err := h.run(context.Background(), applyModeReboot)
	if err == nil || !strings.Contains(err.Error(), "the config was applied, but the node is not back") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigApplyRebootAnswerLost(t *testing.T) {
	h := &applyHarness{node: newFakeConfigNode(t)}
	h.node.failures = []error{status.Error(codes.Unavailable, "connection reset")}
	h.node.lostAnswer = true

	if err := h.run(context.Background(), applyModeReboot); err != nil || h.waited != 1 {
		t.Fatalf("a rebooting node may drop the answer: %v (waited %d)", err, h.waited)
	}

	// Any other mode reports the error.
	h = &applyHarness{node: newFakeConfigNode(t)}
	h.node.failures = []error{status.Error(codes.Unavailable, "connection reset")}

	if err := h.run(context.Background(), applyModeStaged); err == nil {
		t.Fatal("a lost answer must fail a staged apply")
	}
}

func TestConfigApplyCancelWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h := &applyHarness{node: newFakeConfigNode(t)}

	// Cancelled before anything: the node is not even read. (The fake ignores ctx.)
	err := h.run(ctx, applyModeReboot)
	if err == nil || !strings.Contains(err.Error(), "stopped while waiting") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigApplyRefusals(t *testing.T) {
	h := &applyHarness{node: newFakeConfigNode(t)}
	if err := h.run(context.Background(), "try"); err == nil || !strings.Contains(err.Error(), "unknown apply mode") {
		t.Errorf("mode = %v", err)
	}

	node := newFakeConfigNode(t)
	base := node.base()

	err := runConfigApply(context.Background(), node, configApply{base: base, draft: base, mode: applyModeAuto, emit: func(string, string) {}})
	if !errors.Is(err, errConfigUnchanged) {
		t.Errorf("unchanged = %v", err)
	}

	err = runConfigApply(context.Background(), node, configApply{base: withLabel(t, base, "other"), draft: withLabel(t, base, "x"), mode: applyModeAuto, emit: func(string, string) {}})
	if !errors.Is(err, errConfigChanged) {
		t.Errorf("stale base = %v", err)
	}
}

type applyRecorder struct{ done chan string }

func (r applyRecorder) OnProgress(string)        {}
func (r applyRecorder) OnDone(errMessage string) { r.done <- errMessage }

func TestStartConfigApplyDemo(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	node := demoNodes()[0].Node

	base, err := NodeMachineConfig(demo, "", node, false)
	if err != nil {
		t.Fatal(err)
	}

	rec := applyRecorder{make(chan string, 1)}
	StartConfigApply(demo, "", node, base, withLabel(t, base, "x"), applyModeStaged, rec)

	if got := <-rec.done; got != errDemoUnavailable.Error() {
		t.Fatalf("the demo cannot be changed: %q", got)
	}

	entries := readAudit(t, "", "config-apply")
	if len(entries) != 1 || entries[0].Params != "mode=staged" || entries[0].Outcome == auditOK {
		t.Errorf("audit = %+v", entries)
	}
}
