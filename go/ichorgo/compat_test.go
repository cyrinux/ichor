package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFriendlyErrorUnavailable(t *testing.T) {
	unavailable := []error{
		status.Error(codes.Unimplemented, "unknown service machine.LifecycleService"),
		status.Error(codes.Unimplemented, "unknown method Netstat for service machine.MachineService"),
		status.Error(codes.Unimplemented, "method PacketCapture not implemented"),
		fmt.Errorf("rpc failed: %w", status.Error(codes.Unimplemented, "nope")),
		errors.New(`rpc error: code = Unimplemented desc = unknown service machine.DebugService`),
		status.Error(codes.NotFound, `resource "smartstatuses" is not registered`),
	}

	for _, err := range unavailable {
		if !isUnavailableAPI(err) {
			t.Errorf("isUnavailableAPI(%v) = false", err)
		}

		if got := friendlyError(err); got != notAvailable {
			t.Errorf("friendlyError(%v) = %q", err, got)
		}
	}

	available := []error{
		nil,
		status.Error(codes.PermissionDenied, "not authorized"),
		status.Error(codes.NotFound, "resource LinkStatuses.net.talos.dev(network/eth9@undefined) doesn't exist"),
		status.Error(codes.Unavailable, "connection refused"),
		errors.New("boom"),
	}

	for _, err := range available {
		if isUnavailableAPI(err) {
			t.Errorf("isUnavailableAPI(%v) = true", err)
		}
	}

	if got := friendlyError(status.Error(codes.PermissionDenied, "x")); !strings.HasPrefix(got, "permission denied") {
		t.Errorf("permission denied mapping changed: %q", got)
	}
}

func TestNotAvailableNamesTheVersion(t *testing.T) {
	if got := notAvailableOn(""); got != "not available on this node's Talos version" {
		t.Errorf("got %q", got)
	}

	if got := notAvailableOn("v1.6.7"); got != "not available on this node's Talos version (v1.6.7)" {
		t.Errorf("got %q", got)
	}

	// A session that already knows the node's version names it without any call.
	s := &session{}
	s.versions.Store("10.0.0.3", "v1.7.0")

	if got := s.friendly("10.0.0.3", status.Error(codes.Unimplemented, "unknown service")); got != "not available on this node's Talos version (v1.7.0)" {
		t.Errorf("got %q", got)
	}

	if got := s.friendly("10.0.0.3", status.Error(codes.PermissionDenied, "no")); !strings.HasPrefix(got, "permission denied") {
		t.Errorf("got %q", got)
	}

	if got := s.nodeVersion(context.Background(), "10.0.0.3"); got != "v1.7.0" {
		t.Errorf("cached version = %q", got)
	}
}

// requiredFeatures are the names the apps rely on.
var requiredFeatures = []string{
	"events", "containers", "processes", "logFollow", "serviceControl", "packetCapture", "upgrade", "volumes",
	"diskUsage", "mounts", "kubespan", "etcd", "etcdSnapshot", "etcdMemberActions", "resourceBrowser",
	"supportBundle", "diskHealth", "issueConfig", "network", "connections", "time", "hardware", "images",
	"machineConfig", "debugShell", "reset",
}

func TestFeatureTable(t *testing.T) {
	names := make([]string, 0, len(featureRules))

	for _, r := range featureRules {
		if slices.Contains(names, r.name) {
			t.Errorf("duplicate feature %q", r.name)
		}

		names = append(names, r.name)

		if r.min != "" && (!strings.HasPrefix(r.min, "v1.") || len(strings.Split(r.min, ".")) != 2) {
			t.Errorf("%s: min %q is not a vMAJOR.MINOR version", r.name, r.min)
		}
	}

	for _, want := range requiredFeatures {
		if !slices.Contains(names, want) {
			t.Errorf("feature %q missing from the table", want)
		}
	}

	if len(names) != len(requiredFeatures) {
		t.Errorf("%d features, want %d", len(names), len(requiredFeatures))
	}
}

