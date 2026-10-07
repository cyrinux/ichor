package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// generatedConfig is a real control plane config, secrets included, made once.
var generatedConfig = sync.OnceValues(func() ([]byte, error) {
	in, err := generate.NewInput("test", "https://192.0.2.10:6443", "1.34.0")
	if err != nil {
		return nil, err
	}

	cfg, err := in.Config(machine.TypeControlPlane)
	if err != nil {
		return nil, err
	}

	return cfg.Bytes()
})

type fakeApply struct {
	data   string
	mode   machineapi.ApplyConfigurationRequest_Mode
	dryRun bool
	tryFor time.Duration
}

// fakeConfigNode is a node holding a config: a try replaces it until revert is called.
type fakeConfigNode struct {
	t        *testing.T
	active   []byte
	previous []byte
	calls    []fakeApply
	reboot   bool    // what AUTO answers
	failures []error // returned by the next real (not dry run) AUTO applies
	// tryErr is what a TRY answers; tryApplies tells whether the node applied it all the
	// same (an answer lost on the way back).
	tryErr     error
	tryApplies bool
}

func newFakeConfigNode(t *testing.T) *fakeConfigNode {
	t.Helper()

	raw, err := generatedConfig()
	if err != nil {
		t.Fatal(err)
	}

	return &fakeConfigNode{t: t, active: raw}
}

func (n *fakeConfigNode) current(context.Context) (configSnapshot, error) {
	provider, err := configloader.NewFromBytes(n.active)
	if err != nil {
		return configSnapshot{}, err
	}

	return snapshotOf(provider)
}

func (n *fakeConfigNode) apply(_ context.Context, data []byte, mode machineapi.ApplyConfigurationRequest_Mode, dryRun bool, tryFor time.Duration) (configApplyResult, error) {
	n.calls = append(n.calls, fakeApply{string(data), mode, dryRun, tryFor})

	result := configApplyResult{mode: machineapi.ApplyConfigurationRequest_NO_REBOOT}
	if n.reboot {
		result.mode = machineapi.ApplyConfigurationRequest_REBOOT
	}

	switch {
	case dryRun:
	case mode == machineapi.ApplyConfigurationRequest_TRY:
		if n.tryErr == nil || n.tryApplies {
			n.previous, n.active = n.active, data
		}

		if n.tryErr != nil {
			return configApplyResult{}, n.tryErr
		}
	default:
		if len(n.failures) > 0 {
			err := n.failures[0]
			n.failures = n.failures[1:]

			return configApplyResult{}, err
		}

		n.previous, n.active = nil, data
	}

	return result, nil
}

// revert is what Talos does when a try times out.
func (n *fakeConfigNode) revert() {
	if n.previous != nil {
		n.active, n.previous = n.previous, nil
	}
}

func (n *fakeConfigNode) base() string {
	n.t.Helper()

	snap, err := n.current(context.Background())
	if err != nil {
		n.t.Fatal(err)
	}

	return snap.redacted
}

// withLabel is base edited through MachineConfigEdit: one more node label.
func withLabel(t *testing.T, base, value string) string {
	t.Helper()

	// The label map may not be there yet.
	if with, err := MachineConfigEdit(base, `{"doc":0,"path":["machine"],"op":"add","key":"nodeLabels","type":"object"}`); err == nil {
		base = with
	}

	return edit(t, base, `{"doc":0,"path":["machine","nodeLabels"],"op":"add","key":"ichor.test/label","type":"string","value":"`+value+`"}`)
}

func TestSnapshotRedactsSecrets(t *testing.T) {
	node := newFakeConfigNode(t)

	snap, err := node.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(snap.redacted, redacted) || strings.Contains(snap.plain, redacted) {
		t.Fatal("the redacted config must hold the mask and the plain one must not")
	}

	if !strings.Contains(snap.plain, "BEGIN") && !strings.Contains(snap.plain, "LS0t") {
		t.Fatal("the plain config holds the certificates")
	}
}

