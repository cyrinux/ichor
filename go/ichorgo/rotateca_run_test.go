package ichorgo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
)

type caRecorder struct {
	events  chan caRotationProgress
	configs chan caHandedConfig
	done    chan string
}

type caHandedConfig struct {
	yaml  string
	final bool
}

func newCARecorder() *caRecorder {
	return &caRecorder{events: make(chan caRotationProgress, 1024), configs: make(chan caHandedConfig, 4), done: make(chan string, 1)}
}

func (r *caRecorder) OnProgress(js string) {
	var p caRotationProgress
	if err := json.Unmarshal([]byte(js), &p); err == nil {
		r.events <- p
	}
}

func (r *caRecorder) OnTalosconfig(yaml string, isFinal bool) {
	r.configs <- caHandedConfig{yaml, isFinal}
}

func (r *caRecorder) OnDone(errMessage string) { r.done <- errMessage }

// wait returns the run's error message and the phases it went through, in order.
func (r *caRecorder) wait(t *testing.T) (string, []string) {
	t.Helper()

	select {
	case msg := <-r.done:
		close(r.events)

		var phases []string

		for e := range r.events {
			if len(phases) == 0 || phases[len(phases)-1] != e.Phase {
				phases = append(phases, e.Phase)
			}
		}

		return msg, phases
	case <-time.After(20 * time.Second):
		t.Fatal("the rotation did not end")
	}

	return "", nil
}

// fastCARotation shortens the run's waits and makes its new clients reach the fake: the fake
// serves one certificate and checks none, so the clients use the test talosconfig's TLS
// whatever CA the run gave them; what the test checks is the configs the nodes end with.
func fastCARotation(t *testing.T, cfg string) {
	t.Helper()

	interval, handover, dial := upgradePollInterval, caHandoverFor, dialTalosconfig
	upgradePollInterval, caHandoverFor = 5*time.Millisecond, 2*time.Second

	parsed, err := clientconfig.FromString(cfg)
	if err != nil {
		t.Fatal(err)
	}

	dialTalosconfig = func(ctx context.Context, _ *clientconfig.Config) (*client.Client, error) {
		return client.New(ctx, client.WithConfig(parsed))
	}

	resetFingerprintLineage()

	t.Cleanup(func() {
		upgradePollInterval, caHandoverFor, dialTalosconfig = interval, handover, dial
		resetFingerprintLineage()
	})
}

// nodeSecurity is the issuing CA (crt, key) and the accepted CAs of node's config.
func nodeSecurity(t *testing.T, f *fakeTalos, node string) (crt, key []byte, accepted [][]byte) {
	t.Helper()

	found := fakeTalosState{f: f}.find(node, config.MachineConfigType, config.ActiveID)
	if len(found) == 0 {
		t.Fatalf("%s has no config", node)
	}

	sec := found[0].(*config.MachineConfig).Provider().Machine().Security()
	if ca := sec.IssuingCA(); ca != nil {
		crt, key = ca.Crt, ca.Key
	}

	for _, ca := range sec.AcceptedCAs() {
		accepted = append(accepted, ca.Crt)
	}

	return crt, key, accepted
}

