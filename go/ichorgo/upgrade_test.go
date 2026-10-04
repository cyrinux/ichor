package ichorgo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const schematic = "376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba"

func TestUpgradeImage(t *testing.T) {
	for _, tc := range []struct{ current, version, want string }{
		{"ghcr.io/siderolabs/installer:v1.13.4", "v1.14.1", "ghcr.io/siderolabs/installer:v1.14.1"},
		{"ghcr.io/siderolabs/installer:v1.13.4", "1.14.1", "ghcr.io/siderolabs/installer:v1.14.1"},
		{"factory.talos.dev/installer/" + schematic + ":v1.13.0", "1.14.0-beta.1", "factory.talos.dev/installer/" + schematic + ":v1.14.0-beta.1"},
		{"factory.talos.dev/metal-installer-secureboot/" + schematic + ":v1.13.0", "v1.14.1", "factory.talos.dev/metal-installer-secureboot/" + schematic + ":v1.14.1"},
		{"registry.local:5000/siderolabs/installer:v1.13.0@sha256:" + strings.Repeat("a", 64), "v1.14.1", "registry.local:5000/siderolabs/installer:v1.14.1"},
		{"registry.local:5000/siderolabs/installer@sha256:" + strings.Repeat("b", 64), "v1.14.1", "registry.local:5000/siderolabs/installer:v1.14.1"},
		{"", "v1.14.1", "ghcr.io/siderolabs/installer:v1.14.1"},
		{"ghcr.io/siderolabs/installer:v1.13.4", "latest", ""},
		{"ghcr.io/siderolabs/installer:v1.13.4", "v1.14", ""},
		{"ghcr.io/siderolabs/installer:v1.13.4", "v1.14.1; rm -rf", ""},
	} {
		if got := UpgradeImage(tc.current, tc.version); got != tc.want {
			t.Errorf("UpgradeImage(%q, %q) = %q, want %q", tc.current, tc.version, got, tc.want)
		}
	}
}

func TestImageSchematic(t *testing.T) {
	if got := imageSchematic("factory.talos.dev/installer/" + schematic + ":v1.14.1"); got != schematic {
		t.Fatalf("schematic %q", got)
	}

	for _, img := range []string{"ghcr.io/siderolabs/installer:v1.14.1", "factory.talos.dev/installer:v1.14.1", "evil.example/installer/" + schematic + ":v1"} {
		if got := imageSchematic(img); got != "" {
			t.Errorf("%q: schematic %q", img, got)
		}
	}
}

func running(node, version string, cp bool) planPeer {
	return planPeer{node: node, hostname: "host-" + node, reachable: true, stage: "running", ready: true, version: version, controlPlane: cp}
}

func TestComputePlanQuorum(t *testing.T) {
	cp := func(members, healthy int, thisHealthy bool) upgradePlan {
		return computePlan(planInput{
			target:       running("a", "v1.14.0", true),
			others:       []planPeer{running("b", "v1.14.0", true), running("c", "v1.14.0", true)},
			image:        "ghcr.io/siderolabs/installer:v1.14.0",
			etcd:         &etcdHealth{members: members, healthy: healthy, thisMember: true, thisHealthy: thisHealthy},
			defaultImage: "ghcr.io/siderolabs/installer:v1.14.0",
		})
	}

	ok := cp(3, 3, true)
	if len(ok.Blockers) != 0 || !ok.Etcd.QuorumAfterLoss || ok.Etcd.Members != 3 || ok.Forceable {
		t.Fatalf("healthy 3 members: %+v", ok)
	}

	// 3 members, another one unhealthy: taking this one down leaves 1 of 3.
	lost := cp(3, 2, true)
	if lost.Etcd.QuorumAfterLoss || len(lost.Blockers) != 2 || !lost.Forceable {
		t.Fatalf("3 members/1 unhealthy: %+v", lost)
	}

	// 3 members, this one unhealthy: quorum holds, but Talos still wants all healthy.
	self := cp(3, 2, false)
	if !self.Etcd.QuorumAfterLoss || len(self.Blockers) != 1 || !strings.Contains(self.Blockers[0], "unhealthy") {
		t.Fatalf("3 members/self unhealthy: %+v", self)
	}

	// 5 members, 1 other unhealthy: quorum holds after loss (3 of 5).
	if five := cp(5, 4, true); !five.Etcd.QuorumAfterLoss {
		t.Fatalf("5 members: %+v", five)
	}

	two := cp(2, 2, true)
	if two.Etcd.QuorumAfterLoss || len(two.Blockers) != 1 || !strings.Contains(two.Blockers[0], "quorum") {
		t.Fatalf("2 members: %+v", two)
	}

	single := cp(1, 1, true)
	if len(single.Blockers) != 0 || single.Etcd.QuorumAfterLoss || !containsText(single.Acknowledge, "single control plane") || containsText(single.Warnings, "single control plane") {
		t.Fatalf("single member: %+v", single)
	}
}

func containsText(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
}