func TestRestoreSecretsRoundTrip(t *testing.T) {
	node := newFakeConfigNode(t)

	snap, err := node.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// An untouched draft gives the node's config back, secrets and all.
	plan, err := planConfigChange(snap, snap.redacted, snap.redacted)
	if err != nil {
		t.Fatal(err)
	}

	if plan.changed() || strings.Contains(string(plan.data), redacted) {
		t.Fatal("an untouched draft must not change anything nor carry a mask")
	}

	canonical, err := canonicalConfig(string(plan.data))
	if err != nil || canonical != snap.plain {
		t.Fatalf("the restored config differs from the node's: %v", err)
	}
}

func TestPreviewShowsTheChangeWithoutSecrets(t *testing.T) {
	node := newFakeConfigNode(t)
	base := node.base()

	preview, err := previewConfig(context.Background(), node, base, withLabel(t, base, "node-renamed"))
	if err != nil {
		t.Fatal(err)
	}

	if !preview.Changed || preview.NeedsReboot {
		t.Fatalf("%+v", preview)
	}

	found := false

	for _, l := range preview.Lines {
		found = found || l.Kind == diffLineAdded && strings.Contains(l.Text, "node-renamed")

		if l.Kind == diffLineRemoved {
			t.Errorf("unexpected removed line %q", l.Text)
		}
	}

	if !found {
		t.Fatalf("the new label is not in the diff: %+v", preview.Lines)
	}

	// The node validates the real thing: one dry run, with the secrets and no mask.
	if len(node.calls) != 1 || !node.calls[0].dryRun || node.calls[0].mode != machineapi.ApplyConfigurationRequest_AUTO {
		t.Fatalf("calls: %+v", node.calls)
	}

	sent := node.calls[0].data
	if strings.Contains(sent, redacted) || !strings.Contains(sent, "node-renamed") {
		t.Fatal("what is sent must hold the edit and no mask")
	}

	out, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}

	snap, _ := node.current(context.Background()) //nolint:errcheck
	for _, line := range strings.Split(snap.plain, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), ": "); ok && len(value) > 40 && strings.Contains(string(out), value) {
			t.Fatalf("the preview leaks the value of %s", key)
		}
	}
}

func TestPreviewUnchangedAsksNothing(t *testing.T) {
	node := newFakeConfigNode(t)
	base := node.base()

	preview, err := previewConfig(context.Background(), node, base, base)
	if err != nil || preview.Changed || len(preview.Lines) != 0 || len(node.calls) != 0 {
		t.Fatalf("%+v %v %d", preview, err, len(node.calls))
	}
}

func TestPreviewReportsReboot(t *testing.T) {
	node := newFakeConfigNode(t)
	node.reboot = true
	base := node.base()

	preview, err := previewConfig(context.Background(), node, base, withLabel(t, base, "x"))
	if err != nil || !preview.NeedsReboot {
		t.Fatalf("%+v %v", preview, err)
	}
}

func TestPlanRefusals(t *testing.T) {
	node := newFakeConfigNode(t)

	snap, err := node.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	base := snap.redacted

	if _, err := planConfigChange(snap, withLabel(t, base, "stale"), base); !errors.Is(err, errConfigChanged) {
		t.Fatalf("a base that is not the node's config must be refused: %v", err)
	}

	for name, draft := range map[string]string{
		"secret changed":     strings.Replace(base, "token: '"+redacted+"'", "token: abcdef.0123456789abcdef", 1),
		"secret removed":     removeLine(base, "    token: '"+redacted+"'"),
		"mask typed":         withLabel(t, base, "placeholder") + "",
		"not YAML":           base + "\n  broken: [",
		"empty":              "",
		"not a config":       "machine: 5\n",
		"mask in new field":  strings.Replace(base, "machine:\n", "machine:\n    nodeLabels:\n        a: '"+redacted+"'\n", 1),
		"mask in new doc":    base + "---\napiVersion: v1alpha1\nkind: HostnameConfig\nhostname: '" + redacted + "'\n",
		"unknown field":      strings.Replace(base, "machine:\n", "machine:\n    notAField: 1\n", 1),
		"secret over object": strings.Replace(base, "debug: false", "debug: '"+redacted+"'", 1),
	} {
		if name == "mask typed" {
			draft = strings.Replace(draft, "placeholder", redacted, 1)
		}

		if draft == base {
			t.Fatalf("%s: the fixture changed nothing", name)
		}

		if plan, err := planConfigChange(snap, base, draft); err == nil {
			t.Errorf("%s must be refused (changed: %v)", name, plan.changed())
		}
	}
}

