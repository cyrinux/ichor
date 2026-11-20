package ichorgo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cyrinux/ichor/go/internal/httpx"
	"golang.org/x/crypto/ssh"
)

// A cluster added from a kubeconfig can be reached through an SSH bastion: every connection to
// its API server (probe, REST, exec, attach, port-forward, VM console) is a direct-tcpip channel
// on one SSH connection, opened by the app (no external tunnel, no listener on the device). TLS
// to the API server is unchanged, so the bastion only sees encrypted bytes.
//
// The setting is a device setting kept in the sealed auth store, under its own key: the
// bastion's host and user, the key (or password) and the pinned host key. A changed host key
// stops every call until the user trusts the new one (KubeSetBastion with hostKey).
//
// Precedence: a cluster with a proxy-url cannot have a bastion (KubeSetBastion refuses it); if a
// stored bastion meets a proxy-url anyway (the kubeconfig was replaced later), the proxy-url wins
// and the bastion is ignored. A sign-in's own HTTP calls (OIDC, cloud token endpoints, a
// Teleport proxy) never go through the bastion: only connections to the API server do.

// bastionMethod is the auth store method of a bastion entry.
const bastionMethod = "ssh-bastion"

// The secrets of a bastion entry; KubeSetBastion takes the same names.
const (
	bastionHost       = "host"
	bastionUser       = "user"
	bastionPrivateKey = "privateKey"
	bastionPassphrase = "passphrase"
	bastionPassword   = "password"
	bastionHostKey    = "hostKey"
)

// Why a context cannot have a bastion (kubeBastionInfo.Unavailable).
const (
	bastionUnavailableProxy = "proxy-url"
	bastionUnavailableDemo  = "demo"
	bastionUnavailableOmni  = "omni"
)

// KubeBastionUnavailable starts the error of KubeSetBastion for a context that cannot have a
// bastion; the reason follows (proxy-url, demo, omni).
const KubeBastionUnavailable = "kube-bastion-unavailable"

// kubeBastionKey is the auth store key of a context's bastion: letters only (masking never
// rewrites it), and not the sign-in's key.
func kubeBastionKey(fingerprint string) string {
	return letterHash(sha256.Sum256([]byte("bastion\x00" + fingerprint)))
}

// bastionTarget is a stored context as far as a bastion is concerned.
type bastionTarget struct {
	key string
	// server is the kubeconfig's API server (host:port), what KubeSetBastion checks it reaches.
	server      string
	unavailable string
	// talos is a talosconfig context: server is its first endpoint's Talos API, and the
	// bastion has a scope (talos_bastion.go).
	talos bool
}

func bastionContext(storedYAML, name string) (bastionTarget, error) {
	if isDemoContext(storedYAML, name) {
		return bastionTarget{unavailable: bastionUnavailableDemo}, nil
	}

	if !isKubeconfig(storedYAML) {
		return talosBastionContext(storedYAML, name)
	}

	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return bastionTarget{}, err
	}

	ctx, ok := doc.context(name)
	if !ok {
		return bastionTarget{}, fmt.Errorf("context %q not found in the kubeconfig", name)
	}

	cluster, _ := doc.cluster(ctx.Context.Cluster)
	out := bastionTarget{key: kubeBastionKey(kubeFingerprint(name, cluster))}

	if cluster == nil {
		return out, nil
	}

	if server, err := url.Parse(cluster.Cluster.Server); err == nil {
		out.server = hostWithPort(server.Host)
	}

	switch {
	case isDemoServer(cluster):
		out.unavailable = bastionUnavailableDemo
	case cluster.Cluster.ProxyURL != "":
		out.unavailable = bastionUnavailableProxy
	}

	return out, nil
}

// loadBastion is the bastion stored under key, if there is one.
func loadBastion(key string) (httpx.BastionConfig, bool) {
	state := kubeAuth.load(key)
	if state.Method != bastionMethod {
		return httpx.BastionConfig{}, false
	}

	return httpx.BastionConfig{
		Host: state.secret(bastionHost), User: state.secret(bastionUser),
		PrivateKey: state.secret(bastionPrivateKey), Passphrase: state.secret(bastionPassphrase),
		Password: state.secret(bastionPassword), HostKey: state.secret(bastionHostKey),
	}, true
}

