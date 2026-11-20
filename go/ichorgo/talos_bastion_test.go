package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/internal/httpx"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// talosBastionFixture is a fake Talos cluster (one control plane, serving a kubeconfig whose
// API server only the bastion resolves) and an in-process bastion, not set yet.
type talosBastionFixture struct {
	config  string
	key     string
	bastion *testBastion
}

const talosBastionNode = "192.0.2.51"

func newTalosBastionFixture(t *testing.T) *talosBastionFixture {
	t.Helper()

	withAuthStore(t)
	withDataDir(t)

	f := newFakeTalos()
	f.addNode(t, talosBastionNode, "v1.11.0", machine.TypeControlPlane)

	api, _ := bastionAPI(t)
	f.mu.Lock()
	f.kubeconfigs = map[string][]byte{talosBastionNode: []byte(bastionKubeconfig(api, nil, ""))}
	f.mu.Unlock()

	config := f.start(t, talosBastionNode)
	key, pk := testClientKey(t)

	t.Cleanup(func() {
		kubeClients.forgetConfig(config, "fake")
		sessions.forgetAuth(mustTalosBastionKey(t, config))
	})

	return &talosBastionFixture{config: config, key: key, bastion: newTestBastion(t, pk)}
}

func mustTalosBastionKey(t *testing.T, config string) string {
	t.Helper()

	return mustBastionKeyOf(t, config, "fake")
}

func mustBastionKeyOf(t *testing.T, config, contextName string) string {
	t.Helper()

	target, err := bastionContext(config, contextName)
	if err != nil || target.key == "" {
		t.Fatalf("bastion key: %+v, %v", target, err)
	}

	return target.key
}

// set stores the bastion with scope ("" for the default) and returns the info JSON.
func (f *talosBastionFixture) set(t *testing.T, scope string) kubeBastionInfo {
	t.Helper()

	fields := map[string]string{"host": f.bastion.addr(), "user": "jump", "privateKey": f.key, "hostKey": f.bastion.pin()}
	if scope != "" {
		fields["scope"] = scope
	}

	out, err := KubeSetBastion(f.config, "fake", setBastionJSON(t, fields))
	if err != nil {
		t.Fatalf("KubeSetBastion: %v", err)
	}

	var info kubeBastionInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatal(err)
	}

	return info
}

// talosRead is a Talos API call to the node through the context's session.
func (f *talosBastionFixture) talosRead() error {
	_, err := withSessionFrom(context.Background(), f.config, "fake", callTimeout, func(ctx context.Context, s *session) (string, error) {
		_, err := s.client.Version(client.WithNode(ctx, talosBastionNode))

		return "", err
	})

	return err
}

func TestTalosBastionScopeAllCarriesTalosAndKubernetes(t *testing.T) {
	f := newTalosBastionFixture(t)

	info := f.set(t, "")
	if !info.Configured || info.Scope != bastionScopeAll {
		t.Fatalf("info = %+v", info)
	}

	// The check opened one channel to the first endpoint.
	checked := f.bastion.forwards.Load()
	if checked != 1 {
		t.Fatalf("the check forwarded %d channels, want 1", checked)
	}

	if err := f.talosRead(); err != nil {
		t.Fatalf("Talos call through the bastion: %v", err)
	}

	afterTalos := f.bastion.forwards.Load()
	if afterTalos <= checked {
		t.Fatal("the Talos call did not go through the bastion")
	}

	// The API server is only reachable through the bastion (api.internal.test).
	if _, err := KubeNamespaces(f.config, "fake", ""); err != nil {
		t.Fatalf("Kubernetes call through the bastion: %v", err)
	}

	if f.bastion.forwards.Load() <= afterTalos {
		t.Error("the Kubernetes call did not go through the bastion")
	}

	readInfo, err := KubeBastionInfo(f.config, "fake")
	if err != nil || !strings.Contains(readInfo, `"scope":"all"`) || !strings.Contains(readInfo, `"configured":true`) {
		t.Errorf("KubeBastionInfo = %s, %v", readInfo, err)
	}
}

func TestTalosBastionScopeKubeLeavesTalosDirect(t *testing.T) {
	f := newTalosBastionFixture(t)

	if info := f.set(t, bastionScopeKube); info.Scope != bastionScopeKube {
		t.Fatalf("info = %+v", info)
	}

	// Scope kube checks the sign-in only: no channel.
	if n := f.bastion.forwards.Load(); n != 0 || f.bastion.handshakes.Load() != 1 {
		t.Fatalf("the check forwarded %d channels in %d handshakes", n, f.bastion.handshakes.Load())
	}

	if err := f.talosRead(); err != nil {
		t.Fatal(err)
	}

	if n := f.bastion.forwards.Load(); n != 0 {
		t.Fatalf("a Talos call went through the bastion with scope kube (%d channels)", n)
	}

	if _, err := KubeNamespaces(f.config, "fake", ""); err != nil {
		t.Fatalf("Kubernetes call through the bastion: %v", err)
	}

	if f.bastion.forwards.Load() == 0 {
		t.Error("the Kubernetes call did not go through the bastion")
	}

	// An edit without a scope keeps the stored one.
	if info := f.set(t, ""); info.Scope != bastionScopeKube {
		t.Errorf("scope after an edit = %q", info.Scope)
	}
}

