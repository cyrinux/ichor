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

// Clusters whose kubeconfig signs in through a plugin (EKS, GKE, OIDC…) cannot run it on a
// phone: the app does what the plugin does, in Go. What it needs to keep (a refresh token,
// keys the user typed) is an auth state per cluster, which the app seals and stores through
// the AuthStore it registers: Go never writes it to disk.

// AuthStore keeps the auth state of each cluster (implemented in Kotlin/Swift, sealed like
// the stored configs). key is the cluster's fingerprint.
type AuthStore interface {
	// Load returns the stored state, "" when none.
	Load(key string) string
	// Save stores state; "" deletes it.
	Save(key, state string)
}

// SetAuthStore registers the app's store, once at start.
func SetAuthStore(store AuthStore) {
	kubeAuth.setStore(store)
}

// kubeAuthState is what a cluster's sign-in keeps. Secrets are what the user entered (keys,
// a service account): they go into backups. Session is what a sign-in on this device got
// (refresh and access tokens): it does not, a restored copy racing this one would log both
// out.
type kubeAuthState struct {
	Method  string            `json:"method"`
	Secrets map[string]string `json:"secrets,omitempty"`
	Session map[string]string `json:"session,omitempty"`
	// User is who the sign-in is, for the UI; Expires the current token's end, SessionExpires
	// when a new sign-in will be needed (0 unknown). Unix seconds.
	User           string `json:"user,omitempty"`
	Expires        int64  `json:"expires,omitempty"`
	SessionExpires int64  `json:"sessionExpires,omitempty"`
}

func (s kubeAuthState) session(key string) string { return s.Session[key] }
func (s kubeAuthState) secret(key string) string  { return s.Secrets[key] }

// withSession returns s with the session values set ("" deletes one).
func (s kubeAuthState) withSession(values map[string]string) kubeAuthState {
	out := s
	out.Session = maps.Clone(s.Session)

	if out.Session == nil {
		out.Session = map[string]string{}
	}

	for k, v := range values {
		if v == "" {
			delete(out.Session, k)
		} else {
			out.Session[k] = v
		}
	}

	return out
}

// errSignInRequired is a cluster that needs the user to sign in (again). The apps look for
// the code at the start of the message.
type errSignInRequired struct {
	method string
	reason string
}

// KubeSignInRequired is the code an error starts with when the cluster needs a sign-in.
const KubeSignInRequired = "kube-sign-in-required"

func (e *errSignInRequired) Error() string {
	msg := KubeSignInRequired + ": sign in to this cluster (" + e.method + ")"
	if e.reason != "" {
		msg += ": " + e.reason
	}

	return msg
}

func signInRequired(method, reason string) error {
	return &errSignInRequired{method: method, reason: reason}
}

// signInMethod mints a bearer token for one cluster from its auth state.
type signInMethod interface {
	name() string
	// mint returns a token valid until expiry and the state to store when it changed. With
	// nothing to mint from, it returns errSignInRequired.
	mint(ctx context.Context, state kubeAuthState) (token string, expiry time.Time, updated kubeAuthState, err error)
}

// credentialMethod is a method the user signs in to by entering secrets (keys, a service
// account), checked by minting a token with them.
type credentialMethod interface {
	signInMethod
	// fromSecrets builds the state from what the user entered, or says what is wrong.
	fromSecrets(secrets map[string]string) (kubeAuthState, error)
}

// interactiveMethod is a method the user signs in to in a browser or with a device code.
type interactiveMethod interface {
	signInMethod
	signIn(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error)
}

// tokenRefreshMargin renews a token this long before it expires.
const tokenRefreshMargin = time.Minute

// authTokenSource is the token of one cluster, minted once at a time (concurrent calls wait
// for the same refresh: a rotating refresh token must not be used twice).
type authTokenSource struct {
	key    string
	method signInMethod

	mu     sync.Mutex
	token  string
	expiry time.Time
}

func (s *authTokenSource) get(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Until(s.expiry) > tokenRefreshMargin {
		return s.token, nil
	}

	state := kubeAuth.load(s.key)
	if state.Method != "" && state.Method != s.method.name() {
		return "", signInRequired(s.method.name(), "the stored sign-in is for another method")
	}

	state.Method = s.method.name()

	token, expiry, updated, err := s.method.mint(ctx, state)
	if err != nil {
		return "", err
	}

	updated.Method = s.method.name()
	updated.Expires = expiry.Unix()
	kubeAuth.save(s.key, updated)

	s.token, s.expiry = token, expiry

	return token, nil
}