func removeLine(text, line string) string {
	return strings.Replace(text, line+"\n", "", 1)
}

func TestRestoreSecretsInLists(t *testing.T) {
	real := "peers:\n    - name: a\n      key: secret-a\n    - name: b\n      key: secret-b\n    - name: c\n      key: secret-c\n"
	base := strings.NewReplacer("secret-a", "'******'", "secret-b", "'******'", "secret-c", "'******'").Replace(real)

	item := func(name string) string { return "    - name: " + name + "\n      key: '******'\n" }

	for name, tc := range map[string]struct {
		draft string
		want  string // "" when it must be refused
	}{
		"untouched":            {base, real},
		"reordered":            {"peers:\n" + item("c") + item("a") + item("b"), "peers:\n    - name: c\n      key: secret-c\n    - name: a\n      key: secret-a\n    - name: b\n      key: secret-b\n"},
		"one removed":          {"peers:\n" + item("a") + item("c"), "peers:\n    - name: a\n      key: secret-a\n    - name: c\n      key: secret-c\n"},
		"one added":            {base + "    - name: d\n      key: new-key\n", real + "    - name: d\n      key: new-key\n"},
		"one edited in place":  {strings.Replace(base, "name: b", "name: b2", 1), strings.Replace(real, "name: b", "name: b2", 1)},
		"edited and one gone":  {"peers:\n" + strings.Replace(item("b"), "name: b", "name: b2", 1) + item("c"), ""},
		"edited and moved":     {"peers:\n" + strings.Replace(item("b"), "name: b", "name: b2", 1) + item("a") + item("c"), ""},
		"added with a mask":    {base + item("d"), ""},
		"secret typed in item": {strings.Replace(base, "'******'", "typed", 1), ""},
	} {
		got, err := restoreSecrets(real, base, tc.draft)

		switch {
		case tc.want == "" && err == nil:
			t.Errorf("%s must be refused, got:\n%s", name, got)
		case tc.want != "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case tc.want != "" && got != tc.want:
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, tc.want)
		}
	}
}

func TestRestoreSecretsAcrossDocuments(t *testing.T) {
	real := "version: v1alpha1\nmachine:\n    token: sec-m\n---\napiVersion: v1alpha1\nkind: WireguardConfig\nname: wg0\nprivateKey: key0\n---\napiVersion: v1alpha1\nkind: WireguardConfig\nname: wg1\nprivateKey: key1\n"
	base := strings.NewReplacer("sec-m", "'******'", "key0", "'******'", "key1", "'******'").Replace(real)

	docs := strings.Split(base, "---\n")

	// Documents reordered, one dropped, one added: each mask finds its own secret.
	draft := docs[2] + "---\n" + docs[0] + "---\napiVersion: v1alpha1\nkind: HostnameConfig\nhostname: h\n"

	got, err := restoreSecrets(real, base, draft)
	if err != nil {
		t.Fatal(err)
	}

	want := "apiVersion: v1alpha1\nkind: WireguardConfig\nname: wg1\nprivateKey: key1\n---\nversion: v1alpha1\nmachine:\n    token: sec-m\n---\napiVersion: v1alpha1\nkind: HostnameConfig\nhostname: h\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// Renaming a document makes it a new one: its mask has no secret to come from.
	if _, err := restoreSecrets(real, base, strings.Replace(base, "name: wg0", "name: wg9", 1)); err == nil {
		t.Fatal("a renamed document with a mask must be refused")
	}
}

