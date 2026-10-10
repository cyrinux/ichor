package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// stepRecorder replaces the upgrade steps and records which ran, in order.
type stepRecorder struct {
	ran        []string
	recheckErr error
	requestErr error
}

func (r *stepRecorder) steps() upgradeSteps {
	return upgradeSteps{
		recheck: func(context.Context) error {
			r.ran = append(r.ran, "recheck")

			return r.recheckErr
		},
		request: func(_ context.Context, emit func(phase, msg string)) error {
			r.ran = append(r.ran, "request")
			emit(phaseInstall, "installing")

			return r.requestErr
		},
		follow: func(_ context.Context, emit func(phase, msg string)) error {
			r.ran = append(r.ran, "follow")
			emit(phaseDone, "running v1.18.1")

			return nil
		},
		back: func(_ context.Context, emit func(string)) error {
			r.ran = append(r.ran, "back")
			emit("the node is back and Ready")

			return nil
		},
	}
}

func phasesOf(t *testing.T, events []string) []string {
	t.Helper()

	var out []string

	for _, e := range events {
		var p maintenanceProgress
		if err := json.Unmarshal([]byte(e), &p); err != nil {
			t.Fatal(err)
		}

		out = append(out, p.Phase+" "+p.Message)
	}

	return out
}

func TestDrainThenUpgrade(t *testing.T) {
	api, k := newDrainAPI(t, 0)
	rec, steps := &progressRecorder{}, &stepRecorder{}
	m := maintenance{action: maintenanceUpgrade, listener: rec}

	if err := m.drainThenUpgrade(context.Background(), k, maintenancePlan{KubeNode: "w1", Hostname: "w1"}, steps.steps()); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(steps.ran, []string{"recheck", "request", "follow", "back"}) {
		t.Errorf("steps = %v", steps.ran)
	}

	// Cordoned, drained (the two evictable pods), then uncordoned.
	if len(api.patches) != 2 || !strings.Contains(api.patches[0], `"unschedulable":true`) || !strings.Contains(api.patches[1], `"unschedulable":false`) {
		t.Errorf("patches = %v", api.patches)
	}

	if len(api.evictions) != 2 {
		t.Errorf("evictions = %v", api.evictions)
	}

	got := phasesOf(t, rec.got)
	for _, want := range []string{"upgrade installing: installing", "upgrade done: running v1.18.1", "waiting the node is back and Ready", "uncordon uncordoning w1"} {
		if !slices.Contains(got, want) {
			t.Errorf("no %q in %v", want, got)
		}
	}
}

func TestDrainThenUpgradeStopsOnAFailedDrain(t *testing.T) {
	api, k := newDrainAPI(t, 0)
	api.forbid = true
	steps := &stepRecorder{}
	m := maintenance{action: maintenanceUpgrade, listener: &progressRecorder{}}

	err := m.drainThenUpgrade(context.Background(), k, maintenancePlan{KubeNode: "w1"}, steps.steps())
	if err == nil || !strings.Contains(err.Error(), "w1 stays cordoned") || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("err = %v", err)
	}

	if len(steps.ran) != 0 {
		t.Errorf("the upgrade went on after a failed drain: %v", steps.ran)
	}

	if len(api.patches) != 1 {
		t.Errorf("the node must stay cordoned: %v", api.patches)
	}
}

func TestDrainThenUpgradeStopsOnAFailedRecheck(t *testing.T) {
	api, k := newDrainAPI(t, 0)
	steps := &stepRecorder{recheckErr: errors.New("upgrade refused: etcd would lose quorum")}
	m := maintenance{action: maintenanceUpgrade, listener: &progressRecorder{}}

	err := m.drainThenUpgrade(context.Background(), k, maintenancePlan{KubeNode: "w1"}, steps.steps())
	if err == nil || !strings.Contains(err.Error(), "lose quorum") || !strings.Contains(err.Error(), "stays cordoned") {
		t.Fatalf("err = %v", err)
	}

	if !slices.Equal(steps.ran, []string{"recheck"}) || len(api.patches) != 1 {
		t.Errorf("steps = %v, patches = %v", steps.ran, api.patches)
	}
}

