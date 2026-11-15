package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newClusterHost is where the fake's installed apid answers (serveCAPort): the generated
// talosconfig names the host only, and the client dials Talos's own port on it.
const newClusterHost = "127.0.0.1"

func newClusterSpecJSON(t *testing.T, edit func(*newClusterSpec)) string {
	t.Helper()

	spec := newClusterSpec{Name: "lab", Endpoint: newClusterHost, TalosVersion: "v1.14.2", InstallDisk: "/dev/vda"}
	if edit != nil {
		edit(&spec)
	}

	b, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func prepareNewClusterTest(t *testing.T, edit func(*newClusterSpec)) newClusterDraft {
	t.Helper()

	out, err := NewClusterPrepare(newClusterSpecJSON(t, edit))
	if err != nil {
		t.Fatal(err)
	}

	var d newClusterDraft
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}

	return d
}

func TestNewClusterPrepare(t *testing.T) {
	d := prepareNewClusterTest(t, nil)

	newClusterDraftsMu.Lock()
	entry := newClusterDrafts[d.DraftID]
	newClusterDraftsMu.Unlock()

	if entry == nil {
		t.Fatal("the draft is not kept")
	}

	cfg, err := clientconfig.FromString(d.Talosconfig)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Context != "lab" || len(cfg.Contexts) != 1 || !slices.Equal(cfg.Contexts["lab"].Endpoints, []string{newClusterHost}) {
		t.Errorf("talosconfig: context %q, %+v", cfg.Context, cfg.Contexts)
	}

	for _, want := range []string{"ghcr.io/siderolabs/installer:v1.14.2", "/dev/vda", "https://127.0.0.1:6443"} {
		if !strings.Contains(entry.full, want) {
			t.Errorf("the config lacks %q", want)
		}
	}

	if strings.Contains(entry.full, "NoSchedule") {
		t.Error("a single control plane does not run workloads by default")
	}

	token := entry.bundle.Secrets.BootstrapToken
	if !strings.Contains(entry.full, token) || strings.Contains(d.RedactedControlplane, token) || strings.Contains(d.RedactedWorker, token) {
		t.Error("the preview is not redacted")
	}

	if strings.Contains(d.RedactedControlplane, "PRIVATE KEY") || strings.Contains(d.RedactedWorker, "PRIVATE KEY") {
		t.Error("the preview carries a key")
	}

	if d.KubernetesVersion != constants.DefaultKubernetesVersion || d.TalosVersion != "v1.14.2" || d.Size != len(entry.full) || len(d.Warnings) < 3 {
		t.Errorf("draft: %+v", d)
	}
}

func TestNewClusterPrepareOptions(t *testing.T) {
	off := false

	d := prepareNewClusterTest(t, func(s *newClusterSpec) {
		s.KubernetesVersion = "v1.34.1"
		s.InstallImage = "factory.talos.dev/installer/abc:v1.14.2"
		s.AllowScheduling = &off
		s.AdditionalSANs = []string{"lab.example.org", "lab.example.org", " "}
		s.ExtraEditsJSON = `[{"doc":0,"path":["debug"],"op":"set","type":"bool","value":"true"}]`
	})

	entry, err := takeNewClusterDraft(d.DraftID)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"factory.talos.dev/installer/abc:v1.14.2", "lab.example.org", "v1.34.1", "debug: true"} {
		if !strings.Contains(entry.full, want) {
			t.Errorf("the config lacks %q", want)
		}
	}

	if !strings.Contains(entry.full, "NoSchedule") || strings.Count(entry.full, "lab.example.org") != 2 {
		t.Error("scheduling or SANs not as asked")
	}
}

func TestNewClusterPrepareRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*newClusterSpec)
		want string
	}{
		{"no name", func(s *newClusterSpec) { s.Name = "" }, "cluster name"},
		{"dotted name", func(s *newClusterSpec) { s.Name = "a.b" }, "cluster name"},
		{"no address", func(s *newClusterSpec) { s.Endpoint = "" }, "IP address"},
		{"bad address", func(s *newClusterSpec) { s.Endpoint = "a b" }, "IP address"},
		{"no disk", func(s *newClusterSpec) { s.InstallDisk = "sda" }, "not a disk"},
		{"bad k8s", func(s *newClusterSpec) { s.KubernetesVersion = "2.0" }, "Kubernetes version"},
		{"bad talos", func(s *newClusterSpec) { s.TalosVersion = "latest" }, "Talos version"},
		{"bad image", func(s *newClusterSpec) { s.InstallImage = "a b" }, "image reference"},
		{"bad edits", func(s *newClusterSpec) { s.ExtraEditsJSON = "{" }, "invalid edits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClusterPrepare(newClusterSpecJSON(t, tc.edit))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}

	if _, err := NewClusterPrepare("{"); err == nil {
		t.Error("malformed JSON accepted")
	}

	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	if _, err := NewClusterPrepare(newClusterSpecJSON(t, nil)); err == nil || err.Error() != errJoinPrivacy.Error() {
		t.Errorf("privacy: got %v", err)
	}
}

func TestNewClusterKubeVersion(t *testing.T) {
	for _, tc := range []struct {
		k8s, talos string
		defaulted  bool
		want, err  string
	}{
		{"1.37.1", "", true, "1.37.1", ""},
		{"1.37.1", "v1.14.2", true, "1.37.1", ""},
		{"1.37.1", "v1.12.0", true, "1.35.0", ""},
		{"1.37.1", "v1.12.0", false, "", "too new"},
		{"1.20.0", "v1.12.0", true, "", "too old"},
		{"1.37.1", "v9.0.0", true, "1.37.1", ""},
	} {
		got, err := newClusterKubeVersion(tc.k8s, tc.talos, tc.defaulted)
		if got != tc.want || (tc.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s on %s (defaulted %v): got %q, %v", tc.k8s, tc.talos, tc.defaulted, got, err)
		}
	}
}

// The demo's maintenance address gets a throwaway draft: nothing is dialled.
func TestNewClusterPrepareDemo(t *testing.T) {
	d := prepareNewClusterTest(t, func(s *newClusterSpec) { s.Endpoint = demoMaintenanceAddress; s.TalosVersion = demoTalosVersion })
	if d.DraftID == "" || d.Talosconfig == "" {
		t.Errorf("draft: %+v", d)
	}
}

// newClusterMachine is a machine in maintenance mode (the fake answering calls that name no
// node) whose installed apid, once the config arrives, answers on serveCAPort with the
// draft's own secrets.
func newClusterMachine(t *testing.T) (f *fakeTalos, address string, d newClusterDraft) {
	t.Helper()

	f = newFakeTalos()
	f.setVersion("", "v1.14.2")
	address, _ = f.serve(t)

	d = prepareNewClusterTest(t, nil)

	newClusterDraftsMu.Lock()
	bundle := newClusterDrafts[d.DraftID].bundle
	newClusterDraftsMu.Unlock()

	f.serveCA(t, bundle)
	f.kubeconfigs = map[string][]byte{newClusterHost: []byte("apiVersion: v1\nkind: Config\n")}

	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		go func() {
			time.Sleep(joinStepDelay)
			f.setVersion(newClusterHost, "v1.14.2")
		}()
	}

	return f, address, d
}

func startNewClusterTest(d newClusterDraft, address, stored string, rec *joinRecorder, timeout time.Duration) *JoinRun {
	c := &newClusterRun{draftID: d.DraftID, address: address, stored: stored, listener: maskedJoinListener{rec}, interval: joinPollInterval}

	return c.start(timeout)
}

func TestStartNewClusterPhases(t *testing.T) {
	f, address, d := newClusterMachine(t)
	f.bootstrapErrs = []error{status.Error(codes.FailedPrecondition, "bootstrap is not available yet"), status.Error(codes.Unavailable, "etcd starting")}

	rec := newJoinRecorder()
	startNewClusterTest(d, address, d.Talosconfig, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" || node != newClusterHost {
		t.Fatalf("got %q, %q", node, errMessage)
	}

	want := []string{newClusterPhaseSecrets, newClusterPhaseApplying, newClusterPhaseInstalling, newClusterPhaseBooted, newClusterPhaseBootstrapping, newClusterPhaseReady}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases %v, want %v", got, want)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.applies) != 1 || f.applies[0].node != "" || f.applies[0].mode != machineapi.ApplyConfigurationRequest_AUTO {
		t.Errorf("applies %+v, want one AUTO apply in maintenance mode", f.applies)
	}

	if len(f.bootstraps) != 3 {
		t.Errorf("%d bootstraps, want two refusals then one success", len(f.bootstraps))
	}

	if _, err := takeNewClusterDraft(d.DraftID); err == nil {
		t.Error("the draft survived its run")
	}
}

// A bootstrap whose answer was lost, retried onto a cluster that has it: a success.
func TestStartNewClusterBootstrapAlreadyDone(t *testing.T) {
	f, address, d := newClusterMachine(t)
	f.bootstrapErrs = []error{status.Error(codes.AlreadyExists, "etcd data directory is not empty")}

	rec := newJoinRecorder()
	startNewClusterTest(d, address, d.Talosconfig, rec, time.Minute)

	if _, errMessage, _ := rec.wait(t); errMessage != "" {
		t.Errorf("got %q", errMessage)
	}
}

