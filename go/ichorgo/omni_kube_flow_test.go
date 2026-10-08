package ichorgo

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/siderolabs/go-api-signature/pkg/message"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
)

// fakeOmniOIDC is Omni's OIDC issuer: an authorization request redirects to the login page,
// the code comes from Authenticate (fakeOmniAPI), the token endpoint checks PKCE.
type fakeOmniOIDC struct {
	*httptest.Server

	mu        sync.Mutex
	challenge string
	scope     string
}

func startFakeOmniOIDC(t *testing.T) *fakeOmniOIDC {
	t.Helper()

	f := &fakeOmniOIDC{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oidc/.well-known/openid-configuration":
			fmt.Fprintf(w, `{"authorization_endpoint":%q,"token_endpoint":%q}`, f.URL+"/oidc/authorize", f.URL+"/oidc/oauth/token")
		case "/oidc/authorize":
			q := r.URL.Query()
			if q.Get("client_id") != omniOIDCClient || q.Get("redirect_uri") != omniOIDCRedirect || q.Get("code_challenge_method") != "S256" {
				http.Error(w, "bad request", http.StatusBadRequest)

				return
			}

			f.mu.Lock()
			f.challenge, f.scope = q.Get("code_challenge"), q.Get("scope")
			f.mu.Unlock()

			http.Redirect(w, r, "/omni/oidc-login/req-1", http.StatusFound)
		case "/oidc/oauth/token":
			_ = r.ParseForm()
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))

			f.mu.Lock()
			ok := r.PostForm.Get("code") == "c0ffee" && base64.RawURLEncoding.EncodeToString(sum[:]) == f.challenge
			f.mu.Unlock()

			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant"}`)

				return
			}

			fmt.Fprintf(w, `{"id_token":%q,"expires_in":43200}`, idToken(map[string]any{"sub": testUserIdentity, "exp": time.Now().Add(12 * time.Hour).Unix()}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)

	return f
}

// omniKubeAPI adds Authenticate and Kubeconfig to fakeOmniAPI.
type omniKubeAPI struct {
	*fakeOmniAPI

	issuer string
}

func (f *omniKubeAPI) handle(srv any, stream grpc.ServerStream) error {
	method, _ := grpc.MethodFromServerStream(stream)
	md, _ := metadata.FromIncomingContext(stream.Context())

	var resp []byte

	switch method {
	case "/oidc.OIDCService/Authenticate":
		if err := message.NewGRPC(md, method).VerifySignature(f.key); err != nil {
			return status.Error(codes.Unauthenticated, err.Error())
		}

		req, err := recvRaw(stream)
		if err != nil {
			return err
		}

		if id, _ := protoField(req, 1); string(id) != "req-1" {
			return status.Errorf(codes.NotFound, "auth request %q", id)
		}

		resp = protowire.AppendTag(resp, 2, protowire.BytesType)
		resp = protowire.AppendString(resp, "c0ffee")
	case "/management.ManagementService/Kubeconfig":
		if err := message.NewGRPC(md, method).VerifySignature(f.key); err != nil {
			return status.Error(codes.Unauthenticated, err.Error())
		}

		if _, err := recvRaw(stream); err != nil {
			return err
		}

		cluster := md.Get("context")[0]
		kube := strings.NewReplacer("acme.omni.example.com/oidc", strings.TrimPrefix(f.issuer, "https://"), "cluster:demo", "cluster:"+cluster).Replace(omniKubeconfigSample)
		resp = protowire.AppendTag(resp, 1, protowire.BytesType)
		resp = protowire.AppendString(resp, kube)
	default:
		return f.fakeOmniAPI.handle(srv, stream)
	}

	return sendRaw(stream, resp)
}

func TestOmniOIDCTokenWithoutBrowser(t *testing.T) {
	useMemAuthStore(t)

	oidc := startFakeOmniOIDC(t)
	f := &omniKubeAPI{fakeOmniAPI: &fakeOmniAPI{clusters: []string{"demo"}}, issuer: oidc.URL + "/oidc"}

	endpoint, caB64 := startTLSServer(t, func(s *grpc.Server) { v1alpha1.RegisterStateServer(s, f.fakeOmniAPI) }, grpc.UnknownServiceHandler(f.handle))
	pool := oidc.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	ca, _ := base64.StdEncoding.DecodeString(caB64)
	pool.AppendCertsFromPEM(ca)
	omniTestRoots = pool

	t.Cleanup(func() { omniTestRoots = nil })

	f.key = signInTestAccount(t, "https://"+endpoint)

	cfgCtx, _ := omniInstanceContext("https://"+endpoint, testUserIdentity)
	cfgCtx.Cluster = "demo"

	signer, err := loadOmniSigner(omniAuthKey(cfgCtx), cfgCtx)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	kubeYAML, issuer, err := omniClusterKubeconfig(ctx, cfgCtx, signer)
	if err != nil {
		t.Fatal(err)
	}

	if issuer != f.issuer || !strings.Contains(kubeYAML, "server: https://acme.kubernetes.omni.example.com") || strings.Contains(kubeYAML, "oidc-login") {
		t.Fatalf("issuer = %q, kubeconfig:\n%s", issuer, kubeYAML)
	}

	tokens := &omniKubeTokens{ctx: cfgCtx, issuer: issuer, cluster: "demo"}

	token, expiry, state, err := tokens.mint(ctx, kubeAuthState{})
	if err != nil {
		t.Fatal(err)
	}

	claims, _ := readIDToken(token)
	if claims.Subject != testUserIdentity || time.Until(expiry) < 11*time.Hour || state.User != testUserIdentity {
		t.Fatalf("claims = %+v, expiry = %v, state = %+v", claims, expiry, state)
	}

	oidc.mu.Lock()
	scope := oidc.scope
	oidc.mu.Unlock()

	if scope != "openid cluster:demo" {
		t.Fatalf("scope = %q", scope)
	}
}

func TestOmniKubeTokensNeedSignIn(t *testing.T) {
	useMemAuthStore(t)

	cfgCtx, _ := omniInstanceContext("https://acme.eu-central-1.omni.example.com", testUserIdentity)
	tokens := &omniKubeTokens{ctx: cfgCtx, issuer: "https://acme.eu-central-1.omni.example.com/oidc", cluster: "demo"}

	if _, _, _, err := tokens.mint(context.Background(), kubeAuthState{}); !strings.HasPrefix(fmt.Sprint(err), KubeSignInRequired) {
		t.Fatalf("err = %v, want a sign-in request", err)
	}
}
