package ichorgo

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeLeases is an in-memory Lease API with resourceVersion checks.
type fakeLeases struct {
	lease   *kubeLease
	version int
	// raceOnReplace changes the Lease before a replace lands (another app won).
	raceOnReplace bool
	err           error
}

func (f *fakeLeases) store(l kubeLease) *kubeLease {
	f.version++
	l.Metadata.ResourceVersion = strconv.Itoa(f.version)

	if l.Metadata.UID == "" {
		l.Metadata.UID = "uid-" + strconv.Itoa(f.version)
	}

	f.lease = &l
	out := l

	return &out
}

func (f *fakeLeases) get(context.Context) (*kubeLease, error) {
	if f.err != nil {
		return nil, f.err
	}

	if f.lease == nil {
		return nil, nil
	}

	out := *f.lease

	return &out, nil
}

func (f *fakeLeases) create(_ context.Context, l kubeLease) (*kubeLease, error) {
	if f.err != nil {
		return nil, f.err
	}

	if f.lease != nil {
		return nil, errLeaseConflict
	}

	return f.store(l), nil
}

func (f *fakeLeases) replace(_ context.Context, l kubeLease) (*kubeLease, error) {
	if f.raceOnReplace {
		f.store(*f.lease)
	}

	if f.lease == nil || l.Metadata.ResourceVersion != f.lease.Metadata.ResourceVersion {
		return nil, errLeaseConflict
	}

	return f.store(l), nil
}

func (f *fakeLeases) delete(_ context.Context, uid, resourceVersion string) error {
	if f.lease == nil {
		return nil
	}

	if f.lease.Metadata.UID != uid || f.lease.Metadata.ResourceVersion != resourceVersion {
		return errLeaseConflict
	}

	f.lease = nil

	return nil
}

func notSettled(*upgradeLockInfo) bool { return false }