// tryHarness runs runConfigTry against a fake node with a clock the test drives.
type tryHarness struct {
	node     *fakeConfigNode
	commands chan string
	expire   chan time.Time
	phases   []string
	messages []string
}

func newTryHarness(t *testing.T) *tryHarness {
	t.Helper()

	return &tryHarness{node: newFakeConfigNode(t), commands: make(chan string, 4), expire: make(chan time.Time, 1)}
}

func (h *tryHarness) run(ctx context.Context, draft func(base string) string, timeoutSec int) (string, error) {
	base := h.node.base()

	return runConfigTry(ctx, h.node, configTry{
		base: base, draft: draft(base), timeoutSec: timeoutSec, commands: h.commands,
		after: func(d time.Duration) <-chan time.Time {
			if d == configRevertWait {
				fired := make(chan time.Time, 1)
				fired <- time.Time{}

				return fired
			}

			return h.expire
		},
		emit: func(phase, message string, _ time.Time) {
			h.phases = append(h.phases, phase)
			if message != "" {
				h.messages = append(h.messages, message)
			}
		},
	})
}

func renamed(t *testing.T) func(string) string {
	return func(base string) string { return withLabel(t, base, "tried") }
}

func TestTryThenKeep(t *testing.T) {
	h := newTryHarness(t)
	h.commands <- tryCommandKeep

	outcome, err := h.run(context.Background(), renamed(t), 300)
	if err != nil || outcome != tryOutcomeKept {
		t.Fatalf("%q %v", outcome, err)
	}

	calls := h.node.calls
	if len(calls) != 3 {
		t.Fatalf("calls: %d", len(calls))
	}

	if !calls[0].dryRun || calls[0].mode != machineapi.ApplyConfigurationRequest_AUTO {
		t.Errorf("first a dry run: %+v", calls[0].mode)
	}

	if calls[1].dryRun || calls[1].mode != machineapi.ApplyConfigurationRequest_TRY || calls[1].tryFor != 5*time.Minute {
		t.Errorf("then the try with its timeout: %v %v", calls[1].mode, calls[1].tryFor)
	}

	if calls[2].dryRun || calls[2].mode != machineapi.ApplyConfigurationRequest_AUTO || calls[2].data != calls[1].data {
		t.Errorf("keeping applies the same config in AUTO mode")
	}

	if strings.Join(h.phases, ",") != "applying,trying,keeping" {
		t.Errorf("phases: %v", h.phases)
	}

	if !strings.Contains(string(h.node.active), "tried") || h.node.previous != nil {
		t.Error("the kept config is the node's, with nothing left to revert")
	}
}

func TestTryThenRevertNow(t *testing.T) {
	h := newTryHarness(t)
	original := string(h.node.active)
	h.commands <- tryCommandRevert

	outcome, err := h.run(context.Background(), renamed(t), 60)
	if err != nil || outcome != tryOutcomeReverted {
		t.Fatalf("%q %v", outcome, err)
	}

	if last := h.node.calls[len(h.node.calls)-1]; last.data != original || last.mode != machineapi.ApplyConfigurationRequest_AUTO || last.dryRun {
		t.Fatal("reverting applies the node's original bytes in AUTO mode")
	}
}

func TestTryTimesOutAndReverts(t *testing.T) {
	h := newTryHarness(t)
	original := string(h.node.active)

	go func() {
		// Talos reverts, then the app's own timer fires.
		for len(h.node.calls) < 2 {
			time.Sleep(time.Millisecond)
		}

		h.node.revert()
		h.expire <- time.Time{}
	}()

	outcome, err := h.run(context.Background(), renamed(t), 600)
	if err != nil || outcome != tryOutcomeReverted {
		t.Fatalf("%q %v", outcome, err)
	}

	if len(h.node.calls) != 2 || string(h.node.active) != original {
		t.Fatalf("nothing is applied after the try: %d calls", len(h.node.calls))
	}
}

func TestTryTimeoutWithoutRevertIsAnError(t *testing.T) {
	h := newTryHarness(t)
	h.expire <- time.Time{}

	if outcome, err := h.run(context.Background(), renamed(t), 60); err == nil {
		t.Fatalf("a node still showing the edited config must be reported, got %q", outcome)
	}
}

