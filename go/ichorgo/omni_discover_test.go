package ichorgo

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/go-api-signature/pkg/message"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// fakeOmniAPI is Omni's own API: the cluster list and the talosconfigs, signed calls only.
type fakeOmniAPI struct {
	key      *pgp.Key
	clusters []string
	denyList bool
	endpoint string // what the talosconfigs name
}

func (f *fakeOmniAPI) handle(_ any, stream grpc.ServerStream) error {
	method, _ := grpc.MethodFromServerStream(stream)
	md, _ := metadata.FromIncomingContext(stream.Context())

	if err := message.NewGRPC(md, method).VerifySignature(f.key); err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}

	var req []byte
	if err := stream.RecvMsg(&req); err != nil {
		return err
	}

	var resp []byte

	switch method {
	case "/omni.resources.ResourceService/List":
		if got := md.Get("runtime"); len(got) != 1 || got[0] != "Omni" {
			return status.Errorf(codes.InvalidArgument, "runtime = %v", got)
		}

		if f.denyList {
			return status.Error(codes.PermissionDenied, "no access")
		}

		for _, name := range f.clusters {
			item := fmt.Sprintf(`{"metadata":{"id":%q},"spec":{"talos_version":"1.11.2","kubernetes_version":"1.34.1"}}`, name)
			resp = protowire.AppendTag(resp, 1, protowire.BytesType)
			resp = protowire.AppendString(resp, item)
		}
	case "/management.ManagementService/Talosconfig":
		cluster := md.Get("context")
		if len(cluster) != 1 {
			return status.Error(codes.InvalidArgument, "no cluster")
		}

		config := fmt.Sprintf("context: acme-%[1]s\ncontexts:\n    acme-%[1]s:\n        endpoints:\n            - %[2]s\n        auth:\n            siderov1:\n                identity: %[3]s\n        cluster: %[1]s\n", cluster[0], f.endpoint, testUserIdentity)
		resp = protowire.AppendTag(resp, 1, protowire.BytesType)
		resp = protowire.AppendString(resp, config)
	default:
		return status.Errorf(codes.Unimplemented, "%s", method)
	}

	return stream.SendMsg(&resp)
}

func startFakeOmniAPI(t *testing.T, f *fakeOmniAPI) string {
	t.Helper()

	endpoint, caB64 := startTLSServer(t, func(s *grpc.Server) {}, grpc.UnknownServiceHandler(f.handle), grpc.ForceServerCodec(rawCodec{}))

	ca, _ := base64.StdEncoding.DecodeString(caB64)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	omniTestRoots = pool

	t.Cleanup(func() { omniTestRoots = nil })

	f.endpoint = "https://" + endpoint

	return f.endpoint
}

func signInTestAccount(t *testing.T, endpoint string) *pgp.Key {
	t.Helper()

	key, err := pgp.GenerateKey("ichor", "test", testUserIdentity, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	cfgCtx, _ := omniInstanceContext(endpoint, testUserIdentity)
	armored, _ := key.Armor()
	kubeAuth.save(omniAuthKey(cfgCtx), kubeAuthState{Method: omniUserMethod, Session: map[string]string{omniKeySession: armored}})

	return key
}

func TestDiscoverOmniClusters(t *testing.T) {
	useMemAuthStore(t)

	f := &fakeOmniAPI{clusters: []string{"demo", "prod"}}
	endpoint := startFakeOmniAPI(t, f)
	f.key = signInTestAccount(t, endpoint)

	out, err := DiscoverOmniClusters(strings.TrimPrefix(endpoint, "https://"), testUserIdentity)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := parseTalosconfig(out)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Contexts) != 2 || cfg.Contexts["acme-prod"].Cluster != "prod" || !isOmni(cfg.Contexts["acme-demo"]) {
		t.Fatalf("contexts = %v", sortedContextNames(cfg))
	}

	// The imported contexts are signed in already: same identity, same instance.
	if _, err := loadOmniSigner(omniAuthKey(cfg.Contexts["acme-demo"]), cfg.Contexts["acme-demo"]); err != nil {
		t.Fatalf("imported context not signed in: %v", err)
	}
}

func TestDiscoverOmniClustersNeedsSignIn(t *testing.T) {
	useMemAuthStore(t)

	_, err := DiscoverOmniClusters("https://acme.eu-central-1.omni.example.com", testUserIdentity)
	if !strings.HasPrefix(fmt.Sprint(err), KubeSignInRequired) {
		t.Fatalf("err = %v, want a sign-in request", err)
	}
}

func TestDiscoverOmniClustersRoleCannotList(t *testing.T) {
	useMemAuthStore(t)

	f := &fakeOmniAPI{clusters: []string{"demo"}, denyList: true}
	endpoint := startFakeOmniAPI(t, f)
	f.key = signInTestAccount(t, endpoint)

	if _, err := DiscoverOmniClusters(endpoint, testUserIdentity); !strings.Contains(fmt.Sprint(err), "Reader role") {
		t.Fatalf("err = %v, want the role explained", err)
	}
}

func TestSetOmniServiceAccount(t *testing.T) {
	useMemAuthStore(t)

	encoded, key := testServiceAccountKey(t, time.Hour)
	f := &fakeOmniAPI{clusters: []string{"demo"}, key: key}
	endpoint := startFakeOmniAPI(t, f)

	identity, err := SetOmniServiceAccount(endpoint, "OMNI_SERVICE_ACCOUNT_KEY="+encoded)
	if err != nil {
		t.Fatal(err)
	}

	if identity != "ichor-test"+serviceAccountDomain {
		t.Fatalf("identity = %q", identity)
	}

	cfgCtx, _ := omniInstanceContext(endpoint, identity)
	if _, err := loadOmniSigner(omniAuthKey(cfgCtx), cfgCtx); err != nil {
		t.Fatalf("service account not stored: %v", err)
	}

	other, _ := testServiceAccountKey(t, time.Hour)
	if _, err := SetOmniServiceAccount(endpoint, other); !strings.HasPrefix(fmt.Sprint(err), KubeSignInRequired) {
		t.Fatalf("err = %v, want a refused key", err)
	}
}
