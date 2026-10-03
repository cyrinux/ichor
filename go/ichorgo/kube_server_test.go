package ichorgo

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeKubeServer(t *testing.T) {
	for input, want := range map[string]string{
		"":                            "",
		"   ":                         "",
		"k8s.example.com":             "https://k8s.example.com",
		" k8s.example.com:16443 ":     "https://k8s.example.com:16443",
		"https://10.0.0.5:6443/":      "https://10.0.0.5:6443",
		"https://proxy.lan/k8s/prod/": "https://proxy.lan/k8s/prod",
		"[fd00::1]:6443":              "https://[fd00::1]:6443",
	} {
		got, err := NormalizeKubeServer(input)
		if err != nil || got != want {
			t.Errorf("NormalizeKubeServer(%q) = %q, %v; want %q", input, got, err, want)
		}
	}

	for _, input := range []string{"http://k8s.lan:6443", "https://", "https://user:pw@k8s.lan", "k8s.lan:6443?x=1", "k8s.lan:99999", "k8s.lan:0", "k8s.lan:", "https://k8s.lan#a"} {
		if got, err := NormalizeKubeServer(input); err == nil {
			t.Errorf("NormalizeKubeServer(%q) = %q, want an error", input, got)
		}
	}
}

func TestApplyKubeServerKeepsPortAndCertificateName(t *testing.T) {
	f := newFakeKubeAPI(t, nil)

	creds, err := parseKubeconfig(f.kubeconfigFor("https://vip.lan:6443"))
	if err != nil {
		t.Fatal(err)
	}

	if err := creds.applyKubeServer("k8s.example.com"); err != nil {
		t.Fatal(err)
	}

	if creds.server.String() != "https://k8s.example.com:6443" || creds.tls.ServerName != "vip.lan" {
		t.Fatalf("server %s, TLS name %q", creds.server, creds.tls.ServerName)
	}
}

func TestOpenKubeClientUsesOnlyTheGivenServer(t *testing.T) {
	f := newFakeKubeAPI(t, nil)
	port := f.URL[strings.LastIndex(f.URL, ":")+1:]

	// The kubeconfig's address does not answer; "localhost" is not in the test certificate,
	// which is still checked against the kubeconfig's host (127.0.0.1).
	k, err := openKubeClient(context.Background(), f.kubeconfigFor("https://127.0.0.1:1"), nil, "localhost:"+port)
	if err != nil {
		t.Fatal(err)
	}
	defer k.close()

	if k.base.Host != "localhost:"+port {
		t.Fatalf("picked %s", k.base)
	}

	// The Talos endpoints are not tried in its place.
	if _, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), []string{"127.0.0.1"}, "unreachable.invalid:1"); err == nil {
		t.Fatal("expected the given server to be the only one tried")
	}

	if _, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "http://plain"); err == nil {
		t.Fatal("expected an invalid server to be refused")
	}
}

func TestWithKubeServerRewritesCurrentCluster(t *testing.T) {
	f := newFakeKubeAPI(t, nil)
	kubeconfig := f.kubeconfigFor("https://vip.lan:6443")

	if out, err := withKubeServer(kubeconfig, " "); err != nil || out != kubeconfig {
		t.Fatalf("a blank server changed the kubeconfig: %v", err)
	}

	out, err := withKubeServer(kubeconfig, "k8s.example.com:16443")
	if err != nil {
		t.Fatal(err)
	}

	creds, err := parseKubeconfig(out)
	if err != nil {
		t.Fatal(err)
	}

	if creds.server.String() != "https://k8s.example.com:16443" || creds.tls.ServerName != "vip.lan" || creds.token != "secret-token" {
		t.Fatalf("server %s, TLS name %q", creds.server, creds.tls.ServerName)
	}

	// The other cluster is left alone.
	if !strings.Contains(out, "https://other.invalid:6443") {
		t.Fatalf("other cluster rewritten:\n%s", out)
	}
}

func TestApplyKubeServerRefusesUnverifiedKubeconfig(t *testing.T) {
	f := newFakeKubeAPI(t, nil)
	cfg := strings.Replace(f.kubeconfigFor("https://vip.lan:6443"), "    server: https://vip.lan", "    insecure-skip-tls-verify: true\n    server: https://vip.lan", 1)

	creds, err := parseKubeconfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := creds.applyKubeServer("k8s.example.com"); err == nil {
		t.Fatal("expected an unverified kubeconfig to refuse another address")
	}
}
