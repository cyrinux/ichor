package ichorgo

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	authpb "github.com/siderolabs/go-api-signature/api/auth"
	"github.com/siderolabs/go-api-signature/pkg/message"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"github.com/siderolabs/go-api-signature/pkg/serviceaccount"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

func useMemAuthStore(t *testing.T) *memAuthStore {
	t.Helper()

	store := newMemAuthStore()
	SetAuthStore(store)
	t.Cleanup(func() { SetAuthStore(nil) })

	return store
}

func testServiceAccount(t *testing.T, name string, lifetime time.Duration) (string, *pgp.Key) {
	t.Helper()

	key, err := pgp.GenerateKey(name, "test", name+"@serviceaccount.omni.sidero.dev", lifetime)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := serviceaccount.Encode(name, key)
	if err != nil {
		t.Fatal(err)
	}

	return encoded, key
}

func omniInfo(t *testing.T, yaml string) omniSignInInfo {
	t.Helper()

	out, err := OmniSignInInfo(yaml, "prod")
	if err != nil {
		t.Fatal(err)
	}

	var info omniSignInInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatal(err)
	}

	return info
}

func TestOmniServiceAccount(t *testing.T) {
	store := useMemAuthStore(t)
	key, _ := testServiceAccount(t, "ichor-ci", time.Hour)

	if info := omniInfo(t, omniConfig); info.SignedIn || info.Instance != "https://acme.omni.example.com" || info.Identity != "ops@example.com" {
		t.Fatalf("info before = %+v", info)
	}

	// Pasted keys come wrapped.
	if err := OmniSetServiceAccount(omniConfig, "prod", key[:20]+"\n"+key[20:]+"\n"); err != nil {
		t.Fatal(err)
	}

	info := omniInfo(t, omniConfig)
	if !info.SignedIn || info.Method != omniMethodServiceAccount || info.User != "ichor-ci" {
		t.Fatalf("info after = %+v", info)
	}

	stored := store.Load(contextFingerprintOf(t, omniConfig, "prod"))

	backup, err := KubeAuthForBackup(stored)
	if err != nil || !strings.Contains(backup, key) {
		t.Fatalf("a service account goes into backups: %q, %v", backup, err)
	}

	if err := OmniSignOut(omniConfig, "prod"); err != nil {
		t.Fatal(err)
	}

	if omniInfo(t, omniConfig).SignedIn {
		t.Fatal("still signed in after signing out")
	}
}

