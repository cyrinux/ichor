package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"
)

// S8 step 26: the Compute Engine create signed in with a Google account instead of a service
// account key. The sign-in is the GKE one, run through the discovery sign-in (StartDiscoverSignIn,
// not bound to a cluster): gcloud's client in the browser, an OAuth client of your own, a workforce
// login config, or the build's Google client on iOS. Its state (the secrets and the session the
// GKE sign-in keeps, kubeAuthState) is the credential the app seals, with the quota project;
// gkeMethod.mint turns it into a cloud-platform token, and a state it rotated comes back as
// "credential" from GCPTest and GCPOptions for the app to seal again. Nothing is kept in Go between
// calls but the sign-in run's state, until GCPSignInCredential takes it.

const (
	gcpSignInGcloud    = "gcloud"
	gcpSignInOAuth     = "oauth"
	gcpSignInWorkforce = "workforce"
	gcpSignInNative    = "native"
	gcpSignInTimeout   = 60 * time.Second
)

var errGCPSignInFirst = errors.New("sign in with Google first")

// gcpUser is a client's Google-account side: the sign-in state it mints with, and whether a mint
// changed it (a rotated refresh token, a new access token kept in the session).
type gcpUser struct {
	state   kubeAuthState
	rotated bool
}

// usesGoogleAccount tells a signed-in Google account from a service account key.
func (c gcpCredential) usesGoogleAccount() bool { return c.GoogleSignIn != nil }

// newGCPUserClient is newGCPClient for a signed-in Google account: the project is the quota
// project, and the account must be allowed to use it.
func newGCPUserClient(c gcpCredential) (*gcpClient, error) {
	project := strings.TrimSpace(c.Project)

	switch {
	case len(c.GoogleSignIn.Secrets) == 0:
		return nil, errGCPSignInFirst
	case project == "":
		return nil, errors.New("pick the project the calls are billed to (the quota project)")
	case !gcpProjectPattern.MatchString(project):
		return nil, fmt.Errorf("%q is not a project ID", project)
	}

	zone := strings.ToLower(strings.TrimSpace(c.Zone))
	if zone != "" && !gcpZonePattern.MatchString(zone) {
		return nil, fmt.Errorf("%q is not a Compute Engine zone (europe-west1-b)", zone)
	}

	state := *c.GoogleSignIn
	state.Method = authGKE

	return &gcpClient{project: project, zone: zone, user: &gcpUser{state: state}, account: cmpOr(c.Account, state.User)}, nil
}

// mintUser mints with the Google account's sign-in state, keeping what the mint changed.
func (c *gcpClient) mintUser(ctx context.Context) (string, time.Time, error) {
	token, expiry, next, err := gkeMethod{}.mint(ctx, c.user.state)
	if err != nil {
		var required *errSignInRequired
		if errors.As(err, &required) {
			return "", time.Time{}, fmt.Errorf("it has ended: %s", c.renewHint())
		}

		return "", time.Time{}, err
	}

	if !maps.Equal(next.Secrets, c.user.state.Secrets) || !maps.Equal(next.Session, c.user.state.Session) {
		c.user.state, c.user.rotated = next, true
	}

	return token, expiry, nil
}

// userSecrets are the values of the sign-in state a message must never carry.
func (c *gcpClient) userSecrets() []string {
	if c.user == nil {
		return nil
	}

	var out []string

	for _, v := range c.user.state.Session {
		out = append(out, v)
	}

	if creds, err := parseUserCredentials(c.user.state.secret(gcpFieldUserCredentials)); err == nil {
		out = append(out, creds.RefreshToken, creds.ClientSecret)
	}

	return append(out, c.user.state.secret(gcpFieldOAuthClientSecret), c.user.state.secret(gcpFieldAccessToken))
}

// renewHint is how to sign in again once Google refuses the stored sign-in.
func (c *gcpClient) renewHint() string {
	if c.user == nil {
		return ""
	}

	switch {
	case c.user.state.secret(gcpFieldGcloudSignIn) != "":
		return gcpGcloudRenewHint
	case c.user.state.secret(gcpFieldLoginConfig) != "":
		return "sign in again with the workforce login config from the Google Cloud card"
	}

	return "sign in with Google again from the Google Cloud card"
}

// updated is the credential JSON to seal again when a mint changed the sign-in state, else "".
func (c *gcpClient) updated() string {
	if c.user == nil || !c.user.rotated {
		return ""
	}

	state := c.user.state

	return gcpCredential{Project: c.project, Zone: c.zone, GoogleSignIn: &state, Account: c.account}.encode()
}

// encode is the credential as JSON; plain strings and maps, so it always encodes.
func (c gcpCredential) encode() string {
	data, _ := json.Marshal(c) //nolint:errcheck

	return string(data)
}

// gcpSignInRun is the last GCPSignInStart's secrets: GCPSignInCredential reads the session of
// the discovery sign-in they ran.
var gcpSignInRun struct {
	mu      sync.Mutex
	secrets map[string]string
}

