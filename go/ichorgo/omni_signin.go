package ichorgo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"runtime"
	"time"

	"github.com/siderolabs/go-api-signature/pkg/client/auth"
	"github.com/siderolabs/go-api-signature/pkg/pgp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// omniKeyLifetime is how long a browser sign-in lasts. Omni refuses keys living over 8h.
const omniKeyLifetime = 6 * time.Hour

// dialOmni connects to the Omni instance itself (not through a cluster), for its auth API.
// A variable for tests.
var dialOmni = func(instance string) (*grpc.ClientConn, error) {
	u, err := url.Parse(instance)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid Omni address %q", instance)
	}

	port := u.Port()
	if port == "" {
		port = "443"
	}

	return grpc.NewClient(net.JoinHostPort(u.Hostname(), port),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})))
}

// StartOmniSignIn signs the named stored Omni context in through the browser, as talosctl
// does: a new key is registered with Omni, the user approves it on the page OnPrompt
// opens, and the key is stored (see AuthStore) until it expires.
func StartOmniSignIn(storedYAML, contextName string, listener SignInListener) *SignInRun {
	contextName = unmaskContext(storedYAML, contextName)

	listener = maskedSignInListener{listener}

	return startSignIn(listener, func(ctx context.Context, listener SignInListener, _ <-chan string) error {
		return runOmniSignIn(ctx, storedYAML, contextName, func(p signInPrompt) {
			if raw, err := toJSON(p); err == nil {
				listener.OnPrompt(raw)
			}
		})
	})
}

func runOmniSignIn(ctx context.Context, storedYAML, contextName string, prompt func(signInPrompt)) error {
	oc, err := omniContextOf(storedYAML, contextName)
	if err != nil {
		return err
	}

	identity := oc.ctx.Auth.SideroV1.Identity
	if identity == "" {
		return errors.New("signing in to Omni needs the account's email (identity) in the cluster's settings")
	}

	key, err := pgp.GenerateKey("Ichor", runtime.GOOS, identity, omniKeyLifetime)
	if err != nil {
		return fmt.Errorf("create a key: %w", err)
	}

	public, err := key.ArmorPublic()
	if err != nil {
		return fmt.Errorf("create a key: %w", err)
	}

	conn, err := dialOmni(oc.ctx.Endpoints[0])
	if err != nil {
		return err
	}

	defer conn.Close() //nolint:errcheck

	omni := auth.NewClient(conn)

	loginURL, err := omni.RegisterPGPPublicKey(ctx, identity, []byte(public))
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		return fmt.Errorf("the key was refused by Omni: %s", friendlyError(err))
	}

	prompt(signInPrompt{Kind: "browser", URL: loginURL, ExpiresIn: int(omniKeyLifetime / time.Second)})

	if err := omni.AwaitPublicKeyConfirmation(ctx, key.Fingerprint()); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("the key was not approved: %s", friendlyError(err))
	}

	armored, err := key.Armor()
	if err != nil {
		return fmt.Errorf("keep the key: %w", err)
	}

	kubeAuth.forget(oc.key)
	kubeAuth.save(oc.key, kubeAuthState{
		Method:         omniMethodBrowser,
		Session:        map[string]string{omniSessionKey: armored},
		User:           identity,
		SessionExpires: time.Now().Add(omniKeyLifetime).Unix(),
	})

	return nil
}
