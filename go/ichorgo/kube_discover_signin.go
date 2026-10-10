package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
)

// A discovery that signs in in the browser (GKE with the organisation's OAuth client, or the
// build's own Google client on iOS): the apps run StartDiscoverSignIn before DiscoverClusters,
// and the session it gets is kept here, in memory only, for that discovery and for the
// clusters the user then imports with the same credentials (KubeSetCredentials): one browser
// round in all. One discovery runs at a time (the import sheet is modal); a new run replaces
// the session, ForgetDiscoverSignIn drops it.

// discoverSignInTimeout bounds a discovery's browser sign-in, as an Omni account's.
const discoverSignInTimeout = omniConfirmTimeout

var discoverySignIn struct {
	mu    sync.Mutex
	state *kubeAuthState
}

// StartDiscoverSignIn runs the browser sign-in the discovery credentials (secretsJSON, the
// fields KubeDiscoverOptions lists for provider) need before DiscoverClusters. Credentials
// that need none (a key, gcloud user credentials, the token the Android app holds) end at
// once with no error.
func StartDiscoverSignIn(provider, secretsJSON string, listener SignInListener) *SignInRun {
	listener = maskedSignInListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), discoverSignInTimeout)
	run := &SignInRun{cancel: cancel, callbacks: make(chan string, 1)}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := runDiscoverSignIn(ctx, provider, secretsJSON, listener, run.callbacks)
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

func runDiscoverSignIn(ctx context.Context, provider, secretsJSON string, listener SignInListener, callbacks <-chan string) error {
	secrets, err := discoverySecrets(secretsJSON)
	if err != nil {
		return err
	}

	if provider != discoverGKE || !gkeNeedsBrowser(secrets) {
		return nil
	}

	state, err := gkeMethod{}.fromSecrets(secrets)
	if err != nil {
		return err
	}

	ForgetDiscoverSignIn()

	signed, err := gkeMethod{}.signIn(ctx, state, func(p signInPrompt) { emitJSON(p, listener.OnPrompt) }, callbacks)
	if err != nil {
		return err
	}

	signed.Method = authGKE

	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	discoverySignIn.state = &signed

	return nil
}

// ForgetDiscoverSignIn drops the session a discovery's sign-in got (the apps call it when the
// import screen closes).
func ForgetDiscoverSignIn() {
	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	discoverySignIn.state = nil
}

// gkeNeedsBrowser tells GKE credentials that sign in in the browser: the OAuth client, or the
// build's Google client on iOS.
func gkeNeedsBrowser(s map[string]string) bool {
	return s[gcpFieldOAuthClientID] != "" || s[gcpFieldGoogleSignIn] == googleSignInIOS
}

// withDiscoverySession is state with the session of the discovery's sign-in when that sign-in
// was for the same credentials, else state.
func withDiscoverySession(state kubeAuthState) kubeAuthState {
	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	signed := discoverySignIn.state
	if signed == nil || !maps.Equal(signed.Secrets, state.Secrets) {
		return state
	}

	out := state
	out.Session, out.User, out.SessionExpires = maps.Clone(signed.Session), signed.User, signed.SessionExpires

	return out
}

// discoverySecrets is secretsJSON as DiscoverClusters reads it: values trimmed.
func discoverySecrets(secretsJSON string) (map[string]string, error) {
	var secrets map[string]string
	if err := json.Unmarshal([]byte(secretsJSON), &secrets); err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}

	out := make(map[string]string, len(secrets))
	for k, v := range secrets {
		out[k] = strings.TrimSpace(v)
	}

	return out, nil
}