// gcpSignInSecrets are the GKE fields of a sign-in kind: gcloud's client (no field), an OAuth
// client of your own (gcpOAuthClientId, gcpOAuthClientSecret, gcpOAuthRedirectUrl), a workforce
// login config (gcpLoginConfigJson), or the build's Google client on iOS.
func gcpSignInSecrets(kind, secretsJSON string) (map[string]string, error) {
	given := map[string]string{}
	if strings.TrimSpace(secretsJSON) != "" {
		var err error
		if given, err = discoverySecrets(secretsJSON); err != nil {
			return nil, err
		}
	}

	switch kind {
	case gcpSignInGcloud:
		return map[string]string{gcpFieldGcloudSignIn: "1"}, nil
	case gcpSignInOAuth:
		if given[gcpFieldOAuthClientID] == "" {
			return nil, errors.New("enter the OAuth client ID")
		}

		return pick(given, gcpFieldOAuthClientID, gcpFieldOAuthClientSecret, gcpFieldOAuthRedirectURL), nil
	case gcpSignInWorkforce:
		if given[gcpFieldLoginConfig] == "" {
			return nil, errors.New("paste the workforce login config (gcloud iam workforce-pools create-login-config)")
		}

		return pick(given, gcpFieldLoginConfig), nil
	case gcpSignInNative:
		if platform, _ := registeredGoogleSignIn(); platform == googleSignInIOS {
			return map[string]string{gcpFieldGoogleSignIn: googleSignInIOS}, nil
		}

		return nil, errors.New("this build offers no native Google sign-in: use the browser sign-in")
	}

	return nil, fmt.Errorf("%q is not a Google sign-in (gcloud, oauth, workforce or native)", kind)
}

// GCPSignInChoices are the Google sign-ins this build offers for the create, in the order the
// sheet shows them. JSON: ["native"?, "gcloud", "oauth", "workforce"]: native only where the
// build carries its own Google client and Go runs the flow (iOS).
func GCPSignInChoices() (out string, err error) {
	defer maskResult(&out, &err)

	choices := []string{gcpSignInGcloud, gcpSignInOAuth, gcpSignInWorkforce}
	if platform, _ := registeredGoogleSignIn(); platform == googleSignInIOS {
		choices = append([]string{gcpSignInNative}, choices...)
	}

	return toJSON(choices)
}

// GCPSignInStart runs the browser sign-in of kind (GCPSignInChoices) with secretsJSON's fields,
// as the GKE discovery does: the listener gets the prompt (the URL to open, or the code to paste
// back through the run's Complete) and the end. Then GCPSignInCredential takes the result.
func GCPSignInStart(kind, secretsJSON string, listener SignInListener) *SignInRun {
	listener = maskedSignInListener{listener}

	secrets, err := gcpSignInSecrets(strings.TrimSpace(kind), secretsJSON)
	if err != nil {
		run := &SignInRun{cancel: func() {}, callbacks: make(chan string, 1)}

		go finishSignIn(listener, err)

		return run
	}

	gcpSignInRun.mu.Lock()
	gcpSignInRun.secrets = secrets
	gcpSignInRun.mu.Unlock()

	raw, _ := json.Marshal(secrets) //nolint:errcheck // plain strings

	return StartDiscoverSignIn(discoverGKE, string(raw), listener)
}

type gcpSignInResult struct {
	Credential string       `json:"credential"`
	Account    string       `json:"account"`
	Projects   []gcpProject `json:"projects"`
	More       bool         `json:"more"`
}

// GCPSignInCredential takes the session the last GCPSignInStart got (once: the run's state is
// forgotten), and lists the projects the account sees for the quota-project pick. JSON:
// {"credential":<sign-in state, no project yet>,"account","projects":[{"id","name"}],"more"}:
// the app picks a project, sets it in the credential's "project" and tests it with GCPTest.
func GCPSignInCredential() (out string, err error) {
	defer maskResult(&out, &err)

	gcpSignInRun.mu.Lock()
	secrets := gcpSignInRun.secrets
	gcpSignInRun.secrets = nil
	gcpSignInRun.mu.Unlock()

	if secrets == nil {
		return "", errGCPSignInFirst
	}

	state := withDiscoverySession(kubeAuthState{Method: authGKE, Secrets: secrets})
	ForgetDiscoverSignIn()

	if len(state.Session) == 0 && state.secret(gcpFieldUserCredentials) == "" {
		return "", errGCPSignInFirst
	}

	ctx, cancel := context.WithTimeout(context.Background(), gcpSignInTimeout)
	defer cancel()

	token, _, next, err := gkeMethod{}.mint(ctx, state)
	if err != nil {
		return "", err
	}

	ids, more, err := listGCPProjects(ctx, gkeAccount{token: token})
	if err != nil {
		return "", err
	}

	projects := make([]gcpProject, 0, len(ids))
	for _, id := range ids {
		projects = append(projects, gcpProject{ID: id, Name: id})
	}

	account := cmpOr(next.User, state.User, "Google account")
	cred := gcpCredential{GoogleSignIn: &next, Account: account}

	return toJSON(gcpSignInResult{Credential: cred.encode(), Account: account, Projects: projects, More: more})
}
