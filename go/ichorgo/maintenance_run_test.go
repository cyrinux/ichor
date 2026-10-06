package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
)

func TestErrText(t *testing.T) {
	if got := errText(nil); got != "" {
		t.Errorf("nil is no message, got %q", got)
	}

	if got := errText(errors.New("boom")); got != "boom" {
		t.Errorf("got %q", got)
	}
}

func TestMetadataAccessors(t *testing.T) {
	// A single-node answer carries no metadata at all.
	if metaError(nil) != "" || metaHost(nil) != "" {
		t.Error("absent metadata must read as no error and no host")
	}

	//lint:ignore SA1019 the deprecated fields are what the accessors read
	md := &common.Metadata{Hostname: "10.0.0.7", Error: "rpc error: unavailable"}
	if metaHost(md) != "10.0.0.7" || metaError(md) != "rpc error: unavailable" {
		t.Errorf("got %q, %q", metaHost(md), metaError(md))
	}
}

func TestStoppedCordonedKeepsTheCause(t *testing.T) {
	cause := errors.New("eviction refused")
	err := stoppedCordoned("worker-1", cause)

	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "worker-1 stays cordoned") {
		t.Errorf("got %v", err)
	}
}

type progressRecorder struct {
	MaintenanceListener

	got []string
}

func (p *progressRecorder) OnProgress(json string) { p.got = append(p.got, json) }

func TestMaintenanceEmit(t *testing.T) {
	rec := &progressRecorder{}
	maintenance{listener: rec}.emit(phaseDrain, "evicting 1 pods", []drainPod{{Namespace: "shop", Name: "web-1", State: podGone}})

	if len(rec.got) != 1 {
		t.Fatalf("got %d events", len(rec.got))
	}

	var ev maintenanceProgress
	if err := json.Unmarshal([]byte(rec.got[0]), &ev); err != nil {
		t.Fatal(err)
	}

	if ev.Phase != phaseDrain || ev.Message != "evicting 1 pods" || ev.At == 0 || len(ev.Pods) != 1 || ev.Pods[0].State != podGone {
		t.Errorf("got %+v", ev)
	}
}

// A run is refused before anything is dialled: an unknown action, and the demo cluster.
func TestMaintenanceRunRefusals(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	bad := maintenance{kube: kubeTarget{config: cfg}, node: "192.0.2.10", action: "format"}
	if err := bad.run(context.Background()); err == nil || !strings.Contains(err.Error(), `unknown maintenance action "format"`) {
		t.Errorf("got %v", err)
	}

	demo := maintenance{kube: kubeTarget{config: cfg}, node: "192.0.2.10", action: maintenanceReboot}
	if err := demo.run(context.Background()); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("got %v", err)
	}
}
