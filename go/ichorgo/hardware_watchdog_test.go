package ichorgo

import (
	"context"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func watchdogStatus(device string, timeout time.Duration) *runtime.WatchdogTimerStatus {
	w := runtime.NewWatchdogTimerStatus(runtime.WatchdogTimerConfigID)
	*w.TypedSpec() = runtime.WatchdogTimerStatusSpec{Device: device, Timeout: timeout, FeedInterval: timeout / 3}

	return w
}

func TestNodeHardwareWatchdogFake(t *testing.T) {
	const armed, none = "192.0.2.61", "192.0.2.62"

	f := newFakeTalos()
	f.addNode(t, armed, "v1.11.0", machine.TypeWorker)
	f.addNode(t, none, "v1.11.0", machine.TypeWorker)
	f.put(armed, watchdogStatus("/dev/watchdog0", 2*time.Minute))

	cfg := f.start(t, armed, none)

	cases := []struct {
		node string
		want *watchdogInfo
	}{
		{node: armed, want: &watchdogInfo{Device: "/dev/watchdog0", TimeoutSec: 120, FeedIntervalSec: 40}},
		// Nothing configured: no section error, just no watchdog.
		{node: none},
	}

	for _, c := range cases {
		out, err := NodeHardware(cfg, "fake", c.node)
		got := decodeJSON[nodeHardware](t, out, err)

		if got.Errors["watchdog"] != "" {
			t.Errorf("%s: watchdog error %q", c.node, got.Errors["watchdog"])
		}

		if (got.Watchdog == nil) != (c.want == nil) || got.Watchdog != nil && *got.Watchdog != *c.want {
			t.Errorf("%s: watchdog %+v, want %+v", c.node, got.Watchdog, c.want)
		}
	}
}

// unknownTypeState answers every List like a Talos that does not know the type.
type unknownTypeState struct{ state.CoreStateReader }

func (unknownTypeState) List(context.Context, resource.Kind, ...state.ListOption) (resource.List, error) {
	return resource.List{}, status.Error(codes.Unimplemented, "unknown resource type")
}

func TestReadWatchdogFailureIsAnError(t *testing.T) {
	if wd, err := readWatchdog(context.Background(), unknownTypeState{}); err == nil || wd != nil {
		t.Errorf("got %+v, %v", wd, err)
	}
}

func TestWatchdogSetting(t *testing.T) {
	if got := watchdogSetting(nil); got != "off" {
		t.Errorf("none: %q", got)
	}

	if got := watchdogSetting(&watchdogInfo{Device: "/dev/watchdog0", TimeoutSec: 120}); got != "/dev/watchdog0 2m0s" {
		t.Errorf("armed: %q", got)
	}

	if got := mapWatchdog(nil); got != nil {
		t.Errorf("empty list: %+v", got)
	}
}
