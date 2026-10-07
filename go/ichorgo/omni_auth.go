package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	pgpcrypto "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/siderolabs/go-api-signature/pkg/client/auth"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"github.com/siderolabs/go-api-signature/pkg/serviceaccount"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// An Omni context signs in like a kubeconfig cluster (kube_auth.go), through the same
// AuthStore and the same JSON for the app: an account identity in the browser (Omni shows
// the key ichor generated and the user confirms it), a service account with its key.

const (
	omniUserMethod           = "omni"
	omniServiceAccountMethod = "omni-service-account"
	// omniServiceAccountField is the key omnictl prints as OMNI_SERVICE_ACCOUNT_KEY.
	omniServiceAccountField = "serviceAccountKey"
	omniKeySession          = "pgpKey"
	// omniKeyLifetime is what talosctl asks for: Omni refuses user keys living longer.
	omniKeyLifetime = 4 * time.Hour
	// omniConfirmTimeout bounds the wait for the user to confirm the key in the browser.
	omniConfirmTimeout = 10 * time.Minute
)

// talosSignInRequired is an Omni context that needs a sign-in. It carries the kube code
// (KubeSignInRequired): the apps handle both kinds of cluster the same way.
func talosSignInRequired(method, reason string) error {
	return signInRequired(method, reason)
}

// omniMethodName is how ctx signs in: a service account identity has a key, anyone else
// confirms one in the browser.
func omniMethodName(ctx *clientconfig.Context) string {
	if isOmniServiceAccount(omniIdentity(ctx)) {
		return omniServiceAccountMethod
	}

	return omniUserMethod
}

// omniSigner is the identity and key calls are signed with.
type omniSigner struct {
	identity string
	key      *pgp.Key
}

// loadOmniSigner reads the stored sign-in of the Omni context stored under key.
func loadOmniSigner(key string, ctx *clientconfig.Context) (omniSigner, error) {
	method := omniMethodName(ctx)

	state := kubeAuth.load(key)
	if state.Method != method {
		return omniSigner{}, talosSignInRequired(method, "")
	}

	if method == omniServiceAccountMethod {
		signer, err := decodeServiceAccount(state.secret(omniServiceAccountField))
		if err != nil {
			return omniSigner{}, talosSignInRequired(method, err.Error())
		}

		return signer, nil
	}

	signer, err := decodeUserKey(omniIdentity(ctx), state.session(omniKeySession))
	if err != nil {
		return omniSigner{}, talosSignInRequired(method, err.Error())
	}

	return signer, nil
}

// decodeServiceAccount parses an OMNI_SERVICE_ACCOUNT_KEY (pasted with or without its name).
func decodeServiceAccount(value string) (omniSigner, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "OMNI_SERVICE_ACCOUNT_KEY=")
	value = strings.TrimPrefix(value, "SIDERO_SERVICE_ACCOUNT_KEY=")

	if value == "" {
		return omniSigner{}, errors.New("no service account key")
	}

	sa, err := serviceaccount.Decode(value)
	if err != nil {
		return omniSigner{}, errors.New("not an Omni service account key (OMNI_SERVICE_ACCOUNT_KEY)")
	}

	if sa.Key.IsExpired(0) {
		return omniSigner{}, errors.New("the service account key expired: renew it in Omni")
	}

	return omniSigner{identity: sa.Name, key: sa.Key}, nil
}

func decodeUserKey(identity, armored string) (omniSigner, error) {
	if armored == "" {
		return omniSigner{}, errors.New("")
	}

	parsed, err := pgpcrypto.NewKeyFromArmored(armored)
	if err != nil {
		return omniSigner{}, errors.New("the stored key is unreadable")
	}

	key, err := pgp.NewKey(parsed)
	if err != nil {
		return omniSigner{}, errors.New("the stored key is unreadable")
	}

	if key.IsExpired(0) {
		return omniSigner{}, errors.New("the Omni sign-in expired")
	}

	return omniSigner{identity: identity, key: key}, nil
}

// omniSignInContext is a stored Omni context and where its sign-in is kept.
type omniSignInContext struct {
	key string
	ctx *clientconfig.Context
}

func loadOmniSignInContext(storedYAML, contextName string) (omniSignInContext, error) {
	name, ctx, err := resolveContext(storedYAML, contextName)
	if err != nil {
		return omniSignInContext{}, err
	}

	if !isOmni(ctx) {
		return omniSignInContext{ctx: ctx}, nil
	}

	if omniIdentity(ctx) == "" {
		return omniSignInContext{}, errors.New("the Omni context has no identity (auth.siderov1.identity)")
	}

	return omniSignInContext{key: contextFingerprint(name, ctx), ctx: ctx}, nil
}

// TalosSignInInfo describes how the named stored talosconfig context signs in, as a JSON
// kubeSignInKind, or "" when it has a client certificate.
func TalosSignInInfo(storedYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(storedYAML, contextName)

	sc, err := loadOmniSignInContext(storedYAML, contextName)
	if err != nil || sc.key == "" {
		return "", err
	}

	method := omniMethodName(sc.ctx)
	info := kubeSignInKind{Method: method, Kind: "browser"}

	if method == omniServiceAccountMethod {
		info.Kind = "credentials"
		info.Fields = []string{omniServiceAccountField}
		info.Options = [][]string{info.Fields}
	}

	if _, err := loadOmniSigner(sc.key, sc.ctx); err == nil {
		state := kubeAuth.load(sc.key)
		info.SignedIn = true
		info.User, info.SessionExpires = state.User, state.SessionExpires
	}

	return toJSON(info)
}