func TestDrainThenUpgradeKeepsANodeCordonedBefore(t *testing.T) {
	api, k := newDrainAPI(t, 0)
	m := maintenance{action: maintenanceUpgrade, listener: &progressRecorder{}}

	if err := m.drainThenUpgrade(context.Background(), k, maintenancePlan{KubeNode: "w1", Cordoned: true}, (&stepRecorder{}).steps()); err != nil {
		t.Fatal(err)
	}

	if len(api.patches) != 1 {
		t.Errorf("a node cordoned before the run must stay so: %v", api.patches)
	}
}

func TestMaintenanceUpgradeChecks(t *testing.T) {
	plan := maintenancePlan{upgrade: upgradePlan{CurrentVersion: "v1.18.0", Blockers: []string{}, Acknowledge: []string{}}}

	if err := (maintenance{image: "not an image"}).upgradeChecks(plan); err == nil || !strings.Contains(err.Error(), "invalid installer image") {
		t.Errorf("bad image = %v", err)
	}

	if err := (maintenance{image: "ghcr.io/siderolabs/installer:v1.18.1"}).upgradeChecks(plan); err != nil {
		t.Errorf("good image = %v", err)
	}

	blocked := plan
	blocked.upgrade.Blockers = []string{"node is booting, not running"}

	if err := (maintenance{image: "ghcr.io/siderolabs/installer:v1.18.1"}).upgradeChecks(blocked); err == nil || !strings.Contains(err.Error(), "booting") {
		t.Errorf("blocked = %v", err)
	}
}

func TestMaintenanceLockRequest(t *testing.T) {
	plan := maintenancePlan{Hostname: "w1", upgrade: upgradePlan{CurrentVersion: "v1.18.0"}}

	up, _ := (maintenance{action: maintenanceUpgrade, node: "192.0.2.5", image: "ghcr.io/siderolabs/installer:v1.18.1"}).lockRequest(plan)
	if up.from != "v1.18.0" || up.to != "v1.18.1" || up.node != "192.0.2.5" || up.hostname != "w1" {
		t.Errorf("upgrade lock = %+v", up)
	}

	reboot, takeOver := (maintenance{action: maintenanceReboot}).lockRequest(plan)
	if reboot.to != maintenanceLockTo || reboot.from != "" || takeOver(&upgradeLockInfo{}) {
		t.Errorf("reboot lock = %+v", reboot)
	}
}

func TestUpgradeSkipsDrain(t *testing.T) {
	for version, want := range map[string]bool{"": false, "v1.13.2": false, "v1.17.9": false, "v1.18.0": true, "v1.19.0-alpha.1": true} {
		if got := upgradeSkipsDrain(version); got != want {
			t.Errorf("%q: %t, want %t", version, got, want)
		}
	}
}

// doneRecorder waits for OnDone.
type doneRecorder struct {
	done chan string
}

func (d doneRecorder) OnProgress(string)        {}
func (d doneRecorder) OnDone(errMessage string) { d.done <- errMessage }

func TestStartNodeMaintenanceUpgradeDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := doneRecorder{done: make(chan string, 1)}
	StartNodeMaintenanceUpgrade(cfg, "Demo cluster", "", "192.0.2.20", "ghcr.io/siderolabs/installer:v1.18.1", false, false, false, rec)

	select {
	case msg := <-rec.done:
		if msg != errDemoUnavailable.Error() {
			t.Fatalf("demo = %q", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("OnDone not called")
	}

	entries := readAudit(t, "Demo cluster", "maintenance-upgrade")
	if len(entries) != 1 || entries[0].Params != "image=ghcr.io/siderolabs/installer:v1.18.1 include-bare=false force=false" || entries[0].Outcome == auditOK {
		t.Errorf("audit = %+v", entries)
	}
}