func TestComputePlanNodes(t *testing.T) {
	unreachable := computePlan(planInput{target: planPeer{node: "a", hostname: "a", err: "unreachable: connection refused"}})
	if len(unreachable.Blockers) != 1 || !strings.Contains(unreachable.Blockers[0], "unreachable") || unreachable.Forceable {
		t.Fatalf("unreachable: %+v", unreachable)
	}

	booting := running("b", "v1.14.0", false)
	booting.stage = "booting"

	down := planPeer{node: "c", hostname: "c", err: "timed out"}
	notReady := running("d", "v1.13.4", false)
	notReady.ready = false

	plan := computePlan(planInput{
		target:       running("a", "v1.14.0", false),
		others:       []planPeer{booting, down, notReady},
		imageErr:     "PermissionDenied",
		defaultImage: "ghcr.io/siderolabs/installer:v1.14.0",
	})

	if len(plan.Blockers) != 1 || !strings.Contains(plan.Blockers[0], "host-b is booting") || plan.Forceable {
		t.Fatalf("blockers %+v", plan.Blockers)
	}

	for _, w := range []string{"c is unreachable", "host-d is not ready", "v1.13.4, v1.14.0", "installer image"} {
		if !containsText(plan.Warnings, w) {
			t.Errorf("missing warning %q in %v", w, plan.Warnings)
		}
	}

	if plan.CurrentImage != "ghcr.io/siderolabs/installer:v1.14.0" || plan.Etcd != nil {
		t.Fatalf("plan %+v", plan)
	}

	upgrading := running("a", "v1.14.0", false)
	upgrading.stage = "upgrading"

	if p := computePlan(planInput{target: upgrading}); !containsText(p.Blockers, "already in progress") {
		t.Fatalf("upgrading target: %+v", p.Blockers)
	}

	// A control plane whose etcd cannot be checked is blocked, but force can skip it.
	noEtcd := computePlan(planInput{target: running("a", "v1.14.0", true), etcd: &etcdHealth{err: "timed out"}})
	if !noEtcd.Forceable || upgradeRefusal(noEtcd, true) != nil || upgradeRefusal(noEtcd, false) == nil {
		t.Fatalf("etcd unknown: %+v", noEtcd)
	}

	// Force never skips the other blockers.
	mixed := computePlan(planInput{target: running("a", "v1.14.0", true), others: []planPeer{booting}, etcd: &etcdHealth{members: 2, healthy: 2, thisMember: true, thisHealthy: true}})
	if mixed.Forceable || upgradeRefusal(mixed, true) == nil || strings.Contains(upgradeRefusal(mixed, true).Error(), "quorum") {
		t.Fatalf("mixed: %+v / %v", mixed, upgradeRefusal(mixed, true))
	}
}

func TestComputePlanSchematic(t *testing.T) {
	p := computePlan(planInput{target: running("a", "v1.14.0", false), image: "factory.talos.dev/installer/" + schematic + ":v1.14.0"})
	if p.Schematic != schematic || len(p.Warnings) != 0 {
		t.Fatalf("plan %+v", p)
	}
}

type fakeNode struct {
	steps []upgradeObservation
	i     int
}

func (f *fakeNode) observe(context.Context) upgradeObservation {
	o := f.steps[min(f.i, len(f.steps)-1)]
	f.i++

	return o
}

func obs(version, stage string, ready bool) upgradeObservation {
	return upgradeObservation{reachable: true, version: version, stage: stage, ready: ready}
}

var down = upgradeObservation{}

func follow(t *testing.T, steps []upgradeObservation, tick time.Duration, timeout time.Duration) ([]string, string, error) {
	t.Helper()

	return followWith(t, &upgradeTracker{oldVersion: "v1.13.4"}, steps, tick, timeout)
}

func followWith(
	t *testing.T, tracker *upgradeTracker, steps []upgradeObservation, tick time.Duration, timeout time.Duration,
) ([]string, string, error) {
	t.Helper()

	f := &fakeNode{steps: steps}
	clock := time.Unix(0, 0)

	var phases []string

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	v, err := followUpgrade(ctx, tracker, f.observe,
		func(phase, _ string) { phases = append(phases, phase) },
		time.Microsecond, func() time.Time { clock = clock.Add(tick); return clock })

	return phases, v, err
}

func TestFollowUpgradeSuccess(t *testing.T) {
	phases, v, err := follow(t, []upgradeObservation{
		obs("v1.13.4", "running", true),
		obs("v1.13.4", "upgrading", false),
		obs("v1.13.4", "rebooting", false),
		down, down,
		obs("v1.14.1", "booting", false),
		obs("v1.14.1", "running", false),
		obs("v1.14.1", "running", true),
	}, time.Second, 5*time.Second)
	if err != nil || v != "v1.14.1" {
		t.Fatalf("v=%q err=%v", v, err)
	}

	want := "installing,rebooting,waiting for node,booted,done"
	if got := strings.Join(phases, ","); got != want {
		t.Fatalf("phases %s, want %s", got, want)
	}
}

