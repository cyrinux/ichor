package ichorgo

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// testIdentity returns base64 PEM ca/crt/key using an Ed25519 key, like talosctl generates.
func testIdentity(t testing.TB, notAfter time.Time) (ca, crt, key string) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"os:admin"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}

	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	crtPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: pkcs8})
	enc := base64.StdEncoding.EncodeToString

	return enc(crtPEM), enc(crtPEM), enc(keyPEM)
}

func testConfig(t *testing.T, notAfter time.Time) string {
	ca, crt, key := testIdentity(t, notAfter)

	return fmt.Sprintf(`context: lab
contexts:
  lab:
    endpoints:
      - 10.0.0.1
      - talos.example.org
    nodes:
      - 10.0.0.2
      - 10.0.0.3
    ca: %s
    crt: %s
    key: %s
  other:
    endpoints:
      - 10.1.0.1
    ca: %s
    crt: %s
    key: %s
`, ca, crt, key, ca, crt, key)
}

func TestParseConfigSummary(t *testing.T) {
	notAfter := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	cfg := testConfig(t, notAfter)

	out, err := ParseConfig(cfg)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	var got configSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if got.Current != "lab" {
		t.Errorf("current = %q, want lab", got.Current)
	}

	if len(got.Contexts) != 2 || got.Contexts[0].Name != "lab" || got.Contexts[1].Name != "other" {
		t.Fatalf("contexts not sorted/complete: %+v", got.Contexts)
	}

	lab := got.Contexts[0]
	if strings.Join(lab.Endpoints, ",") != "10.0.0.1,talos.example.org" {
		t.Errorf("endpoints = %v", lab.Endpoints)
	}

	if strings.Join(lab.Nodes, ",") != "10.0.0.2,10.0.0.3" {
		t.Errorf("nodes = %v", lab.Nodes)
	}

	if lab.CertNotAfter != notAfter.Unix() {
		t.Errorf("certNotAfter = %d, want %d", lab.CertNotAfter, notAfter.Unix())
	}

	if strings.Join(lab.Roles, ",") != "os:admin" {
		t.Errorf("roles = %v", lab.Roles)
	}
}

func TestParseConfigNeverLeaksKeyMaterial(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))

	out, err := ParseConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, needle := range []string{"PRIVATE", "CERTIFICATE", "LS0t"} { // LS0t = base64("---")
		if strings.Contains(out, needle) {
			t.Errorf("summary contains %q", needle)
		}
	}
}

func TestParseConfigErrors(t *testing.T) {
	cases := map[string]string{
		"empty":       "",
		"not yaml":    "{{{",
		"no contexts": "context: x\ncontexts: {}\n",
		"bad cert": `context: a
contexts:
  a:
    endpoints: [1.2.3.4]
    ca: Zm9v
    crt: Zm9v
    key: Zm9v
`,
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig(in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// A talosconfig generated before the cluster had addresses (talosctl gen secrets/config
// without --endpoints) lists none: it imports, and the app then asks for one.
func TestParseConfigWithoutEndpoints(t *testing.T) {
	ca, crt, key := testIdentity(t, time.Now().Add(time.Hour))
	cfg := fmt.Sprintf("context: lab\ncontexts:\n    lab:\n        endpoints: []\n        ca: %s\n        crt: %s\n        key: %s\n", ca, crt, key)

	out, err := ParseConfig(cfg)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	var got configSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Contexts) != 1 || len(got.Contexts[0].Endpoints) != 0 || len(got.Contexts[0].Nodes) != 0 {
		t.Fatalf("contexts = %+v", got.Contexts)
	}

	if _, err := openSession(cfg, "lab"); !errors.Is(err, errNoEndpoints) {
		t.Errorf("openSession err = %v, want errNoEndpoints", err)
	}

	added, err := AddContextEndpoint(cfg, "lab", "10.0.0.5")
	if err != nil {
		t.Fatalf("AddContextEndpoint: %v", err)
	}

	_, ctx, err := resolveContext(added, "lab")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(ctx.Endpoints, ",") != "10.0.0.5" || strings.Join(ctx.Nodes, ",") != "10.0.0.5" {
		t.Errorf("after adding: endpoints %v, nodes %v", ctx.Endpoints, ctx.Nodes)
	}
}

func TestParseConfigCurrentContextMissingFallsBack(t *testing.T) {
	cfg := strings.Replace(testConfig(t, time.Now().Add(time.Hour)), "context: lab", "context: gone", 1)

	out, err := ParseConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var got configSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if got.Current != "lab" {
		t.Errorf("current = %q, want first context lab", got.Current)
	}
}

func TestResolveContext(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))

	if _, _, err := resolveContext(cfg, "nope"); err == nil {
		t.Error("expected error for unknown context")
	}

	name, ctx, err := resolveContext(cfg, "")
	if err != nil || name != "lab" || len(ctx.Nodes) != 2 {
		t.Errorf("default context: name=%q nodes=%v err=%v", name, ctx, err)
	}

	// A context without nodes falls back to its endpoints as nodes.
	_, other, err := resolveContext(cfg, "other")
	if err != nil {
		t.Fatal(err)
	}

	if nodes := targetNodes(other); strings.Join(nodes, ",") != "10.1.0.1" {
		t.Errorf("targetNodes = %v", nodes)
	}
}