// A refused bootstrap after the apply fails the run, keeps no draft, and says so.
func TestStartNewClusterBootstrapRefused(t *testing.T) {
	f, address, d := newClusterMachine(t)
	f.bootstrapErrs = []error{status.Error(codes.PermissionDenied, "not authorized")}

	rec := newJoinRecorder()
	startNewClusterTest(d, address, d.Talosconfig, rec, time.Minute)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "bootstrap etcd") {
		t.Errorf("got %q", errMessage)
	}

	if len(f.bootstraps) != 1 {
		t.Errorf("%d bootstraps, want one", len(f.bootstraps))
	}

	if _, err := takeNewClusterDraft(d.DraftID); err == nil {
		t.Error("a draft that was sent is kept")
	}
}

// Every refusal comes before anything is sent; the draft stays for another try.
func TestStartNewClusterRefusedBeforeApply(t *testing.T) {
	f, address, d := newClusterMachine(t)

	for _, tc := range []struct {
		name, draft, address, stored, want string
	}{
		{"demo", d.DraftID, demoMaintenanceAddress, d.Talosconfig, errDemoUnavailable.Error()},
		{"unknown draft", "nope", address, d.Talosconfig, "draft is gone"},
		{"not stored", d.DraftID, address, "", errNewClusterNotStored.Error()},
		{"public address", d.DraftID, "8.8.8.8", d.Talosconfig, "public"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := newJoinRecorder()
			startNewClusterTest(newClusterDraft{DraftID: tc.draft}, tc.address, tc.stored, rec, time.Minute)

			if _, errMessage, _ := rec.wait(t); !strings.Contains(errMessage, tc.want) {
				t.Errorf("got %q, want %q", errMessage, tc.want)
			}
		})
	}

	SetPrivacyMask(true, "")

	rec := newJoinRecorder()
	startNewClusterTest(d, address, d.Talosconfig, rec, time.Minute)

	_, errMessage, _ := rec.wait(t)

	SetPrivacyMask(false, "")

	if errMessage != errJoinPrivacy.Error() {
		t.Errorf("privacy: got %q", errMessage)
	}

	if len(f.applies) != 0 {
		t.Fatalf("a refused run sent the config: %+v", f.applies)
	}

	// The draft is still there: the right talosconfig now goes through.
	rec = newJoinRecorder()
	startNewClusterTest(d, address, d.Talosconfig, rec, time.Minute)

	if _, errMessage, _ := rec.wait(t); errMessage != "" {
		t.Errorf("retry: got %q", errMessage)
	}
}

// Cancel after the apply only stops following: the config went once, nothing else.
func TestStartNewClusterCancelAfterApply(t *testing.T) {
	f, address, d := newClusterMachine(t)

	applied := make(chan struct{})
	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()
		close(applied)
	}

	rec := newJoinRecorder()
	run := startNewClusterTest(d, address, d.Talosconfig, rec, time.Minute)

	select {
	case <-applied:
	case <-time.After(10 * time.Second):
		t.Fatal("the config was never applied")
	}

	run.Cancel()

	if _, errMessage, _ := rec.wait(t); errMessage != errJoinStopped.Error() {
		t.Errorf("got %q, want %q", errMessage, errJoinStopped)
	}

	if len(f.applies) != 1 || len(f.called("Bootstrap")) != 0 || len(f.called("Reset")) != 0 {
		t.Errorf("applies %d, bootstraps %v, resets %v", len(f.applies), f.called("Bootstrap"), f.called("Reset"))
	}
}

// The activity log names the cluster and the machine, never the config or its secrets.
func TestStartNewClusterAudited(t *testing.T) {
	withDataDir(t)

	_, address, d := newClusterMachine(t)

	rec := newJoinRecorder()
	startNewClusterTest(d, address, d.Talosconfig, rec, time.Minute)

	if _, errMessage, _ := rec.wait(t); errMessage != "" {
		t.Fatal(errMessage)
	}

	generated := readAudit(t, "", "new-cluster-generated")
	runs := readAudit(t, "", "new-cluster-bootstrap")

	if len(generated) != 1 || len(runs) != 1 || runs[0].Outcome != auditOK || runs[0].Node != newClusterHost {
		t.Fatalf("generated %+v, runs %+v", generated, runs)
	}

	for _, e := range append(generated, runs...) {
		if !strings.Contains(e.Params, "name=lab") || strings.Contains(e.Params, "BEGIN") || strings.Contains(e.Params, "secrets") {
			t.Errorf("%s params %q", e.Action, e.Params)
		}
	}
}