// bastionState is c as an auth store entry: everything in Secrets, so KubeAuthForBackup keeps
// it as it is.
func bastionState(c httpx.BastionConfig) kubeAuthState {
	secrets := map[string]string{}

	for k, v := range map[string]string{
		bastionHost: c.Host, bastionUser: c.User, bastionPrivateKey: c.PrivateKey,
		bastionPassphrase: c.Passphrase, bastionPassword: c.Password, bastionHostKey: c.HostKey,
	} {
		if v != "" {
			secrets[k] = v
		}
	}

	return kubeAuthState{Method: bastionMethod, User: c.User, Secrets: secrets}
}

// kubeBastionInfo is what the apps show of a bastion: never a secret.
type kubeBastionInfo struct {
	Configured bool   `json:"configured"`
	Host       string `json:"host,omitempty"`
	User       string `json:"user,omitempty"`
	// Auth is "key" or "password".
	Auth               string `json:"auth,omitempty"`
	HostKeyType        string `json:"hostKeyType,omitempty"`
	HostKeyFingerprint string `json:"hostKeyFingerprint,omitempty"`
	// Unavailable says why the context cannot have a bastion: proxy-url, demo, omni.
	Unavailable string `json:"unavailable"`
	// Scope is a talosconfig context's choice (talos_bastion.go): "all" or "kube"; "" for a
	// kubeconfig context.
	Scope string `json:"scope,omitempty"`
}

func describeBastion(c httpx.BastionConfig) kubeBastionInfo {
	info := kubeBastionInfo{Configured: true, Host: httpx.BastionAddress(c.Host), User: c.User, Auth: "password"}
	if c.PrivateKey != "" {
		info.Auth = "key"
	}

	if key, err := httpx.ParseBastionHostKey(c.HostKey); err == nil {
		info.HostKeyType, info.HostKeyFingerprint = key.Type(), ssh.FingerprintSHA256(key)
	}

	return info
}

// KubeBastionInfo says whether the named stored context goes through an SSH bastion, which one
// and with which pinned host key, and why it cannot have one when it cannot.
func KubeBastionInfo(storedYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(storedYAML, contextName)

	t, err := bastionContext(storedYAML, contextName)
	if err != nil {
		return "", err
	}

	info := kubeBastionInfo{Unavailable: t.unavailable}

	if t.key != "" {
		if c, ok := loadBastion(t.key); ok {
			privacy.learnTarget(c.Host)

			described := describeBastion(c)
			described.Unavailable = t.unavailable
			info = described
		}
	}

	if t.talos {
		info.Scope = loadBastionScope(t.key)
	}

	return toJSON(info)
}

// kubeBastionHostKey is the key a bastion presents, for the first-trust and replace-pin screens.
type kubeBastionHostKey struct {
	Host               string `json:"host"`
	HostKeyType        string `json:"hostKeyType"`
	HostKeyFingerprint string `json:"hostKeyFingerprint"`
	// HostKey is the key as one authorized_keys line: what KubeSetBastion pins.
	HostKey string `json:"hostKey"`
}

// KubeBastionHostKey reads the host key of the bastion at host (host[:port], 22 by default)
// without signing in, for the user to compare and trust.
func KubeBastionHostKey(host string) (out string, err error) {
	defer maskResult(&out, &err)

	host = unmaskBastionHost(host)
	privacy.learnTarget(host)

	key, err := httpx.BastionHostKey(context.Background(), host, kubeProbeTimeout)
	if err != nil {
		return "", err
	}

	return toJSON(kubeBastionHostKey{
		Host: httpx.BastionAddress(host), HostKeyType: key.Type(), HostKeyFingerprint: ssh.FingerprintSHA256(key),
		HostKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
	})
}

