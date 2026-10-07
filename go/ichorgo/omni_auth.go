package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	pgpcrypto "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/siderolabs/go-api-signature/pkg/message"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"github.com/siderolabs/go-api-signature/pkg/serviceaccount"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Omni signs every request with a PGP key: a service account's (the user pastes its key,
// kept as a secret, so in backups) or a key the user approved in the browser (kept as the
// session, this device only; omni_signin.go). Both live in the AuthStore under the
// context's fingerprint, like a kubeconfig cluster's sign-in.

const (
	omniMethodServiceAccount = "omni-service-account"
	omniMethodBrowser        = "omni-browser"

	omniSecretServiceAccount = "serviceAccount"
	omniSessionKey           = "pgpKey"
)

// OmniSignInRequired is the code an error starts with when an Omni cluster needs a key.
const OmniSignInRequired = "omni-sign-in-required"

type errOmniSignIn struct{ reason string }

func (e *errOmniSignIn) Error() string {
	return OmniSignInRequired + ": sign in to Omni: " + e.reason
}

func omniSignInRequired(reason string) error { return &errOmniSignIn{reason: reason} }

// omniClockSkew is how early a browser key counts as expired.
const omniClockSkew = time.Minute

// omniSignInContext is a stored Omni context and where its key lives.
type omniSignInContext struct {
	name string
	key  string
	ctx  *clientconfig.Context
}

func omniContextOf(configYAML, contextName string) (omniSignInContext, error) {
	name, ctx, err := resolveContext(configYAML, contextName)
	if err != nil {
		return omniSignInContext{}, err
	}

	if !isOmni(ctx) {
		return omniSignInContext{}, fmt.Errorf("context %q is not an Omni cluster", name)
	}

	return omniSignInContext{name: name, key: contextFingerprint(name, ctx), ctx: ctx}, nil
}

// omniCredential is what signs: the identity sent along and its key.
type omniCredential struct {
	identity string
	signer   message.Signer
	expires  time.Time
}

// omniCredentialFrom reads a stored state: the service account first, then a browser key
// still valid.
func omniCredentialFrom(state kubeAuthState, identity string) (omniCredential, error) {
	if sa := state.secret(omniSecretServiceAccount); sa != "" {
		decoded, err := serviceaccount.Decode(sa)
		if err != nil {
			return omniCredential{}, omniSignInRequired("the stored service account key is invalid")
		}

		return omniCredential{identity: decoded.Name, signer: decoded.Key}, nil
	}

	armored := state.session(omniSessionKey)
	if armored == "" {
		return omniCredential{}, omniSignInRequired("no key")
	}

	key, err := parsePGPKey(armored)
	if err != nil {
		return omniCredential{}, omniSignInRequired("the stored key is invalid")
	}

	if key.IsExpired(omniClockSkew) {
		return omniCredential{}, omniSignInRequired("the key expired")
	}

	if state.User != "" {
		identity = state.User
	}

	return omniCredential{identity: identity, signer: key, expires: time.Unix(state.SessionExpires, 0)}, nil
}

func parsePGPKey(armored string) (*pgp.Key, error) {
	raw, err := pgpcrypto.NewKeyFromArmored(armored)
	if err != nil {
		return nil, err
	}

	return pgp.NewKey(raw)
}

// omniSigner signs each call with the key stored now: a new key (or a sign-out) applies to
// cached sessions at once. The parsed key is kept while the stored state is unchanged.
type omniSigner struct {
	key      string
	identity string

	mu     sync.Mutex
	raw    string
	cached omniCredential
}

func (s *omniSigner) credential() (omniCredential, error) {
	raw := kubeAuth.currentStore().Load(s.key)

	s.mu.Lock()
	defer s.mu.Unlock()

	if raw != "" && raw == s.raw {
		return s.cached, nil
	}

	cred, err := omniCredentialFrom(kubeAuth.load(s.key), s.identity)
	if err != nil {
		return omniCredential{}, err
	}

	s.raw, s.cached = raw, cred

	return cred, nil
}

func (s *omniSigner) sign(ctx context.Context, method string) (context.Context, error) {
	cred, err := s.credential()
	if err != nil {
		return nil, err
	}

	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	} else {
		md = md.Copy()
	}

	msg := message.NewGRPC(md, method)
	if err := msg.Sign(cred.identity, cred.signer); err != nil {
		return nil, fmt.Errorf("sign the Omni request: %w", err)
	}

	return metadata.NewOutgoingContext(ctx, msg.Metadata), nil
}