func TestTalosBastionChangedHostKeyStopsTalosCalls(t *testing.T) {
	f := newTalosBastionFixture(t)
	f.set(t, "")

	if err := f.talosRead(); err != nil {
		t.Fatal(err)
	}

	// A new session (after idle, or a restart) meets the new key.
	sessions.forgetAuth(mustTalosBastionKey(t, f.config))
	f.bastion.rotateHostKey(t)

	err := f.talosRead()
	if err == nil || !strings.Contains(err.Error(), httpx.BastionCodeHostKey) {
		t.Fatalf("Talos call after the host key changed: %v", err)
	}

	kubeClients.forgetConfig(f.config, "fake")

	if _, err := KubeNamespaces(f.config, "fake", ""); err == nil {
		t.Fatal("Kubernetes call after the host key changed: no error")
	}

	// Removing the bastion restores direct dialling.
	if err := KubeRemoveBastion(f.config, "fake"); err != nil {
		t.Fatal(err)
	}

	if err := f.talosRead(); err != nil {
		t.Fatalf("Talos call after removing the bastion: %v", err)
	}
}

func TestTalosSessionCloseClosesItsHop(t *testing.T) {
	f := newTalosBastionFixture(t)
	f.set(t, "")

	s, err := openSession(f.config, "fake")
	if err != nil {
		t.Fatal(err)
	}

	if s.bastion == nil || s.bastionKey != mustTalosBastionKey(t, f.config) {
		t.Fatalf("session bastion = %v, key %q", s.bastion, s.bastionKey)
	}

	s.Close()

	if _, err := s.bastion.DialContext(context.Background(), "tcp", "talos.internal.test:50000"); err == nil {
		t.Error("the hop still dials after the session closed")
	}
}

func TestTalosBastionRefusedOnDemoAndOmni(t *testing.T) {
	withAuthStore(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	omni := omniTalosconfigYAML("ops@example.com", "https://omni.example.net", "")

	for name, tc := range map[string]struct{ config, context, want string }{
		"demo": {demo, "", bastionUnavailableDemo},
		"omni": {omni, "acme-demo", bastionUnavailableOmni},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := KubeSetBastion(tc.config, tc.context, `{"host":"bastion.example.net","user":"jump","password":"pw","hostKey":"x"}`)
			if err == nil || !strings.HasPrefix(err.Error(), KubeBastionUnavailable+":") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("KubeSetBastion: %v", err)
			}

			info, err := KubeBastionInfo(tc.config, tc.context)
			if err != nil || !strings.Contains(info, `"unavailable":"`+tc.want+`"`) {
				t.Fatalf("info %s, %v", info, err)
			}
		})
	}
}

func TestTalosBastionScopeRefusesOtherValues(t *testing.T) {
	f := newTalosBastionFixture(t)

	fields := map[string]string{
		"host": f.bastion.addr(), "user": "jump", "privateKey": f.key, "hostKey": f.bastion.pin(), "scope": "talos",
	}
	if _, err := KubeSetBastion(f.config, "fake", setBastionJSON(t, fields)); err == nil {
		t.Error("an unknown scope was accepted")
	}
}

func TestTalosSummaryCarriesTheBastionKey(t *testing.T) {
	in, err := generate.NewInput("summary-test", "https://192.0.2.10:6443", "1.34.0")
	if err != nil {
		t.Fatal(err)
	}

	tc, err := in.Talosconfig()
	if err != nil {
		t.Fatal(err)
	}

	raw, err := tc.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	config := string(raw)

	out, err := ParseConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"bastionKey":"`+mustBastionKeyOf(t, config, "summary-test")+`"`) {
		t.Errorf("summary = %s", out)
	}

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if out, _ := ParseConfig(demo); strings.Contains(out, `"bastionKey"`) {
		t.Errorf("demo summary carries a bastion key: %s", out)
	}
}

func TestTalosEndpointAddress(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.5":                  "192.0.2.5:50000",
		"192.0.2.5:50001":            "192.0.2.5:50001",
		"cp.example.net":             "cp.example.net:50000",
		"https://cp.example.net:443": "cp.example.net:443",
		"2001:db8::1":                "[2001:db8::1]:50000",
		"[2001:db8::1]:50000":        "[2001:db8::1]:50000",
	} {
		if got := talosEndpointAddress(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}