// unmaskBastionHost is host with a masked name or address mapped back (the apps show the
// masked one in screenshot mode), its port kept.
func unmaskBastionHost(host string) string {
	host = strings.TrimSpace(host)

	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return privacy.unmaskNode(host)
	}

	return net.JoinHostPort(privacy.unmaskNode(name), port)
}

// KubeSetBastion sets the SSH bastion of the named stored context from bastionJSON (host, user,
// privateKey, passphrase, password, hostKey, and scope "all" or "kube" for a talosconfig
// context), after checking it for real: the pinned host key, the sign-in and a channel to the
// API server (the first Talos endpoint with scope all; none with scope kube). With a bastion
// already set, an empty field keeps the stored value (an edit without retyping the key; a new
// pin alone replaces the pin). It returns the KubeBastionInfo JSON.
func KubeSetBastion(storedYAML, contextName, bastionJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(storedYAML, contextName)

	t, err := bastionContext(storedYAML, contextName)
	if err != nil {
		return "", err
	}

	if t.unavailable != "" {
		return "", errors.New(KubeBastionUnavailable + ": this cluster cannot go through an SSH bastion (" + t.unavailable + ")")
	}

	c, err := bastionFromJSON(bastionJSON, t.key)
	if err != nil {
		return "", err
	}

	state := bastionState(c)
	info := describeBastion(c)

	if t.talos {
		scope, err := bastionScopeFromJSON(bastionJSON, t.key)
		if err != nil {
			return "", err
		}

		if err := checkTalosBastion(c, scope, t.server); err != nil {
			return "", err
		}

		state.Secrets[bastionScopeField], info.Scope = scope, scope
	} else if err := checkBastion(c, t.server); err != nil {
		return "", err
	}

	privacy.learnTarget(c.Host)
	kubeAuth.forget(t.key)
	kubeAuth.save(t.key, state)
	forgetBastionClients(storedYAML, contextName, t.key)

	return toJSON(info)
}

// bastionScopeFromJSON is the scope bastionJSON asks for, over the one stored under key.
func bastionScopeFromJSON(bastionJSON, key string) (string, error) {
	var in map[string]string
	if err := json.Unmarshal([]byte(bastionJSON), &in); err != nil {
		return "", fmt.Errorf("SSH bastion: %w", err)
	}

	stored := ""
	if _, ok := loadBastion(key); ok {
		stored = loadBastionScope(key)
	}

	return parseBastionScope(in[bastionScopeField], stored)
}

// forgetBastionClients drops the clients that use (or bypass) the bastion under key: the
// context's Kubernetes clients, and the Talos sessions of a talosconfig context. The next
// call opens them with the new setting.
func forgetBastionClients(storedYAML, contextName, key string) {
	kubeClients.forgetConfig(storedYAML, contextName)
	sessions.forgetAuth(key)
}

// bastionFromJSON reads what the user entered over the bastion stored under key, and validates
// it.
func bastionFromJSON(bastionJSON, key string) (httpx.BastionConfig, error) {
	var in map[string]string
	if err := json.Unmarshal([]byte(bastionJSON), &in); err != nil {
		return httpx.BastionConfig{}, fmt.Errorf("SSH bastion: %w", err)
	}

	// Only what has no meaning with spaces around it is trimmed: a password is kept as typed.
	c := httpx.BastionConfig{
		Host: unmaskBastionHost(in[bastionHost]), User: strings.TrimSpace(in[bastionUser]),
		PrivateKey: strings.TrimSpace(in[bastionPrivateKey]), Passphrase: in[bastionPassphrase],
		Password: in[bastionPassword], HostKey: strings.TrimSpace(in[bastionHostKey]),
	}

	if stored, ok := loadBastion(key); ok {
		c = mergeBastion(c, stored)
	}

	switch {
	case c.Host == "":
		return c, errors.New("the SSH bastion needs a host")
	case c.User == "":
		return c, errors.New("the SSH bastion needs a user name")
	case c.PrivateKey != "" && c.Password != "":
		return c, errors.New("the SSH bastion takes a private key or a password, not both")
	case c.PrivateKey == "" && c.Password == "":
		return c, errors.New("the SSH bastion needs a private key or a password")
	}

	c.Host = httpx.BastionAddress(c.Host)

	return c, nil
}

