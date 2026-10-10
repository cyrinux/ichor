package ichorgo

import (
	"slices"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

// oneControlPlaneAndWorker is a control plane serving etcd alone and a worker with a data
// disk next to its system disk.
func oneControlPlaneAndWorker(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, "192.0.2.61", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.62", "v1.11.0", machine.TypeWorker)

	system := block.NewDisk(block.NamespaceName, "sda")
	system.TypedSpec().Size, system.TypedSpec().DevPath = 120<<30, "/dev/sda"

	data := block.NewDisk(block.NamespaceName, "sdb")
	data.TypedSpec().Size, data.TypedSpec().DevPath = 500<<30, "/dev/sdb"

	sd := block.NewSystemDisk(block.NamespaceName, block.SystemDiskID)
	sd.TypedSpec().DiskID, sd.TypedSpec().DevPath = "sda", "/dev/sda"

	f.put("192.0.2.62", system, data, sd)

	return f, f.start(t, "192.0.2.61", "192.0.2.62")
}

func TestNodeResetLastControlPlaneRefused(t *testing.T) {
	f, cfg := oneControlPlaneAndWorker(t)

	out, err := NodeResetPlan(cfg, "fake", "", "192.0.2.61")
	plan := decodeJSON[resetPlan](t, out, err)

	if plan.Role != "controlplane" || !plan.LastControlPlane || len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0], "only control plane") {
		t.Fatalf("plan = %s", out)
	}

	err = NodeReset(cfg, "fake", "192.0.2.61", "system", true, true)
	if err == nil || !strings.Contains(err.Error(), "reset refused: it is the only control plane") {
		t.Fatalf("reset = %v", err)
	}

	if len(f.resets) != 0 {
		t.Errorf("Reset was called: %v", f.resets)
	}
}

func TestNodeResetQuorumLossRefused(t *testing.T) {
	f, cfg := threeControlPlanes(t, false)

	out, err := NodeResetPlan(cfg, "fake", "", "192.0.2.51")
	plan := decodeJSON[resetPlan](t, out, err)

	if plan.LastControlPlane || plan.EtcdMember == nil || plan.EtcdMember.ID != hexID(0xa1) || !plan.EtcdMember.Healthy {
		t.Fatalf("plan = %s", out)
	}

	if len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0], "etcd would lose quorum") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}

	if err := NodeReset(cfg, "fake", "192.0.2.51", "system", true, true); err == nil || !strings.Contains(err.Error(), "lose quorum") {
		t.Fatalf("reset = %v", err)
	}

	if len(f.resets) != 0 {
		t.Errorf("Reset was called: %v", f.resets)
	}
}

func TestNodeResetHealthyControlPlane(t *testing.T) {
	f, cfg := threeControlPlanes(t, true)

	out, err := NodeResetPlan(cfg, "fake", "", "192.0.2.51")
	plan := decodeJSON[resetPlan](t, out, err)

	if len(plan.Blockers) != 0 || plan.EtcdMember == nil || !slices.ContainsFunc(plan.Warnings, func(w string) bool { return strings.Contains(w, "etcd leader") }) {
		t.Fatalf("plan = %s", out)
	}

	if err := NodeReset(cfg, "fake", "192.0.2.51", "system", true, true); err != nil {
		t.Fatal(err)
	}

	if len(f.resets) != 1 || f.resets[0].GetMode() != machineapi.ResetRequest_SYSTEM_DISK || len(f.resets[0].GetUserDisksToWipe()) != 0 {
		t.Fatalf("resets = %v", f.resets)
	}
}

func TestNodeResetWorkerCarriesTheChoice(t *testing.T) {
	withDataDir(t)

	f, cfg := oneControlPlaneAndWorker(t)

	out, err := NodeResetPlan(cfg, "fake", "", "192.0.2.62")
	plan := decodeJSON[resetPlan](t, out, err)

	if plan.Role != "worker" || plan.EtcdMember != nil || len(plan.Blockers) != 0 || !slices.Equal(plan.UserDisks, []string{"/dev/sdb"}) {
		t.Fatalf("plan = %s", out)
	}

	if err := NodeReset(cfg, "fake", "192.0.2.62", "all", true, false); err != nil {
		t.Fatal(err)
	}

	if err := NodeReset(cfg, "fake", "192.0.2.62", "user", false, true); err != nil {
		t.Fatal(err)
	}

	if len(f.resets) != 2 {
		t.Fatalf("resets = %v", f.resets)
	}

	all, user := f.resets[0], f.resets[1]
	if all.GetMode() != machineapi.ResetRequest_ALL || !all.GetGraceful() || all.GetReboot() || !slices.Equal(all.GetUserDisksToWipe(), []string{"/dev/sdb"}) {
		t.Errorf("all = %v", all)
	}

	if user.GetMode() != machineapi.ResetRequest_USER_DISKS || user.GetGraceful() || !user.GetReboot() || !slices.Equal(user.GetUserDisksToWipe(), []string{"/dev/sdb"}) {
		t.Errorf("user = %v", user)
	}

	entries := readAudit(t, "fake", "reset")
	if len(entries) != 2 || entries[0].Node != "192.0.2.62" || entries[0].Params != "wipe=user,graceful=false,reboot=true" || entries[0].Outcome != auditOK {
		t.Errorf("audit = %+v", entries)
	}
}

func TestNodeResetRefusals(t *testing.T) {
	f, cfg := threeControlPlanes(t, true)

	if err := NodeReset(cfg, "fake", "192.0.2.51", "disks", true, true); err == nil || !strings.Contains(err.Error(), "unknown wipe mode") {
		t.Errorf("bad mode = %v", err)
	}

	// No user disk is known on these nodes: there is nothing for a user-disk wipe.
	if err := NodeReset(cfg, "fake", "192.0.2.52", "user", true, true); err == nil || !strings.Contains(err.Error(), "no user disk") {
		t.Errorf("user wipe = %v", err)
	}

	if err := NodeReset(cfg, "fake", "192.0.2.59", "system", true, true); err == nil || !strings.Contains(err.Error(), "not part of this context") {
		t.Errorf("unknown node = %v", err)
	}

	if _, err := NodeResetPlan(cfg, "fake", "", ""); err == nil {
		t.Error("an empty node must be refused")
	}

	if len(f.resets) != 0 {
		t.Errorf("Reset was called: %v", f.resets)
	}
}

func TestComputeResetPlanUnreachable(t *testing.T) {
	plan := computeResetPlan(resetPlanInput{target: planPeer{node: "192.0.2.70", err: "unreachable: connection refused"}})

	if len(plan.Blockers) != 1 || plan.Blockers[0] != "node is unreachable: connection refused" || plan.UserDisks == nil {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestNodeResetPlanDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := NodeResetPlan(demo, "Demo cluster", "", "192.0.2.10")
	plan := decodeJSON[resetPlan](t, out, err)

	if plan.Role != "controlplane" || plan.EtcdMember == nil || plan.EtcdMember.ID != "a1" || len(plan.Blockers) != 0 {
		t.Fatalf("plan = %s", out)
	}

	out, err = NodeResetPlan(demo, "Demo cluster", "", "192.0.2.20")
	if plan = decodeJSON[resetPlan](t, out, err); plan.Role != "worker" || plan.EtcdMember != nil {
		t.Fatalf("worker plan = %s", out)
	}

	if err := NodeReset(demo, "Demo cluster", "192.0.2.20", "system", true, true); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Fatalf("demo reset = %v", err)
	}
}
