package ichorgo

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GKE with "Sign in with Google" through the app's own Google client, when the build carries
// one (SetGoogleSignInClient). On iOS the sign-in runs in the browser with a redirect to the
// reversed-client-ID scheme the app receives and hands over (SignInRun.Complete); Go keeps the
// refresh token. On Android, Google Identity Services gives the app the access token and
// renews it: Go only holds that token while it lasts and asks the app for a new one
// (signInRequired with gcpNativeRenewReason).

const (
	// gcpFieldGoogleSignIn marks the option ("ios" or "android"): the apps draw a button for
	// it, never a text field.
	gcpFieldGoogleSignIn = "gcpGoogleSignIn"
	// gcpFieldAccessToken, its expiry (Unix seconds) and the account: what the Android app
	// hands over after Google Identity Services authorized it.
	gcpFieldAccessToken       = "gcpAccessToken"
	gcpFieldAccessTokenExpiry = "gcpAccessTokenExpiry"
	gcpFieldAccount           = "gcpAccount"

	// gcpNativeRenewReason is the sign-in-required reason the Android app renews silently on.
	gcpNativeRenewReason = "google-native"

	googleSignInIOS     = "ios"
	googleSignInAndroid = "android"
)

// googleSignInClient is the Google client the app's build carries, set at start.
var googleSignInClient struct {
	mu                 sync.RWMutex
	platform, clientID string
}

// SetGoogleSignInClient registers the build's Google client for "Sign in with Google" on GKE:
// platform "ios" with its iOS client ID, or "android" (Google Identity Services needs no ID in
// code); "" removes it. Without one, the option is not offered.
func SetGoogleSignInClient(platform, clientID string) {
	googleSignInClient.mu.Lock()
	defer googleSignInClient.mu.Unlock()

	googleSignInClient.platform, googleSignInClient.clientID = strings.TrimSpace(platform), strings.TrimSpace(clientID)
}

// registeredGoogleSignIn is the build's Google client: its platform ("" for none) and ID.
func registeredGoogleSignIn() (platform, clientID string) {
	googleSignInClient.mu.RLock()
	defer googleSignInClient.mu.RUnlock()

	return googleSignInClient.platform, googleSignInClient.clientID
}

// reversedClientScheme is the URL scheme Google's iOS client type redirects to:
// "123-abc.apps.googleusercontent.com" → "com.googleusercontent.apps.123-abc".
func reversedClientScheme(clientID string) string {
	return "com.googleusercontent.apps." + strings.TrimSuffix(clientID, gcpClientIDSuffix)
}

// googleNativeMethod is the browser sign-in of the build's iOS client, nil when the build
// carries none.
func googleNativeMethod() *oidcMethod {
	platform, clientID := registeredGoogleSignIn()
	if platform != googleSignInIOS || clientID == "" {
		return nil
	}

	return &oidcMethod{
		method: authGKE, issuer: gcpIssuer(), clientID: clientID,
		scopes: []string{"email", gcpScope}, useAccessToken: true,
		redirectURI: reversedClientScheme(clientID) + ":/oauth2redirect", tls: gcpOAuthTLS(), google: true,
	}
}

// errNoGoogleSignIn is a native sign-in stored by a build that carries a Google client,
// used by one that does not (a restored backup): the other options remain.
var errNoGoogleSignIn = signInRequired(authGKE, "Sign in with Google is not available in this build: choose another option")

// nativeSignInState is the state of the native option entered (s): the marker, and for
// Android the token the app holds.
func nativeSignInState(s map[string]string) (kubeAuthState, error) {
	switch s[gcpFieldGoogleSignIn] {
	case googleSignInIOS:
		return kubeAuthState{Secrets: pick(s, gcpFieldGoogleSignIn)}, nil
	case googleSignInAndroid:
		expiry, err := strconv.ParseInt(s[gcpFieldAccessTokenExpiry], 10, 64)
		if s[gcpFieldAccessToken] == "" || err != nil {
			return kubeAuthState{}, errors.New("the Google sign-in gave no access token and expiry")
		}

		state := kubeAuthState{Secrets: pick(s, gcpFieldGoogleSignIn), User: s[gcpFieldAccount]}

		return state.withSession(map[string]string{"bearer": s[gcpFieldAccessToken], "bearerExpiry": strconv.FormatInt(expiry, 10)}), nil
	default:
		return kubeAuthState{}, errors.New("unknown Google sign-in platform")
	}
}

// mintNative is gkeMethod.mint for the native option: the browser sign-in's tokens (iOS), or
// the app-held token while it lasts (Android).
func mintNative(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	if state.secret(gcpFieldGoogleSignIn) == googleSignInIOS {
		m := googleNativeMethod()
		if m == nil {
			return "", time.Time{}, state, errNoGoogleSignIn
		}

		return m.mint(ctx, state)
	}

	if bearer := state.session("bearer"); bearer != "" {
		exp, _ := strconv.ParseInt(state.session("bearerExpiry"), 10, 64)
		if time.Until(time.Unix(exp, 0)) > tokenRefreshMargin {
			return bearer, time.Unix(exp, 0), state, nil
		}
	}

	return "", time.Time{}, state, signInRequired(authGKE, gcpNativeRenewReason)
}

// signInNative is gkeMethod.signIn for the native option: the iOS browser sign-in; Android's
// renews in the app, never here.
func signInNative(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error) {
	if state.secret(gcpFieldGoogleSignIn) != googleSignInIOS {
		return state, errors.New("the app renews this sign-in itself")
	}

	m := googleNativeMethod()
	if m == nil {
		return state, errNoGoogleSignIn
	}

	return m.signIn(ctx, state, prompt, callbacks)
}