func TestTryKeepFailureCanBeRetried(t *testing.T) {
	h := newTryHarness(t)
	h.node.failures = []error{errors.New("connection lost")}
	h.commands <- tryCommandKeep
	h.commands <- tryCommandKeep

	outcome, err := h.run(context.Background(), renamed(t), 300)
	if err != nil || outcome != tryOutcomeKept {
		t.Fatalf("%q %v", outcome, err)
	}

	if strings.Join(h.phases, ",") != "applying,trying,keeping,trying,keeping" || len(h.messages) != 1 || !strings.Contains(h.messages[0], "connection lost") {
		t.Fatalf("phases %v messages %v", h.phases, h.messages)
	}
}

func TestTryRefusals(t *testing.T) {
	t.Run("needs a reboot", func(t *testing.T) {
		h := newTryHarness(t)
		h.node.reboot = true

		if _, err := h.run(context.Background(), renamed(t), 300); !errors.Is(err, errConfigNeedsReboot) {
			t.Fatal(err)
		}

		if len(h.node.calls) != 1 || !h.node.calls[0].dryRun {
			t.Fatal("only the dry run reaches the node")
		}
	})

	t.Run("nothing changed", func(t *testing.T) {
		h := newTryHarness(t)

		if _, err := h.run(context.Background(), func(base string) string { return base }, 300); !errors.Is(err, errConfigUnchanged) || len(h.node.calls) != 0 {
			t.Fatal(err)
		}
	})

	t.Run("timeout not offered", func(t *testing.T) {
		for _, seconds := range []int{0, -1, 30, 3600} {
			h := newTryHarness(t)

			if _, err := h.run(context.Background(), renamed(t), seconds); err == nil || len(h.node.calls) != 0 {
				t.Fatalf("%d s must be refused", seconds)
			}
		}
	})

	t.Run("stopped following", func(t *testing.T) {
		h := newTryHarness(t)

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			for len(h.node.calls) < 2 {
				time.Sleep(time.Millisecond)
			}

			cancel()
		}()

		if _, err := h.run(ctx, renamed(t), 300); !errors.Is(err, errStoppedTry) {
			t.Fatal(err)
		}
	})
}

type tryRecorder struct{ done chan [2]string }

func (r tryRecorder) OnProgress(string)                 {}
func (r tryRecorder) OnDone(outcome, errMessage string) { r.done <- [2]string{outcome, errMessage} }

func TestConfigEditInDemoAndPrivacyMode(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	node := demoNodes()[0].Node

	base, err := NodeMachineConfig(demo, "", node, false)
	if err != nil {
		t.Fatal(err)
	}

	draft := edit(t, base, `{"doc":0,"path":["machine","network","hostname"],"op":"set","type":"string","value":"demo-renamed"}`)

	out, err := MachineConfigPreview(demo, "", node, base, draft)
	if err != nil {
		t.Fatal(err)
	}

	var preview configPreview
	if err := json.Unmarshal([]byte(out), &preview); err != nil || !preview.Changed || len(preview.Lines) == 0 {
		t.Fatalf("%s %v", out, err)
	}

	rec := tryRecorder{make(chan [2]string, 1)}
	StartConfigTry(demo, "", node, base, draft, 60, rec)

	if got := <-rec.done; got[0] != "" || got[1] != errDemoUnavailable.Error() {
		t.Fatalf("the demo cannot be changed: %v", got)
	}

	SetPrivacyMask(true, "")
	defer SetPrivacyMask(false, "")

	if _, err := MachineConfigPreview(demo, "", node, base, draft); err == nil {
		t.Fatal("privacy mode must refuse a preview")
	}

	StartConfigTry(demo, "", node, base, draft, 60, rec)

	if got := <-rec.done; got[0] != "" || !strings.Contains(got[1], "privacy mode") {
		t.Fatalf("privacy mode must refuse a try: %v", got)
	}
}