func TestComputeFeatures(t *testing.T) {
	unsupported := func(version string) []string {
		var out []string

		f := computeFeatures(version)
		if f.Version != version {
			t.Errorf("version = %q", f.Version)
		}

		for name, st := range f.Features {
			if !st.Supported {
				out = append(out, name)

				if !strings.Contains(st.Reason, "not available on this node's Talos version ("+version+")") ||
					!strings.Contains(st.Reason, st.MinVersion) {
					t.Errorf("%s on %s: reason %q", name, version, st.Reason)
				}
			} else if st.Reason != "" {
				t.Errorf("%s on %s: supported with reason %q", name, version, st.Reason)
			}
		}

		slices.Sort(out)

		return out
	}

	tests := []struct {
		version string
		want    []string
	}{
		{"v1.15.0", nil},
		// A prerelease of 1.15 has 1.15's features.
		{"v1.15.0-alpha.2", nil},
		{"v1.14.1", []string{"diskHealth"}},
		{"v1.13.0", []string{"diskHealth"}},
		{"v1.12.4", []string{"debugShell", "diskHealth"}},
		{"v1.8.0", []string{"debugShell", "diskHealth"}},
		{"v1.7.6", []string{"debugShell", "diskHealth", "volumes"}},
		{"v1.5.0", []string{"debugShell", "diskHealth", "volumes"}},
		{"v1.4.8", []string{"debugShell", "diskHealth", "images", "volumes"}},
		{"v1.3.7", []string{"connections", "debugShell", "diskHealth", "etcd", "images", "reset", "volumes"}},
		{"v1.2.0", []string{
			"connections", "debugShell", "diskHealth", "etcd", "etcdMemberActions", "hardware", "images", "kubespan",
			"machineConfig", "network", "reset", "resourceBrowser", "volumes",
		}},
		{"v1.1.3", []string{
			"connections", "debugShell", "diskHealth", "etcd", "etcdMemberActions", "hardware", "images", "kubespan",
			"machineConfig", "network", "packetCapture", "reset", "resourceBrowser", "volumes",
		}},
		{"v1.10.3", []string{"debugShell", "diskHealth"}}, // 1.10 > 1.8: compared as numbers
	}

	for _, tt := range tests {
		if got := unsupported(tt.version); !slices.Equal(got, tt.want) {
			t.Errorf("%s: unsupported = %v, want %v", tt.version, got, tt.want)
		}
	}

	if st := computeFeatures("v1.14.1").Features["volumes"]; !st.Supported || st.MinVersion != "v1.8" {
		t.Errorf("volumes on 1.14 = %+v", st)
	}

	if st := computeFeatures("v1.14.1").Features["processes"]; !st.Supported || st.MinVersion != "" {
		t.Errorf("processes = %+v", st)
	}

	if featureMinVersion("diskHealth") != "v1.15" || featureMinVersion("nope") != "" {
		t.Error("featureMinVersion")
	}
}

