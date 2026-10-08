package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adding clusters from an Omni sign-in: the user names the instance and signs in (an account
// in the browser, or a service account key), then Omni lists the account's clusters and
// gives a talosconfig for each, imported like any other. The sign-in is the one the imported
// contexts use (see omniAuthKey): no second one.

// StartOmniAccountSignIn signs identity (an account email) in to the Omni instance at
// endpoint in the browser: the key it confirms there is stored for that identity's clusters.
func StartOmniAccountSignIn(endpoint, identity string, listener SignInListener) *SignInRun {
	listener = maskedSignInListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), omniConfirmTimeout)
	run := &SignInRun{cancel: cancel, callbacks: make(chan string, 1)}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := func() error {
			cfgCtx, err := omniInstanceContext(endpoint, identity)
			if err != nil {
				return err
			}

			return signInOmniUser(ctx, cfgCtx, listener)
		}()
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

// SetOmniServiceAccount checks serviceAccountKey (OMNI_SERVICE_ACCOUNT_KEY) against the Omni
// instance at endpoint, stores it, and returns the identity it signs as (what
// DiscoverOmniClusters takes).
func SetOmniServiceAccount(endpoint, serviceAccountKey string) (out string, err error) {
	defer maskErr(&err)

	signer, err := decodeServiceAccount(serviceAccountKey)
	if err != nil {
		return "", err
	}

	identity := signer.identity
	if !strings.Contains(identity, "@") {
		identity += serviceAccountDomain
	}

	cfgCtx, err := omniInstanceContext(endpoint, identity)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	// A key Omni authenticates is good, whether or not its role lists clusters.
	if _, err := listOmniClusters(ctx, cfgCtx, signer); err != nil && status.Code(err) != codes.PermissionDenied {
		if needSignIn := (*errSignInRequired)(nil); errors.As(err, &needSignIn) {
			return "", errOmniRefusedKey
		}

		return "", omniAPIError(err)
	}

	key := omniAuthKey(cfgCtx)
	kubeAuth.forget(key)
	kubeAuth.save(key, kubeAuthState{
		Method:  omniServiceAccountMethod,
		Secrets: map[string]string{omniServiceAccountField: strings.TrimSpace(serviceAccountKey)},
		User:    signer.identity,
	})
	omniAuthChanged(key)

	return identity, nil
}

// DiscoverOmniClusters returns a talosconfig of the clusters identity sees on the Omni
// instance at endpoint (one context each, as Omni names them), for the import preview. The
// identity must be signed in (StartOmniAccountSignIn, SetOmniServiceAccount).
func DiscoverOmniClusters(endpoint, identity string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	cfgCtx, err := omniInstanceContext(endpoint, identity)
	if err != nil {
		return "", err
	}

	authKey := omniAuthKey(cfgCtx)

	signer, err := loadOmniSigner(authKey, cfgCtx)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	cfg, err := omniClusterConfigs(ctx, cfgCtx, signer)
	if err != nil {
		return "", err
	}

	// Omni writes its own URL in the configs: the sign-in follows it if it differs.
	for _, c := range cfg.Contexts {
		if key := omniAuthKey(c); key != authKey {
			kubeAuth.save(key, kubeAuth.load(authKey))
			omniAuthChanged(key)
		}
	}

	return encodeConfig(cfg)
}

func listOmniClusters(ctx context.Context, cfgCtx *clientconfig.Context, signer omniSigner) ([]omniCluster, error) {
	cc, err := omniDial(cfgCtx, &omniSigning{method: omniMethodName(cfgCtx), signer: signer})
	if err != nil {
		return nil, fmt.Errorf("connect to Omni: %w", err)
	}

	defer cc.Close() //nolint:errcheck

	return omniListClusters(ctx, cc)
}

// omniClusterConfigs is one talosconfig with a context per cluster of the account.
func omniClusterConfigs(ctx context.Context, cfgCtx *clientconfig.Context, signer omniSigner) (*clientconfig.Config, error) {
	cc, err := omniDial(cfgCtx, &omniSigning{method: omniMethodName(cfgCtx), signer: signer})
	if err != nil {
		return nil, fmt.Errorf("connect to Omni: %w", err)
	}

	defer cc.Close() //nolint:errcheck

	clusters, err := omniListClusters(ctx, cc)
	if err != nil {
		return nil, omniAPIError(err)
	}

	if len(clusters) == 0 {
		return nil, errors.New("this Omni account has no cluster")
	}

	out := &clientconfig.Config{Contexts: map[string]*clientconfig.Context{}}

	for _, cl := range clusters {
		raw, err := omniTalosconfig(ctx, cc, cl.Name)
		if err != nil {
			return nil, fmt.Errorf("talosconfig of %s: %w", cl.Name, omniAPIError(err))
		}

		one, err := parseTalosconfig(string(raw))
		if err != nil {
			return nil, fmt.Errorf("talosconfig of %s: %w", cl.Name, err)
		}

		for name, c := range one.Contexts {
			if c.Cluster == "" {
				c.Cluster = cl.Name
			}

			out.Contexts[name] = c

			if out.Context == "" {
				out.Context = name
			}
		}
	}

	return out, nil
}

// omniAPIError says what Omni's refusals mean for the user.
func omniAPIError(err error) error {
	var needSignIn *errSignInRequired
	if errors.As(err, &needSignIn) {
		return needSignIn
	}

	switch status.Code(err) { //nolint:exhaustive
	case codes.PermissionDenied:
		return errors.New("your Omni role cannot list the clusters: ask for the Reader role, or import the cluster's talosconfig from Omni")
	case codes.Unimplemented:
		return errors.New("this is not an Omni instance, or a version too old")
	}

	return errors.New(friendlyError(err))
}