func TestUpgradeLockAcquire(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	a := upgradeLockRequest{holder: "ichor/a", node: "10.0.0.1", hostname: "cp-1", from: "v1.11.2", to: "v1.12.0"}
	b := upgradeLockRequest{holder: "ichor/b", node: "10.0.0.2", hostname: "cp-2", from: "v1.11.2", to: "v1.12.0"}

	t.Run("free, then held by another run", func(t *testing.T) {
		f := &fakeLeases{}

		res, err := acquireUpgradeLock(ctx, f, a, notSettled, now)
		if err != nil || res.lease == nil || res.held != nil {
			t.Fatalf("first acquire: %+v %v", res, err)
		}

		if f.lease.Spec.HolderIdentity != "ichor/a" || f.lease.Metadata.Annotations[annotationHostname] != "cp-1" {
			t.Errorf("lease = %+v", f.lease)
		}

		other, err := acquireUpgradeLock(ctx, f, b, notSettled, now.Add(time.Minute))
		if err != nil || other.held == nil || other.lease != nil {
			t.Fatalf("second acquire: %+v %v", other, err)
		}

		if msg := other.held.describe(); !strings.Contains(msg, "cp-1 to v1.12.0") {
			t.Errorf("describe = %q", msg)
		}

		// The same run taking it again (retry) keeps it.
		if again, err := acquireUpgradeLock(ctx, f, a, notSettled, now.Add(time.Minute)); err != nil || again.lease == nil {
			t.Errorf("re-acquire by holder: %+v %v", again, err)
		}
	})

	t.Run("an expired lock is taken over", func(t *testing.T) {
		f := &fakeLeases{}
		if _, err := acquireUpgradeLock(ctx, f, a, notSettled, now); err != nil {
			t.Fatal(err)
		}

		res, err := acquireUpgradeLock(ctx, f, b, notSettled, now.Add(upgradeLockDuration+time.Second))
		if err != nil || res.lease == nil || f.lease.Spec.HolderIdentity != "ichor/b" {
			t.Fatalf("takeover: %+v %v", res, err)
		}
	})

	t.Run("a settled upgrade frees its lock", func(t *testing.T) {
		f := &fakeLeases{}
		if _, err := acquireUpgradeLock(ctx, f, a, notSettled, now); err != nil {
			t.Fatal(err)
		}

		settled := func(i *upgradeLockInfo) bool {
			return lockSettled(i, []planPeer{running("10.0.0.1", "v1.12.0", true)})
		}

		if res, err := acquireUpgradeLock(ctx, f, b, settled, now.Add(time.Minute)); err != nil || res.lease == nil {
			t.Fatalf("settled: %+v %v", res, err)
		}
	})

	t.Run("losing the takeover race is an error, not a lock", func(t *testing.T) {
		f := &fakeLeases{}
		if _, err := acquireUpgradeLock(ctx, f, a, notSettled, now); err != nil {
			t.Fatal(err)
		}

		f.raceOnReplace = true

		res, err := acquireUpgradeLock(ctx, f, b, notSettled, now.Add(upgradeLockDuration+time.Second))
		if !errors.Is(err, errLeaseConflict) || res.lease != nil {
			t.Fatalf("race: %+v %v", res, err)
		}
	})

	t.Run("release only frees our own lock", func(t *testing.T) {
		f := &fakeLeases{}

		res, err := acquireUpgradeLock(ctx, f, a, notSettled, now)
		if err != nil {
			t.Fatal(err)
		}

		// Expired and taken over by b: releasing a's lease must not delete b's.
		if _, err := acquireUpgradeLock(ctx, f, b, notSettled, now.Add(upgradeLockDuration+time.Second)); err != nil {
			t.Fatal(err)
		}

		if err := releaseUpgradeLock(ctx, f, res.lease); err != nil || f.lease == nil || f.lease.Spec.HolderIdentity != "ichor/b" {
			t.Fatalf("stale release: %v %+v", err, f.lease)
		}

		mine, _ := f.get(ctx)
		if err := releaseUpgradeLock(ctx, f, mine); err != nil || f.lease != nil {
			t.Fatalf("release: %v %+v", err, f.lease)
		}
	})

	t.Run("API errors are returned", func(t *testing.T) {
		f := &fakeLeases{err: errors.New("forbidden")}
		if _, err := acquireUpgradeLock(ctx, f, a, notSettled, now); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestLockSettled(t *testing.T) {
	info := &upgradeLockInfo{node: "10.0.0.1", from: "v1.11.2", to: "v1.12.0"}

	cases := []struct {
		name  string
		info  *upgradeLockInfo
		peers []planPeer
		want  bool
	}{
		{"on the target version", info, []planPeer{running("10.0.0.1", "v1.12.0", true)}, true},
		{"still on the old version", info, []planPeer{running("10.0.0.1", "v1.11.2", true)}, false},
		{"rebooting", info, []planPeer{{node: "10.0.0.1", reachable: true, stage: "rebooting", version: "v1.12.0"}}, false},
		{"node unknown", info, []planPeer{running("10.0.0.9", "v1.12.0", true)}, false},
		{"reinstall", &upgradeLockInfo{node: "10.0.0.1", from: "v1.12.0", to: "v1.12.0"}, []planPeer{running("10.0.0.1", "v1.12.0", true)}, false},
	}

	for _, c := range cases {
		if got := lockSettled(c.info, c.peers); got != c.want {
			t.Errorf("%s: got %t", c.name, got)
		}
	}
}

func TestComputePlanLockAndAcknowledge(t *testing.T) {
	base := func() planInput {
		return planInput{
			target:       running("10.0.0.2", "v1.11.2", false),
			others:       []planPeer{running("10.0.0.1", "v1.11.2", true)},
			defaultImage: "ghcr.io/siderolabs/installer:v1.11.2",
		}
	}

	in := base()
	in.lock = &lockState{held: &upgradeLockInfo{node: "10.0.0.1", hostname: "cp-1", from: "v1.11.2", to: "v1.12.0", since: time.Now(), expires: time.Now().Add(time.Hour)}}

	if p := computePlan(in); !containsText(p.Blockers, "holds the cluster upgrade lock") || p.Forceable {
		t.Errorf("held lock: %+v", p)
	}

	in.others = []planPeer{running("10.0.0.1", "v1.12.0", true)}
	if p := computePlan(in); containsText(p.Blockers, "upgrade lock") {
		t.Errorf("settled lock still blocks: %+v", p)
	}

	in = base()
	in.lock = &lockState{err: "Kubernetes API: connection refused"}

	if p := computePlan(in); len(p.Blockers) != 0 || !containsText(p.Warnings, "cannot check the cluster upgrade lock") {
		t.Errorf("unreadable lock: %+v", p)
	}

	in = base()
	in.endpoints = []string{"10.0.0.2:50000", "host-10.0.0.2"}

	if p := computePlan(in); !containsText(p.Acknowledge, "only Talos endpoint") {
		t.Errorf("only endpoint: %+v", p)
	}

	in.endpoints = []string{"10.0.0.2", "10.0.0.1"}
	if p := computePlan(in); len(p.Acknowledge) != 0 {
		t.Errorf("two endpoints: %+v", p)
	}
}

func TestUpgradeVersionCheck(t *testing.T) {
	cases := []struct{ from, to, want string }{
		{"v1.11.2", "v1.11.5", ""},
		{"v1.11.2", "v1.12.0", ""},
		{"1.11.2", "v1.12.0-beta.1", ""},
		{"v1.11.2", "v1.11.2", ""},
		{"v1.10.7", "v1.12.0", "skips minor versions"},
		{"v1.12.0", "v2.0.0", "skips minor versions"},
		{"v1.12.1", "v1.12.0", "older"},
		{"v1.12.0", "v1.11.9", "older"},
		{"v1.12.0", "latest", ""},
		{"", "v1.12.0", ""},
	}

	for _, c := range cases {
		got := UpgradeVersionCheck(c.from, c.to)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s -> %s: %q", c.from, c.to, got)
		}
	}
}

func TestAcknowledgmentRefusal(t *testing.T) {
	plan := upgradePlan{CurrentVersion: "v1.11.2", Acknowledge: []string{"single control plane: down"}}

	if err := acknowledgmentRefusal(plan, "v1.12.0", false); err == nil || !strings.Contains(err.Error(), "single control plane") {
		t.Errorf("unacknowledged: %v", err)
	}

	if err := acknowledgmentRefusal(plan, "v1.12.0", true); err != nil {
		t.Errorf("acknowledged: %v", err)
	}

	clean := upgradePlan{CurrentVersion: "v1.11.2"}
	if err := acknowledgmentRefusal(clean, "v1.13.0", false); err == nil || !strings.Contains(err.Error(), "skips minor") {
		t.Errorf("version jump: %v", err)
	}

	if err := acknowledgmentRefusal(clean, "v1.12.0", false); err != nil {
		t.Errorf("clean: %v", err)
	}
}

func TestKubeLeaseStoreCreate(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"POST " + leasesPath: `{"metadata":{"name":"ichor-talos-upgrade","uid":"u1","resourceVersion":"7"},"spec":{"holderIdentity":"ichor/a"}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s := kubeLeaseStore{k}

	if l, err := s.get(context.Background()); err != nil || l != nil {
		t.Fatalf("missing lease: %+v %v", l, err)
	}

	l, err := s.create(context.Background(), upgradeLockRequest{holder: "ichor/a"}.lease(time.Now(), nil))
	if err != nil || l.Metadata.UID != "u1" {
		t.Fatalf("create: %+v %v", l, err)
	}

	var body string

	for _, r := range f.recorded() {
		if r.method == "POST" {
			body = r.body
		}
	}

	if !strings.Contains(body, `"kind":"Lease"`) || !strings.Contains(body, `"leaseDurationSeconds":2400`) {
		t.Errorf("POST body = %s", body)
	}
}