func TestTalosCARotationRun(t *testing.T) {
	f, cfg, oldCA := caCluster(t)
	fastCARotation(t, cfg)

	_, beforeCtx, err := resolveContext(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}

	before := contextFingerprint("fake", beforeCtx)

	rec := newCARecorder()
	run := StartTalosCARotation(cfg, "fake", rec)

	var transition, final caHandedConfig

	select {
	case transition = <-rec.configs:
	case <-time.After(20 * time.Second):
		t.Fatal("no transition talosconfig")
	}

	if transition.final {
		t.Fatal("the first talosconfig handed over must be the transition one")
	}

	// Before Proceed, no node issues with the new CA yet: the swap waits for the phone.
	for _, n := range []string{caControlPlane1, caControlPlane2, caWorker} {
		if crt, _, accepted := nodeSecurity(t, f, n); !bytes.Equal(crt, oldCA) || len(accepted) != 1 {
			t.Fatalf("%s before the handover: issuing changed or %d accepted", n, len(accepted))
		}
	}

	run.Proceed()

	msg, phases := rec.wait(t)
	if msg != "" {
		t.Fatalf("rotation failed: %s", msg)
	}

	want := []string{caPhaseCheck, caPhaseAccept, caPhaseHandover, caPhaseSwap, caPhaseDrop, caPhaseDone}
	if strings.Join(phases, ",") != strings.Join(want, ",") {
		t.Fatalf("phases = %v", phases)
	}

	select {
	case final = <-rec.configs:
	default:
		t.Fatal("no final talosconfig")
	}

	if !final.final {
		t.Fatal("the second talosconfig must be the final one")
	}

	finalCtx := onlyContext(t, final.yaml)
	newCA := decodeB64(t, finalCtx.CA)

	if bytes.Equal(newCA, oldCA) || bytes.Count(newCA, []byte("BEGIN CERTIFICATE")) != 1 {
		t.Fatal("the final talosconfig must trust the new CA alone")
	}

	if ca := decodeB64(t, onlyContext(t, transition.yaml).CA); !bytes.Contains(ca, oldCA) || !bytes.Contains(ca, newCA) {
		t.Fatal("the transition talosconfig must trust both CAs")
	}

	for _, n := range []string{caControlPlane1, caControlPlane2, caWorker} {
		crt, key, accepted := nodeSecurity(t, f, n)

		if !bytes.Equal(crt, newCA) {
			t.Errorf("%s does not issue with the new CA", n)
		}

		if len(accepted) != 0 {
			t.Errorf("%s still accepts %d CA(s)", n, len(accepted))
		}

		if (n == caWorker) != (len(key) == 0) {
			t.Errorf("%s: a control plane holds the CA key, a worker does not (key %d bytes)", n, len(key))
		}
	}

	// Accept, swap and drop each patched every node once, without a reboot.
	if applies := f.called("ApplyConfiguration"); len(applies) != 9 {
		t.Fatalf("applies = %v", applies)
	}

	// The stored config with the final credentials keeps the cluster's fingerprint.
	stored, err := ReplaceContextCredentials(cfg, final.yaml, "fake")
	if err != nil {
		t.Fatal(err)
	}

	_, afterCtx, err := resolveContext(stored, "fake")
	if err != nil {
		t.Fatal(err)
	}

	if afterCtx.CA == beforeCtx.CA {
		t.Fatal("the stored context must carry the new CA")
	}

	if fp := contextFingerprint("fake", afterCtx); fp != before {
		t.Fatalf("fingerprint changed: %s -> %s", before, fp)
	}

	if id := clusterID(afterCtx); id != clusterID(beforeCtx) {
		t.Fatalf("cluster id changed: %s -> %s", clusterID(beforeCtx), id)
	}
}

func TestTalosCARotationHandoverLate(t *testing.T) {
	f, cfg, oldCA := caCluster(t)
	fastCARotation(t, cfg)
	caHandoverFor = 50 * time.Millisecond

	rec := newCARecorder()
	StartTalosCARotation(cfg, "fake", rec)

	msg, phases := rec.wait(t)
	if !strings.Contains(msg, "not stored in time") || phases[len(phases)-1] != caPhaseHandover {
		t.Fatalf("msg = %q, phases = %v", msg, phases)
	}

	for _, n := range []string{caControlPlane1, caControlPlane2, caWorker} {
		if crt, _, accepted := nodeSecurity(t, f, n); !bytes.Equal(crt, oldCA) || len(accepted) != 1 {
			t.Errorf("%s: the issuing CA must not change without the handover", n)
		}
	}
}

func TestTalosCARotationStopped(t *testing.T) {
	f, cfg, oldCA := caCluster(t)
	fastCARotation(t, cfg)

	rec := newCARecorder()
	run := StartTalosCARotation(cfg, "fake", rec)

	select {
	case <-rec.configs:
	case <-time.After(20 * time.Second):
		t.Fatal("no transition talosconfig")
	}

	run.Cancel()

	msg, _ := rec.wait(t)
	if !strings.HasPrefix(msg, "stopped") {
		t.Fatalf("msg = %q", msg)
	}

	if crt, _, _ := nodeSecurity(t, f, caWorker); !bytes.Equal(crt, oldCA) {
		t.Fatal("a stopped run must not swap the issuing CA")
	}
}

func TestTalosCARotationRefusedWhenNotReady(t *testing.T) {
	f, cfg, _ := caCluster(t)
	fastCARotation(t, cfg)
	cfg = strings.Replace(cfg, "            - "+caWorker+"\n", "            - "+caWorker+"\n            - 192.0.2.99\n", 1)

	rec := newCARecorder()
	StartTalosCARotation(cfg, "fake", rec)

	msg, _ := rec.wait(t)
	if !strings.Contains(msg, "cannot start") || !strings.Contains(msg, "192.0.2.99") {
		t.Fatalf("msg = %q", msg)
	}

	if applies := f.called("ApplyConfiguration"); len(applies) != 0 {
		t.Fatalf("a refused rotation applied %v", applies)
	}
}

func TestTalosCARotationDemo(t *testing.T) {
	rec := newCARecorder()
	StartTalosCARotation(demoConfigForTest(t), "", rec)

	if msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Fatalf("msg = %q", msg)
	}
}

func onlyContext(t *testing.T, yaml string) *clientconfig.Context {
	t.Helper()

	cfg, err := clientconfig.FromString(yaml)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Contexts) != 1 {
		t.Fatalf("%d contexts", len(cfg.Contexts))
	}

	for _, c := range cfg.Contexts {
		return c
	}

	return nil
}

func decodeB64(t *testing.T, s string) []byte {
	t.Helper()

	out, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}

	return out
}