func TestFollowUpgradeStagedRebootsTwice(t *testing.T) {
	// Staged: reboot, back on the old version briefly (applying), reboot again.
	phases, v, err := follow(t, []upgradeObservation{
		obs("v1.13.4", "rebooting", false),
		down,
		obs("v1.13.4", "running", true),
		obs("v1.13.4", "upgrading", false),
		down,
		obs("v1.14.1", "running", true),
	}, time.Second, 5*time.Second)
	if err != nil || v != "v1.14.1" || phases[len(phases)-1] != "done" {
		t.Fatalf("phases=%v v=%q err=%v", phases, v, err)
	}
}

func TestFollowUpgradeRolledBack(t *testing.T) {
	_, v, err := follow(t, []upgradeObservation{
		down,
		obs("v1.13.4", "running", true),
	}, time.Minute, 5*time.Second)
	if err == nil || v != "" || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("v=%q err=%v", v, err)
	}
}

func TestFollowUpgradeReinstall(t *testing.T) {
	// Same version (e.g. another schematic): ready again after the reboot is the success.
	phases, v, err := followWith(t, &upgradeTracker{oldVersion: "v1.13.4", reinstall: true}, []upgradeObservation{
		obs("v1.13.4", "running", true),
		obs("v1.13.4", "upgrading", false),
		down,
		obs("v1.13.4", "booting", false),
		obs("v1.13.4", "running", false),
		obs("v1.13.4", "running", true),
	}, time.Minute, 5*time.Second)
	if err != nil || v != "v1.13.4" {
		t.Fatalf("v=%q err=%v", v, err)
	}

	want := "installing,rebooting,waiting for node,booted,done"
	if got := strings.Join(phases, ","); got != want {
		t.Fatalf("phases %s, want %s", got, want)
	}
}

func TestFollowUpgradeStagedReinstall(t *testing.T) {
	staged := func() *upgradeTracker { return &upgradeTracker{oldVersion: "v1.13.4", reinstall: true, staged: true} }

	// The first boot back only applies the staged image: done after the second reboot.
	f := &fakeNode{steps: []upgradeObservation{
		down,
		obs("v1.13.4", "running", true),
		obs("v1.13.4", "running", true),
		obs("v1.13.4", "upgrading", false),
		down,
		obs("v1.13.4", "running", true),
	}}

	clock := time.Unix(0, 0)

	v, err := followUpgrade(context.Background(), staged(), f.observe, func(string, string) {},
		time.Microsecond, func() time.Time { clock = clock.Add(time.Second); return clock })
	if err != nil || v != "v1.13.4" || f.i != len(f.steps) {
		t.Fatalf("v=%q err=%v after %d observations", v, err, f.i)
	}

	// Both reboots seen as one: done once the node stayed up for the grace period.
	_, v, err = followWith(t, staged(), []upgradeObservation{down, obs("v1.13.4", "running", true)}, time.Minute, 5*time.Second)
	if err != nil || v != "v1.13.4" {
		t.Fatalf("one reboot seen: v=%q err=%v", v, err)
	}
}

func TestFollowUpgradeCancelAndTimeout(t *testing.T) {
	f := &fakeNode{steps: []upgradeObservation{obs("v1.13.4", "upgrading", false)}}

	ctx, cancel := context.WithCancel(context.Background())

	_, err := followUpgrade(ctx, &upgradeTracker{oldVersion: "v1.13.4"}, f.observe,
		func(string, string) { cancel() }, time.Millisecond, time.Now)
	if !errors.Is(err, errStoppedFollowing) {
		t.Fatalf("cancel: %v", err)
	}

	_, _, err = follow(t, []upgradeObservation{obs("v1.13.4", "upgrading", false)}, time.Second, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not come back") {
		t.Fatalf("timeout: %v", err)
	}

	_, v, err := follow(t, []upgradeObservation{down, obs("v1.14.1", "running", false)}, time.Second, 20*time.Millisecond)
	if v != "v1.14.1" || err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("booted not ready: v=%q err=%v", v, err)
	}
}

func TestParseReleases(t *testing.T) {
	body := `[
		{"tag_name":"v1.13.4","draft":false,"prerelease":false,"published_at":"2026-08-01T10:00:00Z"},
		{"tag_name":"v1.14.0-beta.1","draft":false,"prerelease":true,"published_at":"2026-08-20T10:00:00Z"},
		{"tag_name":"v1.14.1","draft":false,"prerelease":false,"published_at":"2026-09-15T10:00:00Z"},
		{"tag_name":"v1.15.0-alpha.0","draft":true,"prerelease":true,"published_at":"2026-09-20T10:00:00Z"},
		{"tag_name":"pkg/machinery/v1.14.1","draft":false,"prerelease":false,"published_at":"2026-09-16T10:00:00Z"}
	]`

	got, err := parseReleases([]byte(body))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 || got[0].Version != "v1.14.1" || got[1].Version != "v1.14.0-beta.1" || !got[1].Prerelease || got[2].Date != "2026-08-01T10:00:00Z" {
		t.Fatalf("releases %+v", got)
	}

	if _, err := parseReleases([]byte("{")); err == nil {
		t.Fatal("bad JSON accepted")
	}
}