// mergeBastion fills what the user left empty from the stored bastion.
func mergeBastion(c, stored httpx.BastionConfig) httpx.BastionConfig {
	if c.Host == "" {
		c.Host = stored.Host
	}

	if c.User == "" {
		c.User = stored.User
	}

	if c.HostKey == "" {
		c.HostKey = stored.HostKey
	}

	// No new credential: the stored one, whichever it is.
	if c.PrivateKey == "" && c.Password == "" {
		c.PrivateKey, c.Passphrase, c.Password = stored.PrivateKey, stored.Passphrase, stored.Password
	}

	// The same key again without its passphrase: the stored passphrase.
	if c.PrivateKey != "" && c.PrivateKey == stored.PrivateKey && c.Passphrase == "" {
		c.Passphrase = stored.Passphrase
	}

	return c
}

// checkBastion connects with the pin, signs in and opens (then closes) one channel to server.
func checkBastion(c httpx.BastionConfig, server string) error {
	d, err := httpx.NewBastionDialer(c, kubeProbeTimeout)
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck

	if server == "" {
		return errors.New("the kubeconfig names no API server")
	}

	conn, err := d.DialContext(context.Background(), "tcp", server)
	if err != nil {
		return err
	}

	return conn.Close()
}

// KubeRemoveBastion forgets the SSH bastion of the named stored context: it connects directly
// again.
func KubeRemoveBastion(storedYAML, contextName string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	t, err := bastionContext(storedYAML, contextName)
	if err != nil {
		return err
	}

	if t.key != "" {
		kubeAuth.forget(t.key)
	}

	forgetBastionClients(storedYAML, contextName, t.key)

	return nil
}

// bastionHop is a bastion dialer shared by the clients a context's probe opens (one SSH
// connection for every candidate): the last client closed closes it.
type bastionHop struct {
	*httpx.BastionDialer

	refs atomic.Int32
}

func (h *bastionHop) retain() *bastionHop {
	h.refs.Add(1)

	return h
}

func (h *bastionHop) release() {
	if h.refs.Add(-1) == 0 {
		_ = h.Close()
	}
}

// attachBastion gives creds the bastion stored for the context whose fingerprint this is, if
// any; a proxy-url wins over it.
func attachBastion(creds *kubeCredentials, fingerprint string) error {
	return attachBastionKey(creds, kubeBastionKey(fingerprint))
}

// attachBastionKey is attachBastion for the bastion stored under key ("" for none).
func attachBastionKey(creds *kubeCredentials, key string) error {
	if creds.proxy != nil || key == "" {
		return nil
	}

	c, ok := loadBastion(key)
	if !ok {
		return nil
	}

	d, err := httpx.NewBastionDialer(c, kubeProbeTimeout)
	if err != nil {
		return err
	}

	privacy.learnTarget(c.Host)
	creds.bastion = &bastionHop{BastionDialer: d}

	return nil
}

// streamDialer opens the TCP connections a client makes outside its http.Client: a proxy's or
// a bastion's.
type streamDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// hopRelease is the release of a client's bastion, once however many times the client closes.
type hopRelease struct {
	once sync.Once
	hop  *bastionHop
}

func (r *hopRelease) release() {
	if r == nil {
		return
	}

	r.once.Do(r.hop.release)
}

// hopFailure is err's bastion failure when it is not about one address (a refused forward is),
// nil otherwise.
func hopFailure(err error) *httpx.BastionError {
	var bastionErr *httpx.BastionError
	if errors.As(err, &bastionErr) && bastionErr.Code != httpx.BastionCodeForward {
		return bastionErr
	}

	return nil
}

// asBastionError is err's SSH bastion failure, nil when it is none.
func asBastionError(err error) error {
	var bastionErr *httpx.BastionError
	if errors.As(err, &bastionErr) {
		return bastionErr
	}

	return nil
}