func TestCompareMinor(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.8.3", "v1.8", 0}, {"v1.10.0", "v1.8", 1}, {"v1.7.9", "v1.8", -1},
		{"1.15.0-alpha.1", "v1.15", 0}, {"v2.0.0", "v1.15", 1},
	}

	for _, tt := range tests {
		if got := compareMinor(tt.a, tt.b); got != tt.want {
			t.Errorf("compareMinor(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// fakeUpgrader records the calls of an upgrade; it never talks to a node.
type fakeUpgrader struct {
	legacyErr, pullErr, upgradeErr, rebootErr error
	exitCode                                  int32
	messages                                  []string
	calls                                     []string
}

func (f *fakeUpgrader) legacyUpgrade(_ context.Context, image string, stage, force bool) error {
	f.calls = append(f.calls, fmt.Sprintf("legacy %s stage=%t force=%t", image, stage, force))

	return f.legacyErr
}

func (f *fakeUpgrader) pullImage(_ context.Context, image string) error {
	f.calls = append(f.calls, "pull "+image)

	return f.pullErr
}

func (f *fakeUpgrader) lifecycleUpgrade(_ context.Context, image string, progress func(string)) (int32, error) {
	f.calls = append(f.calls, "lifecycle "+image)

	for _, m := range f.messages {
		progress(m)
	}

	return f.exitCode, f.upgradeErr
}

func (f *fakeUpgrader) reboot(context.Context) error {
	f.calls = append(f.calls, "reboot")

	return f.rebootErr
}

func TestRequestUpgrade(t *testing.T) {
	const image = "factory.talos.dev/installer/abc:v1.18.0"

	unimplemented := status.Error(codes.Unimplemented, "unknown method Upgrade for service machine.MachineService")

	run := func(f *fakeUpgrader, stage, force bool) ([]string, error) {
		var phases []string

		err := requestUpgrade(context.Background(), f, image, stage, force, false, func() error { return nil },
			func(phase, _ string) { phases = append(phases, phase) })

		return phases, err
	}

	t.Run("legacy API when the node has it", func(t *testing.T) {
		f := &fakeUpgrader{}

		phases, err := run(f, true, true)
		if err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(f.calls, []string{"legacy " + image + " stage=true force=true"}) {
			t.Errorf("calls = %v", f.calls)
		}

		if !slices.Equal(phases, []string{phaseInstall}) {
			t.Errorf("phases = %v", phases)
		}
	})

	t.Run("legacy errors are not retried on the new API", func(t *testing.T) {
		f := &fakeUpgrader{legacyErr: status.Error(codes.FailedPrecondition, "etcd member unhealthy")}

		_, err := run(f, false, false)
		if err == nil || !strings.Contains(err.Error(), "etcd member unhealthy") || len(f.calls) != 1 {
			t.Errorf("err = %v, calls = %v", err, f.calls)
		}
	})

	t.Run("falls back to the lifecycle API on Unimplemented", func(t *testing.T) {
		f := &fakeUpgrader{legacyErr: unimplemented, messages: []string{"installing", " ", "done\n"}}

		phases, err := run(f, false, true)
		if err != nil {
			t.Fatal(err)
		}

		want := []string{"legacy " + image + " stage=false force=true", "pull " + image, "lifecycle " + image, "reboot"}
		if !slices.Equal(f.calls, want) {
			t.Errorf("calls = %v", f.calls)
		}

		if !slices.Equal(phases, []string{phaseRequested, phaseInstall, phaseInstall, phaseInstall, phaseRebooting}) {
			t.Errorf("phases = %v", phases)
		}
	})

	t.Run("force never skips the etcd blockers on the lifecycle API", func(t *testing.T) {
		plan := computePlan(planInput{
			target: running("10.0.0.2", "v1.18.0", true),
			etcd:   &etcdHealth{members: 3, healthy: 2, thisMember: true, thisHealthy: true},
		})

		if plan.Forceable || !plan.Drainable || !containsText(plan.Warnings, noDrainWarning) {
			t.Fatalf("plan = %+v", plan)
		}

		// What StartUpgrade does: force passes the first check, the fallback checks again.
		if err := upgradeRefusal(plan, true); err != nil {
			t.Fatalf("forced refusal: %v", err)
		}

		var messages []string

		f := &fakeUpgrader{legacyErr: unimplemented}
		err := requestUpgrade(context.Background(), f, image, false, true, false, func() error { return upgradeRefusal(plan, false) },
			func(_, msg string) { messages = append(messages, msg) })

		if err == nil || !strings.Contains(err.Error(), "upgrade refused") || !strings.Contains(err.Error(), "force cannot skip") {
			t.Errorf("err = %v", err)
		}

		if len(f.calls) != 1 || len(messages) != 0 {
			t.Errorf("something ran after the refusal: %v %v", f.calls, messages)
		}

		// The legacy API (the node checks etcd itself) still honours force.
		f = &fakeUpgrader{}
		if err := requestUpgrade(context.Background(), f, image, false, true, false, func() error { return upgradeRefusal(plan, false) },
			func(string, string) {}); err != nil {
			t.Errorf("legacy forced: %v", err)
		}

		// The warning reaches the progress when the fallback runs, and older versions have neither.
		f = &fakeUpgrader{legacyErr: unimplemented}
		_ = requestUpgrade(context.Background(), f, image, false, false, false, func() error { return nil }, //nolint:errcheck
			func(_, msg string) { messages = append(messages, msg) })

		if !containsText(messages, noDrainWarning) {
			t.Errorf("messages = %v", messages)
		}

		old := computePlan(planInput{
			target: running("10.0.0.2", "v1.17.3", true),
			etcd:   &etcdHealth{members: 3, healthy: 2, thisMember: true, thisHealthy: true},
		})
		if !old.Forceable || old.Drainable || containsText(old.Warnings, noDrainWarning) {
			t.Errorf("1.17 plan = %+v", old)
		}
	})

	t.Run("a broken install stream says the node may need a reboot", func(t *testing.T) {
		f := &fakeUpgrader{legacyErr: unimplemented, messages: []string{"writing image"}, upgradeErr: status.Error(codes.Unavailable, "connection reset")}

		_, err := run(f, false, false)
		if err == nil || !strings.Contains(err.Error(), "without rebooting") || slices.Contains(f.calls, "reboot") {
			t.Errorf("err = %v, calls = %v", err, f.calls)
		}

		// Before the installer said anything, it is the plain failure.
		f = &fakeUpgrader{legacyErr: unimplemented, upgradeErr: errors.New("permission denied")}
		if _, err := run(f, false, false); err == nil || strings.Contains(err.Error(), "without rebooting") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("the lifecycle API has no staged upgrade", func(t *testing.T) {
		f := &fakeUpgrader{legacyErr: unimplemented}

		_, err := run(f, true, false)
		if err == nil || !strings.Contains(err.Error(), "staged") || len(f.calls) != 1 {
			t.Errorf("err = %v, calls = %v", err, f.calls)
		}
	})

	t.Run("a failed install never reboots", func(t *testing.T) {
		f := &fakeUpgrader{legacyErr: unimplemented, exitCode: 1, messages: []string{"no space left"}}

		_, err := run(f, false, false)
		if err == nil || !strings.Contains(err.Error(), "exit code 1") || !strings.Contains(err.Error(), "no space left") {
			t.Errorf("err = %v", err)
		}

		if slices.Contains(f.calls, "reboot") {
			t.Errorf("rebooted after a failed install: %v", f.calls)
		}

		f = &fakeUpgrader{legacyErr: unimplemented, pullErr: errors.New("manifest unknown")}

		if _, err := run(f, false, false); err == nil || slices.Contains(f.calls, "lifecycle "+image) {
			t.Errorf("err = %v, calls = %v", err, f.calls)
		}

		f = &fakeUpgrader{legacyErr: unimplemented, upgradeErr: status.Error(codes.FailedPrecondition, "another installation/upgrade is already in progress")}

		if _, err := run(f, false, false); err == nil || slices.Contains(f.calls, "reboot") {
			t.Errorf("err = %v, calls = %v", err, f.calls)
		}
	})

	t.Run("neither API: reported as not available", func(t *testing.T) {
		f := &fakeUpgrader{legacyErr: unimplemented, pullErr: status.Error(codes.Unimplemented, "unknown service machine.ImageService")}

		if _, err := run(f, false, false); !isUnavailableAPI(err) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("installed but the reboot request failed", func(t *testing.T) {
		f := &fakeUpgrader{legacyErr: unimplemented, rebootErr: status.Error(codes.PermissionDenied, "no")}

		if _, err := run(f, false, false); err == nil || !strings.Contains(err.Error(), "reboot the node") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestRememberVersionDropsDefinitionsOnChange(t *testing.T) {
	s := &session{}
	s.rememberVersion("10.0.0.1", "v1.14.1")
	s.definitions.Store("10.0.0.1", &resourceTypes{})
	s.definitions.Store("10.0.0.2", &resourceTypes{})

	s.rememberVersion("10.0.0.1", "v1.14.1")

	if _, ok := s.definitions.Load("10.0.0.1"); !ok {
		t.Fatal("definitions dropped although the version did not change")
	}

	s.rememberVersion("10.0.0.1", "")

	if v, _ := s.versions.Load("10.0.0.1"); v != "v1.14.1" {
		t.Fatalf("an empty version replaced the cached one: %v", v)
	}

	s.rememberVersion("10.0.0.1", "v1.15.0")

	if _, ok := s.definitions.Load("10.0.0.1"); ok {
		t.Fatal("definitions kept after the node's version changed")
	}

	if _, ok := s.definitions.Load("10.0.0.2"); !ok {
		t.Fatal("another node's definitions were dropped")
	}

	if v, _ := s.versions.Load("10.0.0.1"); v != "v1.15.0" {
		t.Fatalf("version not updated: %v", v)
	}
}