// refused turns Omni refusing the key into a sign-in request.
func refused(err error) error {
	if status.Code(err) == codes.Unauthenticated {
		return omniSignInRequired("Omni refused the key")
	}

	return err
}

func (s *omniSigner) dialOptions() []grpc.DialOption {
	unary := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		signed, err := s.sign(ctx, method)
		if err != nil {
			return err
		}

		return refused(invoker(signed, method, req, reply, cc, opts...))
	}

	stream := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		signed, err := s.sign(ctx, method)
		if err != nil {
			return nil, err
		}

		cs, err := streamer(signed, desc, cc, method, opts...)

		return cs, refused(err)
	}

	return []grpc.DialOption{grpc.WithUnaryInterceptor(unary), grpc.WithStreamInterceptor(stream)}
}

// omniSessionContext is how openSession reaches an Omni cluster: the context without its
// siderov1 auth (the library's own interceptor reads keys from disk and opens a desktop
// browser), the cluster to proxy to, and our signer. Without a stored key it fails before
// dialing, with the sign-in code.
func omniSessionContext(name string, ctx *clientconfig.Context) (*clientconfig.Context, *omniSigner, error) {
	signer := &omniSigner{key: contextFingerprint(name, ctx), identity: ctx.Auth.SideroV1.Identity}
	if _, err := signer.credential(); err != nil {
		return nil, nil, err
	}

	plain := *ctx
	plain.Auth = clientconfig.Auth{}

	return &plain, signer, nil
}

// omniSignInInfo is what the app shows about an Omni cluster's sign-in.
type omniSignInInfo struct {
	SignedIn bool `json:"signedIn"`
	// Method is "omni-service-account" or "omni-browser" when signed in.
	Method string `json:"method,omitempty"`
	// User is the service account or the identity the key signs as.
	User string `json:"user,omitempty"`
	// Identity is the context's own (a browser sign-in needs one); Instance the Omni URL.
	Identity       string `json:"identity,omitempty"`
	Instance       string `json:"instance"`
	SessionExpires int64  `json:"sessionExpires,omitempty"`
}

// OmniSignInInfo describes the sign-in of the named stored Omni context (JSON omniSignInInfo).
func OmniSignInInfo(storedYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(storedYAML, contextName)

	oc, err := omniContextOf(storedYAML, contextName)
	if err != nil {
		return "", err
	}

	info := omniSignInInfo{Identity: oc.ctx.Auth.SideroV1.Identity, Instance: oc.ctx.Endpoints[0]}
	state := kubeAuth.load(oc.key)

	if cred, err := omniCredentialFrom(state, info.Identity); err == nil {
		info.SignedIn, info.Method, info.User = true, state.Method, cred.identity
		if !cred.expires.IsZero() {
			info.SessionExpires = cred.expires.Unix()
		}
	}

	return toJSON(info)
}

// OmniSetServiceAccount signs the named stored Omni context in with a service account key
// (base64, as Omni shows it: OMNI_SERVICE_ACCOUNT_KEY).
func OmniSetServiceAccount(storedYAML, contextName, keyBase64 string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	oc, err := omniContextOf(storedYAML, contextName)
	if err != nil {
		return err
	}

	keyBase64 = strings.Join(strings.Fields(keyBase64), "")

	sa, err := serviceaccount.Decode(keyBase64)
	if err != nil || sa.Key == nil || !sa.Key.IsPrivate() {
		return errors.New("this is not an Omni service account key (base64, as Omni shows it)")
	}

	if sa.Key.IsExpired(omniClockSkew) {
		return errors.New("this service account key has expired")
	}

	kubeAuth.forget(oc.key)
	kubeAuth.save(oc.key, kubeAuthState{
		Method:  omniMethodServiceAccount,
		Secrets: map[string]string{omniSecretServiceAccount: keyBase64},
		User:    sa.Name,
	})

	return nil
}

// OmniSignOut forgets the key of the named stored Omni context.
func OmniSignOut(storedYAML, contextName string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	oc, err := omniContextOf(storedYAML, contextName)
	if err != nil {
		return err
	}

	kubeAuth.forget(oc.key)

	return nil
}
