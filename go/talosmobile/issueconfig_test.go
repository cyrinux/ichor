package talosmobile

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// testPEMIdentity returns raw PEM ca/crt/key (as GenerateClientConfiguration does) for roles.
func testPEMIdentity(t *testing.T, roles []string, notAfter time.Time) (ca, crt, key []byte) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{Organization: roles},
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

	crt = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	key = pem.EncodeToMemory(&pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: pkcs8})

	return crt, crt, key
}

func TestParseRoles(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{in: "os:reader", want: []string{"os:reader"}},
		{in: " os:reader , os:etcd:backup,os:reader", want: []string{"os:reader", "os:etcd:backup"}},
		{in: "os:admin,", want: []string{"os:admin"}},
		{in: "", wantErr: true},
		{in: " , ", wantErr: true},
		{in: "os:root", wantErr: true},
		{in: "os:reader,admin", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseRoles(tt.in)
		if (err != nil) != tt.wantErr || !slices.Equal(got, tt.want) {
			t.Errorf("parseRoles(%q) = %v, %v", tt.in, got, err)
		}
	}
}

func TestGenerateTalosconfigValidation(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))

	tests := []struct {
		roles string
		ttl   int
		want  string
	}{
		{roles: "os:superuser", ttl: 1, want: "unknown role"},
		{roles: "", ttl: 1, want: "no role"},
		{roles: "os:reader", ttl: 0, want: "between 1 and"},
		{roles: "os:reader", ttl: 87601, want: "between 1 and"},
	}

	for _, tt := range tests {
		// Validation happens before any connection is opened.
		_, err := GenerateTalosconfig(cfg, "lab", tt.roles, tt.ttl)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("GenerateTalosconfig(%q, %d) err = %v, want %q", tt.roles, tt.ttl, err, tt.want)
		}
	}
}

func TestBuildTalosconfig(t *testing.T) {
	notAfter := time.Now().Add(365 * 24 * time.Hour).UTC().Truncate(time.Second)
	ca, crt, key := testPEMIdentity(t, []string{"os:reader"}, notAfter)
	src := &clientconfig.Context{
		Endpoints: []string{"10.0.0.1", "talos.example.org"},
		Nodes:     []string{"10.0.0.2", "10.0.0.3"},
		CA:        "old-ca", Crt: "old-crt", Key: "old-key",
	}

	out, err := buildTalosconfig("lab", src, ca, crt, key)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := ParseConfig(out)
	if err != nil {
		t.Fatalf("ParseConfig rejected the generated config: %v\n%s", err, out)
	}

	var got configSummary
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}

	want := configSummary{
		Current: "lab",
		Contexts: []contextSummary{{
			Name:         "lab",
			Fingerprint:  got.Contexts[0].Fingerprint, // covered by TestContextFingerprint
			Endpoints:    src.Endpoints,
			Nodes:        src.Nodes,
			Roles:        []string{"os:reader"},
			CertNotAfter: notAfter.Unix(),
		}},
	}

	if !equalJSON(t, got, want) {
		t.Error("unexpected summary")
	}

	if strings.Contains(out, "old-") {
		t.Error("old credentials leaked into the generated config")
	}

	if _, err := buildTalosconfig("lab", src, ca, nil, key); err == nil {
		t.Error("missing certificate accepted")
	}
}
