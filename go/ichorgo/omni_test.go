package ichorgo

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/go-api-signature/pkg/message"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"github.com/siderolabs/go-api-signature/pkg/serviceaccount"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// omniTalosconfigYAML is a talosconfig like omnictl's, for an invented instance.
func omniTalosconfigYAML(identity, endpoint, ca string) string {
	caLine := ""
	if ca != "" {
		caLine = "\n        ca: " + ca
	}

	return fmt.Sprintf(`context: acme-demo
contexts:
    acme-demo:
        endpoints:
            - %s%s
        auth:
            siderov1:
                identity: %s
        cluster: demo
`, endpoint, caLine, identity)
}

const (
	testOmniEndpoint = "https://acme.eu-central-1.omni.example.com"
	testSAIdentity   = "ichor-test@serviceaccount.omni.sidero.dev"
	testUserIdentity = "someone@example.com"
)

func useMemAuthStore(t *testing.T) {
	t.Helper()
	SetAuthStore(nil)
	t.Cleanup(func() { SetAuthStore(nil) })
}

func testServiceAccountKey(t *testing.T, lifetime time.Duration) (string, *pgp.Key) {
	t.Helper()

	key, err := pgp.GenerateKey("ichor-test", "test", testSAIdentity, lifetime)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := serviceaccount.Encode("ichor-test", key)
	if err != nil {
		t.Fatal(err)
	}

	return encoded, key
}

func TestParseConfigOmniContext(t *testing.T) {
	out, err := ParseConfig(omniTalosconfigYAML(testSAIdentity, testOmniEndpoint, ""))
	if err != nil {
		t.Fatal(err)
	}

	var summary configSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	got := summary.Contexts[0]
	if !got.Omni || got.Identity != testSAIdentity || got.Cluster != "demo" || got.CertNotAfter != 0 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestOmniContextTargetsNoEndpoint(t *testing.T) {
	_, ctx, err := resolveContext(omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, ""), "")
	if err != nil {
		t.Fatal(err)
	}

	if nodes := targetNodes(ctx); len(nodes) != 0 {
		t.Fatalf("targetNodes = %v, want none: the Omni endpoint is no node", nodes)
	}
}

func TestOmniClusterIdentity(t *testing.T) {
	_, a, _ := resolveContext(omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, ""), "")
	_, b, _ := resolveContext(omniTalosconfigYAML(testSAIdentity, testOmniEndpoint, ""), "")
	_, other, _ := resolveContext(strings.Replace(omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, ""), "cluster: demo", "cluster: prod", 1), "")

	if !sameCluster(a, b) || clusterID(a) != clusterID(b) {
		t.Fatal("two identities on the same Omni cluster are the same cluster")
	}

	if sameCluster(a, other) || clusterID(a) == clusterID(other) {
		t.Fatal("two Omni clusters must not be the same cluster")
	}
}

func TestTalosSignInInfo(t *testing.T) {
	useMemAuthStore(t)

	for _, tc := range []struct {
		identity, kind, method string
	}{
		{testSAIdentity, "credentials", omniServiceAccountMethod},
		{testUserIdentity, "browser", omniUserMethod},
	} {
		out, err := TalosSignInInfo(omniTalosconfigYAML(tc.identity, testOmniEndpoint, ""), "acme-demo")
		if err != nil {
			t.Fatal(err)
		}

		var info kubeSignInKind
		if err := json.Unmarshal([]byte(out), &info); err != nil {
			t.Fatal(err)
		}

		if info.Kind != tc.kind || info.Method != tc.method || info.SignedIn {
			t.Fatalf("%s: info = %+v", tc.identity, info)
		}
	}

	out, err := TalosSignInInfo(testConfig(t, time.Now().Add(time.Hour)), "")
	if err != nil || out != "" {
		t.Fatalf("a certificate context has no sign-in: %q, %v", out, err)
	}
}

func TestDecodeServiceAccount(t *testing.T) {
	encoded, key := testServiceAccountKey(t, time.Hour)

	for _, value := range []string{encoded, "OMNI_SERVICE_ACCOUNT_KEY=" + encoded, "  " + encoded + "\n"} {
		signer, err := decodeServiceAccount(value)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}

		if signer.identity != "ichor-test" || signer.key.Fingerprint() != key.Fingerprint() {
			t.Fatalf("signer = %+v", signer)
		}
	}

	if _, err := decodeServiceAccount("bm90IGEga2V5"); err == nil {
		t.Fatal("garbage must not decode")
	}
}

func TestOmniSessionNeedsSignIn(t *testing.T) {
	useMemAuthStore(t)

	_, err := openSession(omniTalosconfigYAML(testSAIdentity, testOmniEndpoint, ""), "")

	var signIn *errSignInRequired
	if !errors.As(err, &signIn) || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("err = %v, want a sign-in request", err)
	}
}

