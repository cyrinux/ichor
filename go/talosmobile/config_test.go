package talosmobile

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// testIdentity returns base64 PEM ca/crt/key using an Ed25519 key, like talosctl generates.
func testIdentity(t *testing.T, notAfter time.Time) (ca, crt, key string) {
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
		"no endpoints": `context: a
contexts:
  a:
    ca: Zm9v
    crt: Zm9v
    key: Zm9v
`,
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