// TalosSetCredentials signs the named stored Omni context in with a service account key
// (secretsJSON: {"serviceAccountKey": "..."}): checked by listing the cluster's machines
// through Omni, then stored.
func TalosSetCredentials(storedYAML, contextName, secretsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	sc, err := loadOmniSignInContext(storedYAML, contextName)
	if err != nil {
		return err
	}

	if sc.key == "" || omniMethodName(sc.ctx) != omniServiceAccountMethod {
		return errors.New("this cluster does not sign in with a service account key")
	}

	var secrets map[string]string
	if err := json.Unmarshal([]byte(secretsJSON), &secrets); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}

	value := strings.TrimSpace(secrets[omniServiceAccountField])

	signer, err := decodeServiceAccount(value)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if err := checkOmniSigner(ctx, sc.ctx, signer); err != nil {
		return err
	}

	kubeAuth.forget(sc.key)
	kubeAuth.save(sc.key, kubeAuthState{
		Method:  omniServiceAccountMethod,
		Secrets: map[string]string{omniServiceAccountField: value},
		User:    signer.identity,
	})
	sessions.forgetAuth(sc.key)

	return nil
}

// checkOmniSigner makes one signed call through Omni with signer.
func checkOmniSigner(ctx context.Context, cfgCtx *clientconfig.Context, signer omniSigner) error {
	opts, err := omniClientOptionsFor(cfgCtx, signer, nil)
	if err != nil {
		return err
	}

	c, err := client.New(ctx, opts...)
	if err != nil {
		return fmt.Errorf("create Talos client: %w", err)
	}

	defer c.Close() //nolint:errcheck

	if _, err := learnOmniNodes(ctx, c); err != nil {
		return errors.New(friendlyError(errors.Unwrap(err)))
	}

	return nil
}

// StartTalosSignIn signs the named stored Omni context in through the browser: it registers
// a new key with Omni, prompts the app to open the page where the user confirms it, waits
// for the confirmation, then stores the key (see AuthStore).
func StartTalosSignIn(storedYAML, contextName string, listener SignInListener) *SignInRun {
	contextName = unmaskContext(storedYAML, contextName)

	listener = maskedSignInListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), omniConfirmTimeout)
	run := &SignInRun{cancel: cancel, callbacks: make(chan string, 1)}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := runOmniSignIn(ctx, storedYAML, contextName, listener)
		if errors.Is(err, context.Canceled) {
			err = nil
		}

		msg := ""
		if err != nil {
			msg = err.Error()
		}

		listener.OnDone(msg)
	}()

	return run
}

func runOmniSignIn(ctx context.Context, storedYAML, contextName string, listener SignInListener) error {
	sc, err := loadOmniSignInContext(storedYAML, contextName)
	if err != nil {
		return err
	}

	if sc.key == "" || omniMethodName(sc.ctx) != omniUserMethod {
		return errors.New("this cluster does not sign in in a browser")
	}

	identity := omniIdentity(sc.ctx)

	key, err := pgp.GenerateKey("ichor", runtime.GOOS+"/"+runtime.GOARCH, identity, omniKeyLifetime)
	if err != nil {
		return fmt.Errorf("generate a key: %w", err)
	}

	if err := confirmOmniKey(ctx, sc.ctx, identity, key, func(loginURL string) {
		emitJSON(signInPrompt{Kind: "browser", URL: loginURL, ExpiresIn: int(omniConfirmTimeout / time.Second)}, listener.OnPrompt)
	}); err != nil {
		return err
	}

	armored, err := key.Armor()
	if err != nil {
		return fmt.Errorf("store the key: %w", err)
	}

	kubeAuth.forget(sc.key)
	kubeAuth.save(sc.key, kubeAuthState{
		Method:         omniUserMethod,
		Session:        map[string]string{omniKeySession: armored},
		User:           identity,
		SessionExpires: time.Now().Add(omniKeyLifetime).Unix(),
	})
	sessions.forgetAuth(sc.key)

	return nil
}

// confirmOmniKey registers key's public half for identity, hands the login page to open, and
// waits until the user confirmed it there. The auth API takes unsigned calls.
func confirmOmniKey(ctx context.Context, cfgCtx *clientconfig.Context, identity string, key *pgp.Key, open func(loginURL string)) error {
	public, err := key.ArmorPublic()
	if err != nil {
		return fmt.Errorf("export the key: %w", err)
	}

	unsigned := *cfgCtx
	unsigned.Auth.SideroV1 = nil

	c, err := client.New(ctx, client.WithConfigContext(&unsigned))
	if err != nil {
		return fmt.Errorf("create Talos client: %w", err)
	}

	defer c.Close() //nolint:errcheck

	authClient := auth.NewClient(c.Conn())

	loginURL, err := authClient.RegisterPGPPublicKey(ctx, identity, []byte(public))
	if err != nil {
		return fmt.Errorf("register the key with Omni: %s", friendlyError(err))
	}

	open(loginURL)

	if err := authClient.AwaitPublicKeyConfirmation(ctx, key.Fingerprint()); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("the key was not confirmed in Omni in time: sign in again")
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("confirm the key: %s", friendlyError(err))
	}

	return nil
}

// TalosSignOut forgets the sign-in of the named stored Omni context.
func TalosSignOut(storedYAML, contextName string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	sc, err := loadOmniSignInContext(storedYAML, contextName)
	if err != nil || sc.key == "" {
		return err
	}

	kubeAuth.forget(sc.key)
	sessions.forgetAuth(sc.key)

	return nil
}