func TestOmniExpiredUserKeyNeedsSignIn(t *testing.T) {
	useMemAuthStore(t)

	cfg := omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, "")
	_, ctx, _ := resolveContext(cfg, "")

	key, err := pgp.GenerateKey("ichor", "test", testUserIdentity, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	armored, _ := key.Armor()
	kubeAuth.save(omniAuthKey(ctx), kubeAuthState{Method: omniUserMethod, Session: map[string]string{omniKeySession: armored}})

	time.Sleep(2 * time.Second)

	if _, err := loadOmniSigner(omniAuthKey(ctx), ctx); !strings.HasPrefix(fmt.Sprint(err), KubeSignInRequired) {
		t.Fatalf("err = %v, want a sign-in request", err)
	}
}

// fakeOmni is the Talos API behind Omni: it checks each call's signature like Omni does.
type fakeOmni struct {
	machineapi.UnimplementedMachineServiceServer

	key *pgp.Key
}

func (f *fakeOmni) Version(ctx context.Context, _ *emptypb.Empty) (*machineapi.VersionResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	if err := message.NewGRPC(md, "/machine.MachineService/Version").VerifySignature(f.key); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	if got := md.Get("context"); len(got) != 1 || got[0] != "demo" {
		return nil, status.Errorf(codes.InvalidArgument, "cluster = %v", got)
	}

	return &machineapi.VersionResponse{Messages: []*machineapi.Version{{Version: &machineapi.VersionInfo{Tag: "v1.11.0"}}}}, nil
}

func TestOmniSessionSignsCalls(t *testing.T) {
	useMemAuthStore(t)

	encoded, key := testServiceAccountKey(t, time.Hour)
	endpoint, caB64 := startFakeOmni(t, &fakeOmni{key: key})
	cfg := omniTalosconfigYAML(testSAIdentity, endpoint, caB64)

	_, ctx, _ := resolveContext(cfg, "")
	kubeAuth.save(omniAuthKey(ctx), kubeAuthState{Method: omniServiceAccountMethod, Secrets: map[string]string{omniServiceAccountField: encoded}})

	s, err := openSession(cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := s.client.Version(callCtx)
	if err != nil {
		t.Fatalf("signed call refused: %v", err)
	}

	if tag := resp.GetMessages()[0].GetVersion().GetTag(); tag != "v1.11.0" {
		t.Fatalf("tag = %q", tag)
	}
}

func TestOmniWrongKeyNeedsSignIn(t *testing.T) {
	useMemAuthStore(t)

	_, serverKey := testServiceAccountKey(t, time.Hour)
	encoded, _ := testServiceAccountKey(t, time.Hour)
	endpoint, caB64 := startFakeOmni(t, &fakeOmni{key: serverKey})
	cfg := omniTalosconfigYAML(testSAIdentity, endpoint, caB64)

	_, ctx, _ := resolveContext(cfg, "")
	kubeAuth.save(omniAuthKey(ctx), kubeAuthState{Method: omniServiceAccountMethod, Secrets: map[string]string{omniServiceAccountField: encoded}})

	s, err := openSession(cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.client.Version(callCtx); !strings.HasPrefix(fmt.Sprint(err), KubeSignInRequired) {
		t.Fatalf("err = %v, want a sign-in request", err)
	}
}

func TestForgetAuthDropsSessions(t *testing.T) {
	closed := 0
	cache := newSessionCache(time.Hour, func(_, contextName string) (*session, error) {
		return &session{authKey: contextName, onClose: func() { closed++ }}, nil
	})

	_, releaseA, _ := cache.acquire("cfg", "a")
	_, releaseB, _ := cache.acquire("cfg", "b")

	releaseA()
	cache.forgetAuth("a")

	if closed != 1 {
		t.Fatalf("closed = %d, want the idle session closed", closed)
	}

	cache.forgetAuth("b")

	if closed != 1 {
		t.Fatal("a session in use must stay open until released")
	}

	releaseB()

	if closed != 2 {
		t.Fatalf("closed = %d, want the dropped session closed by its last user", closed)
	}
}

// startFakeOmni serves srv over TLS on loopback; it returns the endpoint and the base64 CA.
func startFakeOmni(t *testing.T, srv machineapi.MachineServiceServer) (endpoint, caB64 string) {
	t.Helper()

	return startTLSServer(t, func(s *grpc.Server) { machineapi.RegisterMachineServiceServer(s, srv) })
}

// startTLSServer serves a gRPC server over TLS on loopback (a certificate for 127.0.0.1);
// it returns the endpoint and the base64 CA.
func startTLSServer(t *testing.T, register func(*grpc.Server), opts ...grpc.ServerOption) (endpoint, caB64 string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "omni"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	pair, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12})))
	server := grpc.NewServer(opts...)
	register(server)

	go server.Serve(lis) //nolint:errcheck

	t.Cleanup(server.Stop)

	return lis.Addr().String(), base64.StdEncoding.EncodeToString(certPEM)
}