func TestOmniServiceAccountErrors(t *testing.T) {
	useMemAuthStore(t)

	for name, key := range map[string]string{
		"garbage":  "not base64!",
		"not json": "aGVsbG8=",
		"no key":   "eyJuYW1lIjoieCJ9",
	} {
		if err := OmniSetServiceAccount(omniConfig, "prod", key); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	key, _ := testServiceAccount(t, "ichor-ci", time.Hour)
	if err := OmniSetServiceAccount(testConfig(t, time.Now().Add(time.Hour)), "lab", key); err == nil {
		t.Fatal("a direct cluster has no Omni sign-in")
	}
}

func contextFingerprintOf(t *testing.T, yaml, name string) string {
	t.Helper()

	_, ctx, err := resolveContext(yaml, name)
	if err != nil {
		t.Fatal(err)
	}

	return contextFingerprint(name, ctx)
}

func TestOpenSessionOmniNeedsSignIn(t *testing.T) {
	useMemAuthStore(t)

	_, err := openSession(omniConfig, "prod")
	if err == nil || !strings.HasPrefix(err.Error(), OmniSignInRequired) {
		t.Fatalf("err = %v", err)
	}

	key, _ := testServiceAccount(t, "ichor-ci", time.Hour)
	if err := OmniSetServiceAccount(omniConfig, "prod", key); err != nil {
		t.Fatal(err)
	}

	s, err := openSession(omniConfig, "prod")
	if err != nil {
		t.Fatal(err)
	}

	defer s.Close()

	if got := s.client.GetClusterName(); got != "prod-eu" {
		t.Fatalf("cluster = %q, want the Omni cluster", got)
	}
}

func TestTargetNodesOmniHasNoEndpointFallback(t *testing.T) {
	_, ctx, err := resolveContext(omniConfig, "prod")
	if err != nil {
		t.Fatal(err)
	}

	if nodes := targetNodes(ctx); len(nodes) != 0 {
		t.Fatalf("the Omni instance is not a node: %v", nodes)
	}
}

// fakeOmni is Omni's auth API, recording the signed metadata of every call it gets.
type fakeOmni struct {
	authpb.UnimplementedAuthServiceServer

	mu        sync.Mutex
	publicKey string
	identity  string
	confirmed chan struct{}
	signed    []metadata.MD
}

func (f *fakeOmni) RegisterPublicKey(_ context.Context, req *authpb.RegisterPublicKeyRequest) (*authpb.RegisterPublicKeyResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.publicKey, f.identity = string(req.GetPublicKey().GetPgpData()), req.GetIdentity().GetEmail()

	return &authpb.RegisterPublicKeyResponse{LoginUrl: "https://acme.omni.example.com/authenticate?public-key-id=x"}, nil
}

func (f *fakeOmni) AwaitPublicKeyConfirmation(ctx context.Context, _ *authpb.AwaitPublicKeyConfirmationRequest) (*emptypb.Empty, error) {
	select {
	case <-f.confirmed:
		return &emptypb.Empty{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeOmni) RevokePublicKey(ctx context.Context, _ *authpb.RevokePublicKeyRequest) (*emptypb.Empty, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	f.mu.Lock()
	f.signed = append(f.signed, md)
	f.mu.Unlock()

	return &emptypb.Empty{}, nil
}

// serveFakeOmni starts fake on an in-memory listener and points dialOmni at it.
func serveFakeOmni(t *testing.T, fake *fakeOmni) {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	authpb.RegisterAuthServiceServer(srv, fake)

	go srv.Serve(lis) //nolint:errcheck

	t.Cleanup(srv.Stop)

	previous := dialOmni
	dialOmni = func(string) (*grpc.ClientConn, error) { return dialBufconn(lis) }

	t.Cleanup(func() { dialOmni = previous })
}

func dialBufconn(lis *bufconn.Listener, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.NewClient("passthrough:///omni",
		append(opts,
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))...)
}

func TestOmniBrowserSignIn(t *testing.T) {
	useMemAuthStore(t)

	fake := &fakeOmni{confirmed: make(chan struct{})}
	serveFakeOmni(t, fake)

	var prompt signInPrompt

	err := runOmniSignIn(context.Background(), omniConfig, "prod", func(p signInPrompt) {
		prompt = p

		close(fake.confirmed)
	})
	if err != nil {
		t.Fatal(err)
	}

	if prompt.Kind != "browser" || !strings.Contains(prompt.URL, "/authenticate") {
		t.Fatalf("prompt = %+v", prompt)
	}

	if fake.identity != "ops@example.com" || !strings.Contains(fake.publicKey, "PUBLIC KEY") {
		t.Fatalf("registered %q for %q", fake.publicKey, fake.identity)
	}

	info := omniInfo(t, omniConfig)
	if !info.SignedIn || info.Method != omniMethodBrowser || info.User != "ops@example.com" ||
		info.SessionExpires < time.Now().Add(5*time.Hour).Unix() {
		t.Fatalf("info = %+v", info)
	}

	stored := kubeAuth.currentStore().Load(contextFingerprintOf(t, omniConfig, "prod"))
	if backup, err := KubeAuthForBackup(stored); err != nil || backup != "" {
		t.Fatalf("a browser key stays on this device: %q, %v", backup, err)
	}
}

func TestOmniBrowserSignInCancelled(t *testing.T) {
	useMemAuthStore(t)
	serveFakeOmni(t, &fakeOmni{confirmed: make(chan struct{})})

	done := make(chan string, 1)
	run := StartOmniSignIn(omniConfig, "prod", testSignInListener{
		prompt: func(string) {},
		done:   func(msg string) { done <- msg },
	})

	run.Cancel()

	if msg := <-done; msg != "" {
		t.Fatalf("a cancelled sign-in is not an error: %q", msg)
	}

	if omniInfo(t, omniConfig).SignedIn {
		t.Fatal("signed in without approval")
	}
}

func TestOmniBrowserSignInNeedsIdentity(t *testing.T) {
	useMemAuthStore(t)

	yaml := strings.ReplaceAll(omniConfig, "identity: ops@example.com", `identity: ""`)

	err := runOmniSignIn(context.Background(), yaml, "prod", func(signInPrompt) {})
	if err == nil || !strings.Contains(err.Error(), "email") {
		t.Fatalf("err = %v", err)
	}
}

type testSignInListener struct {
	prompt func(string)
	done   func(string)
}

func (l testSignInListener) OnPrompt(json string)     { l.prompt(json) }
func (l testSignInListener) OnDone(errMessage string) { l.done(errMessage) }

// TestOmniSignerSignsEachCall checks what Omni verifies: each call carries a signature of
// its payload by the stored key, under the key's identity, and a new key applies at once.
func TestOmniSignerSignsEachCall(t *testing.T) {
	useMemAuthStore(t)

	fake := &fakeOmni{confirmed: make(chan struct{})}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	authpb.RegisterAuthServiceServer(srv, fake)

	go srv.Serve(lis) //nolint:errcheck

	defer srv.Stop()

	_, ctx, err := resolveContext(omniConfig, "prod")
	if err != nil {
		t.Fatal(err)
	}

	signer := &omniSigner{key: contextFingerprint("prod", ctx), identity: "ops@example.com"}

	conn, err := dialBufconn(lis, signer.dialOptions()...)
	if err != nil {
		t.Fatal(err)
	}

	defer conn.Close() //nolint:errcheck

	revoke := func() error {
		_, err := authpb.NewAuthServiceClient(conn).RevokePublicKey(context.Background(), &authpb.RevokePublicKeyRequest{})

		return err
	}

	if err := revoke(); err == nil || !strings.HasPrefix(err.Error(), OmniSignInRequired) {
		t.Fatalf("no key: err = %v", err)
	}

	for _, name := range []string{"first", "second"} {
		encoded, key := testServiceAccount(t, name, time.Hour)
		if err := OmniSetServiceAccount(omniConfig, "prod", encoded); err != nil {
			t.Fatal(err)
		}

		if err := revoke(); err != nil {
			t.Fatal(err)
		}

		md := fake.signed[len(fake.signed)-1]

		msg := message.NewGRPC(md, authpb.AuthService_RevokePublicKey_FullMethodName)
		if err := msg.VerifySignature(key); err != nil {
			t.Fatalf("%s: signature: %v", name, err)
		}

		sig, err := msg.Signature()
		if err != nil || sig.Identity != name {
			t.Fatalf("%s: identity = %+v, %v", name, sig, err)
		}
	}
}
