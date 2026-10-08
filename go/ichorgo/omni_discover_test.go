package ichorgo

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/siderolabs/go-api-signature/pkg/message"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeOmniAPI is Omni's own API: the cluster list and the talosconfigs, signed calls only.
type fakeOmniAPI struct {
	v1alpha1.UnimplementedStateServer

	key      *pgp.Key
	clusters []string
	denyList bool
	endpoint string // what the talosconfigs name
}

func (f *fakeOmniAPI) handle(_ any, stream grpc.ServerStream) error {
	method, _ := grpc.MethodFromServerStream(stream)
	md, _ := metadata.FromIncomingContext(stream.Context())

	// Omni routes only this content type to its API (application/grpc+proto goes to its web server).
	if got := md.Get("content-type"); len(got) != 1 || got[0] != "application/grpc" {
		return status.Errorf(codes.Unimplemented, "content-type %v reached the web server", got)
	}

	if err := message.NewGRPC(md, method).VerifySignature(f.key); err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}

	req, err := recvRaw(stream)
	if err != nil {
		return err
	}

	var resp []byte

	switch method {
	case "/management.ManagementService/Talosconfig":
		cluster := md.Get("context")
		if len(cluster) != 1 {
			return status.Error(codes.InvalidArgument, "no cluster")
		}

		config := fmt.Sprintf("context: acme-%[1]s\ncontexts:\n    acme-%[1]s:\n        endpoints:\n            - %[2]s\n        auth:\n            siderov1:\n                identity: %[3]s\n        cluster: %[1]s\n", cluster[0], f.endpoint, testUserIdentity)
		resp = protowire.AppendTag(resp, 1, protowire.BytesType)
		resp = protowire.AppendString(resp, config)
	default:
		_ = req

		return status.Errorf(codes.Unimplemented, "%s", method)
	}

	return sendRaw(stream, resp)
}

// recvRaw and sendRaw carry the fake's messages encoded by hand, as an Empty's unknown fields.
func recvRaw(stream grpc.ServerStream) ([]byte, error) {
	in := &emptypb.Empty{}
	if err := stream.RecvMsg(in); err != nil {
		return nil, err
	}

	return in.ProtoReflect().GetUnknown(), nil
}

func sendRaw(stream grpc.ServerStream, msg []byte) error {
	out := &emptypb.Empty{}
	out.ProtoReflect().SetUnknown(msg)

	return stream.SendMsg(out)
}

// List is Omni's COSI state: the clusters, to a signed call.
func (f *fakeOmniAPI) List(req *v1alpha1.ListRequest, stream grpc.ServerStreamingServer[v1alpha1.ListResponse]) error {
	md, _ := metadata.FromIncomingContext(stream.Context())

	if err := message.NewGRPC(md, "/cosi.resource.State/List").VerifySignature(f.key); err != nil {
		return status.Error(codes.Unauthenticated, err.Error())
	}

	if req.GetNamespace() != "default" || req.GetType() != "Clusters.omni.sidero.dev" {
		return status.Errorf(codes.InvalidArgument, "list %s/%s", req.GetNamespace(), req.GetType())
	}

	if f.denyList {
		return status.Error(codes.PermissionDenied, "no access")
	}

	for _, name := range f.clusters {
		res := &v1alpha1.Resource{
			Metadata: &v1alpha1.Metadata{Namespace: "default", Type: req.GetType(), Id: name},
			Spec:     &v1alpha1.Spec{YamlSpec: "kubernetesversion: 1.34.1\ntalosversion: 1.11.2\n"},
		}

		if err := stream.Send(&v1alpha1.ListResponse{Resource: res}); err != nil {
			return err
		}
	}

	return nil
}

func startFakeOmniAPI(t *testing.T, f *fakeOmniAPI) string {
	t.Helper()

	endpoint, caB64 := startTLSServer(t, func(s *grpc.Server) { v1alpha1.RegisterStateServer(s, f) }, grpc.UnknownServiceHandler(f.handle))

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
	if _, err := SetOmniServiceAccount(endpoint, other); fmt.Sprint(err) != "omni refused this service account key" {
		t.Fatalf("err = %v, want a refused key", err)
	}
}