// invalidate drops the cached token: the API server refused it.
func (s *authTokenSource) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.token, s.expiry = "", time.Time{}
}

// authRegistry holds the store and one token source per cluster, kept across clients: a
// dropped client must not lose a fresh token.
type authRegistry struct {
	mu      sync.Mutex
	store   AuthStore
	sources map[string]*authTokenSource
}

var kubeAuth = &authRegistry{store: newMemAuthStore(), sources: map[string]*authTokenSource{}}

func (r *authRegistry) setStore(store AuthStore) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if store == nil {
		store = newMemAuthStore()
	}

	r.store = store
	r.sources = map[string]*authTokenSource{}
}

func (r *authRegistry) currentStore() AuthStore {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.store
}

func (r *authRegistry) load(key string) kubeAuthState {
	var state kubeAuthState

	raw := r.currentStore().Load(key)
	if raw == "" || json.Unmarshal([]byte(raw), &state) != nil {
		return kubeAuthState{}
	}

	return state
}

func (r *authRegistry) save(key string, state kubeAuthState) {
	raw, err := json.Marshal(state)
	if err != nil {
		return
	}

	r.currentStore().Save(key, string(raw))
}

// source is the token source of key, created on first use; a method change replaces it.
func (r *authRegistry) source(key string, method signInMethod) *authTokenSource {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.sources[key]; ok && s.method.name() == method.name() {
		s.method = method

		return s
	}

	s := &authTokenSource{key: key, method: method}
	r.sources[key] = s

	return s
}

// forgetPrefix drops the states and tokens of the sources whose key starts with prefix.
func (r *authRegistry) forgetPrefix(prefix string) {
	r.mu.Lock()

	var keys []string

	for key := range r.sources {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
			delete(r.sources, key)
		}
	}

	store := r.store
	r.mu.Unlock()

	for _, key := range keys {
		store.Save(key, "")
	}
}

// forget drops key's state and token: signed out.
func (r *authRegistry) forget(key string) {
	r.mu.Lock()
	delete(r.sources, key)
	store := r.store
	r.mu.Unlock()

	store.Save(key, "")
}

// memAuthStore keeps states in memory until the app registers its store (and in tests).
type memAuthStore struct {
	mu     sync.Mutex
	states map[string]string
}

func newMemAuthStore() *memAuthStore { return &memAuthStore{states: map[string]string{}} }

func (m *memAuthStore) Load(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.states[key]
}

func (m *memAuthStore) Save(key, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state == "" {
		delete(m.states, key)
	} else {
		m.states[key] = state
	}
}

// kubeSignInContext is a stored context whose user signs in through a method.
type kubeSignInContext struct {
	key    string
	method signInMethod
	user   *kubeStoreUser
}

// signInContext finds the sign-in method of the named stored context; nil method when its
// user has static credentials.
func signInContext(storedYAML, name string) (kubeSignInContext, error) {
	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return kubeSignInContext{}, err
	}

	ctx, ok := doc.context(name)
	if !ok {
		return kubeSignInContext{}, fmt.Errorf("context %q not found in the kubeconfig", name)
	}

	user, ok := doc.user(ctx.Context.User)
	if !ok {
		return kubeSignInContext{}, fmt.Errorf("kubeconfig user %q not found", ctx.Context.User)
	}

	cluster, _ := doc.cluster(ctx.Context.Cluster)
	out := kubeSignInContext{key: kubeFingerprint(name, cluster), user: user}

	method, _ := kubeAuthMethod(user)

	out.method, err = newSignInMethod(method, user, cluster)

	return out, err
}

// newSignInMethod is the implementation of method for user, nil for static credentials.
func newSignInMethod(method string, user *kubeStoreUser, cluster *kubeStoreCluster) (signInMethod, error) {
	switch method {
	case authCert, authToken:
		return nil, nil
	case authOIDC:
		m, err := newOIDCMethod(user)
		if err != nil {
			return nil, err
		}

		return m, nil
	}

	if factory, ok := extraSignInMethods[method]; ok {
		return factory(user, cluster)
	}

	return nil, fmt.Errorf("signing in with %s is not supported yet", method)
}

// extraSignInMethods are the cloud methods, registered by their files.
var extraSignInMethods = map[string]func(*kubeStoreUser, *kubeStoreCluster) (signInMethod, error){}

// supportedSignIn reports whether the app can sign in with method.
func supportedSignIn(method string) bool {
	if method == authOIDC {
		return true
	}

	_, ok := extraSignInMethods[method]

	return ok
}