func TestTargetNodesDedupes(t *testing.T) {
	ctx := &clientconfig.Context{Nodes: []string{"a", "b", "a", "c", "b"}}

	if got := strings.Join(targetNodes(ctx), ","); got != "a,b,c" {
		t.Errorf("targetNodes = %s", got)
	}
}

func TestConfigDigestIsTheCacheKey(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))

	digest := ConfigDigest(cfg) // not cached yet: hashed directly
	if digest != configs.get(cfg).hash || ConfigDigest(strings.Clone(cfg)) != digest {
		t.Fatal("digest differs from the config cache's key")
	}

	if len(digest) != 64 || ConfigDigest(cfg+" ") == digest {
		t.Fatalf("digest %q is not a content hash", digest)
	}
}

func TestParseConfigIfChanged(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))
	digest := ConfigDigest(cfg)
	before := configParses.Load()

	if out, err := ParseConfigIfChanged(cfg, digest); err != nil || out != "" {
		t.Fatalf("known digest: %q, %v; want \"\"", out, err)
	}

	if got := configParses.Load() - before; got != 0 {
		t.Fatalf("parsed %d times on a known digest", got)
	}

	want, err := ParseConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, known := range []string{"", "stale"} {
		if out, err := ParseConfigIfChanged(strings.Clone(cfg), known); err != nil || out != want {
			t.Fatalf("digest %q: %q, %v; want the summary", known, out, err)
		}
	}

	if _, err := ParseConfigIfChanged("", "stale"); err == nil {
		t.Fatal("empty config accepted")
	}
}

func TestParseKubeconfigIfChanged(t *testing.T) {
	kube := testKubeconfigAllAuth(t)
	before := kubeconfigParses.Load()

	if out, err := ParseKubeconfigIfChanged(kube, ConfigDigest(kube)); err != nil || out != "" {
		t.Fatalf("known digest: %q, %v; want \"\"", out, err)
	}

	if got := kubeconfigParses.Load() - before; got != 0 {
		t.Fatalf("parsed %d times on a known digest", got)
	}

	want, err := ParseKubeconfig(kube)
	if err != nil {
		t.Fatal(err)
	}

	if out, err := ParseKubeconfigIfChanged(kube, "stale"); err != nil || out != want {
		t.Fatalf("stale digest: %q, %v; want the summary", out, err)
	}
}

func TestParseConfigIfChangedIsMasked(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))
	enableMask(t, "")

	out, err := ParseConfigIfChanged(cfg, "stale")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "example.org") || strings.Contains(out, "10.1.0.1") {
		t.Fatalf("summary not masked: %s", out)
	}

	if want, _ := ParseConfig(cfg); out != want {
		t.Fatalf("masked differently from ParseConfig:\n%s\n%s", out, want)
	}
}

func TestPrivacyGenerationFollowsTheMask(t *testing.T) {
	t.Cleanup(func() { SetPrivacyMask(false, "") })
	SetPrivacyMask(false, "")

	if got := PrivacyGeneration(); got != 0 {
		t.Fatalf("mask off: generation %d, want 0", got)
	}

	SetPrivacyMask(true, "")

	on := PrivacyGeneration()
	if on == 0 {
		t.Fatal("mask on: generation 0")
	}

	if SetPrivacyMask(true, ""); PrivacyGeneration() != on {
		t.Fatal("same mask: generation changed")
	}

	if SetPrivacyMask(true, "word"); PrivacyGeneration() == on {
		t.Fatal("new words: generation kept")
	}

	words := PrivacyGeneration()

	if SetPrivacyMask(false, ""); PrivacyGeneration() != 0 {
		t.Fatal("mask off again: generation not 0")
	}

	if SetPrivacyMask(true, "word"); PrivacyGeneration() == on || PrivacyGeneration() == words {
		t.Fatal("mask on again: an earlier generation came back")
	}
}