// kubeSignInKind is what the app shows to sign in with a method: "browser" (OIDC, device
// code included), or "credentials" with the fields to ask for.
type kubeSignInKind struct {
	Method string `json:"method"`
	Kind   string `json:"kind"`
	// Fields are the secrets a credentials method asks for (the apps localize each name).
	Fields []string `json:"fields,omitempty"`
	// Options are the alternative field sets (AWS: keys, or IAM Identity Center).
	Options [][]string `json:"options,omitempty"`
	// Values are the stored, non-secret fields of the last sign-in (an IAM Identity Center
	// start URL, account and role), so a new one only needs confirming.
	Values map[string]string `json:"values,omitempty"`
	// SignedIn when a usable state is stored; User and Expires describe it.
	SignedIn       bool   `json:"signedIn"`
	User           string `json:"user,omitempty"`
	SessionExpires int64  `json:"sessionExpires,omitempty"`
}

// signInFields lets a credentials method say what it asks for.
type signInFields interface {
	fieldSets() [][]string
}

// signInRemembers lets a credentials method name the fields that are not secrets: the app
// shows them again when the session has to be renewed.
type signInRemembers interface {
	rememberedFields() []string
}

// KubeSignInInfo describes how the named stored context signs in, as a JSON kubeSignInKind,
// or "" when it has static credentials.
func KubeSignInInfo(storedYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(storedYAML, contextName)

	sc, err := signInContext(storedYAML, contextName)
	if err != nil || sc.method == nil {
		return "", err
	}

	info := kubeSignInKind{Method: sc.method.name(), Kind: "browser"}

	if f, ok := sc.method.(signInFields); ok {
		info.Kind = "credentials"
		info.Options = f.fieldSets()

		if len(info.Options) > 0 {
			info.Fields = info.Options[0]
		}
	}

	state := kubeAuth.load(sc.key)
	info.SignedIn = state.Method == sc.method.name() && (len(state.Session) > 0 || len(state.Secrets) > 0)
	info.User, info.SessionExpires = state.User, state.SessionExpires

	if r, ok := sc.method.(signInRemembers); ok && state.Method == sc.method.name() {
		info.Values = pick(state.Secrets, r.rememberedFields()...)
	}

	return toJSON(info)
}

// KubeSetCredentials signs the named stored context in with secrets the user entered
// (secretsJSON, an object of the fields KubeSignInInfo asked for): checked by minting a
// token, then stored.
func KubeSetCredentials(storedYAML, contextName, secretsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	sc, err := signInContext(storedYAML, contextName)
	if err != nil {
		return err
	}

	cm, ok := sc.method.(credentialMethod)
	if !ok {
		return errors.New("this cluster does not sign in with credentials")
	}

	var secrets map[string]string
	if err := json.Unmarshal([]byte(secretsJSON), &secrets); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}

	for k, v := range secrets {
		secrets[k] = strings.TrimSpace(v)
	}

	state, err := cm.fromSecrets(secrets)
	if err != nil {
		return err
	}

	state.Method = cm.name()

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	_, expiry, updated, err := cm.mint(ctx, state)

	// A method that completes in the browser (IAM Identity Center) keeps what was entered
	// and asks for the sign-in: the app starts it on this error.
	var needSignIn *errSignInRequired
	if _, interactive := cm.(interactiveMethod); interactive && errors.As(err, &needSignIn) {
		kubeAuth.forget(sc.key)
		kubeAuth.save(sc.key, state)

		return err
	}

	if err != nil {
		return err
	}

	updated.Method, updated.Expires = cm.name(), expiry.Unix()
	kubeAuth.forget(sc.key)
	kubeAuth.save(sc.key, updated)
	kubeClients.forgetConfig(storedYAML, contextName)

	return nil
}

// KubeSignOut forgets the sign-in of the named stored context (its tokens and secrets).
func KubeSignOut(storedYAML, contextName string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	sc, err := signInContext(storedYAML, contextName)
	if err != nil {
		return err
	}

	kubeAuth.forget(sc.key)
	kubeClients.forgetConfig(storedYAML, contextName)

	return nil
}

// KubeAuthForBackup is a stored auth state without its session (see kubeAuthState): what a
// backup keeps. "" when nothing is left.
func KubeAuthForBackup(stateJSON string) (out string, err error) {
	defer maskErr(&err)

	var state kubeAuthState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return "", fmt.Errorf("auth state: %w", err)
	}

	if len(state.Secrets) == 0 {
		return "", nil
	}

	kept := kubeAuthState{Method: state.Method, Secrets: state.Secrets, User: state.User}

	return toJSON(kept)
}
