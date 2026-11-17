package ichorgo

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
)

// A Teleport certificate reaches one Kubernetes cluster (tsh kube login writes one context per
// cluster, all through the same proxy). The sign-in that issued it, who the user is at that
// proxy and the connector used, is kept once per proxy and Teleport cluster under
// teleportIdentityKey: every context of the proxy shows it, and signing in on another one
// reuses its connector (the identity provider's session then usually skips the password).

// teleportIdentityPrefix keeps the shared identities apart from the contexts' fingerprints.
const teleportIdentityPrefix = "teleport:"

// teleportUserValue is the KubeSignInInfo value naming who is signed in at the proxy, for a
// context that has no certificate of its own yet.
const teleportUserValue = "teleportUser"

// teleportKubeLabel is a Kubernetes cluster name the app adds by hand: a DNS label.
var teleportKubeLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// teleportIdentityKey is where the sign-in of proxy (host:port) and teleportCluster is kept.
func teleportIdentityKey(proxy, teleportCluster string) string {
	return teleportIdentityPrefix + proxy + "/" + teleportCluster
}

func (m *teleportMethod) identityKey() string { return teleportIdentityKey(m.proxy, m.teleportCluster) }

// sameTeleport reports whether o signs in at the same proxy and Teleport cluster as m.
func (m *teleportMethod) sameTeleport(o *teleportMethod) bool {
	return m.proxy == o.proxy && m.teleportCluster == o.teleportCluster
}

// teleportUserAuthKey is the identity key of a tsh user, "" for any other user: the apps keep
// it in the auth store with the context (ContextSummary.authKey).
func teleportUserAuthKey(user *kubeStoreUser) string {
	e := user.User.Exec
	if e == nil || execCommandName(e) != "tsh" {
		return ""
	}

	proxy, err := teleportProxyAddr(flagValue(e.Args, "--proxy"))
	if err != nil {
		return ""
	}

	return teleportIdentityKey(proxy, flagValue(e.Args, "--teleport-cluster"))
}

// shareSignIn keeps who signed in, and with which connector, for the proxy's other contexts.
func (m *teleportMethod) shareSignIn(signed kubeAuthState) {
	if signed.User == "" {
		return
	}

	kubeAuth.save(m.identityKey(), kubeAuthState{
		Method:  authTeleport,
		Secrets: map[string]string{teleportConnector: signed.secret(teleportConnector)},
		User:    signed.User,
	})
}

// sharedIdentity is the proxy's shared sign-in, empty when none.
func (m *teleportMethod) sharedIdentity() kubeAuthState {
	id := kubeAuth.load(m.identityKey())
	if id.Method != authTeleport {
		return kubeAuthState{}
	}

	return id
}

// teleportCluster is one Kubernetes cluster of a proxy, as KubeTeleportClusters lists it.
type teleportCluster struct {
	Name     string `json:"name"`
	Context  string `json:"context"`
	SignedIn bool   `json:"signedIn"`
	// Expires is the certificate's end, unix seconds; 0 when not signed in.
	Expires int64 `json:"expires,omitempty"`
}

type teleportClusters struct {
	Proxy           string            `json:"proxy,omitempty"`
	TeleportCluster string            `json:"teleportCluster,omitempty"`
	User            string            `json:"user,omitempty"`
	Clusters        []teleportCluster `json:"clusters"`
}

// teleportContext is a stored context that signs in at a Teleport proxy.
type teleportContext struct {
	name   string
	key    string
	method *teleportMethod
}

// teleportContexts are the stored contexts that sign in with tsh, by name.
func teleportContexts(doc *kubeconfigDoc) []teleportContext {
	var out []teleportContext

	for _, name := range doc.sortedNames() {
		ctx, _ := doc.context(name)

		user, ok := doc.user(ctx.Context.User)
		if !ok || teleportUserAuthKey(user) == "" {
			continue
		}

		cluster, _ := doc.cluster(ctx.Context.Cluster)

		m, err := newTeleportMethod(user, cluster)
		if err != nil {
			continue
		}

		out = append(out, teleportContext{name: name, key: kubeFingerprint(name, cluster), method: m})
	}

	return out
}

// teleportSiblings are the stored Teleport contexts of the same proxy and Teleport cluster as
// the named one, it included, and that one's method.
func teleportSiblings(doc *kubeconfigDoc, contextName string) (*teleportMethod, []teleportContext, error) {
	all := teleportContexts(doc)

	i := slices.IndexFunc(all, func(c teleportContext) bool { return c.name == contextName })
	if i < 0 {
		return nil, nil, errors.New("this cluster does not sign in at a Teleport proxy")
	}

	own := all[i].method

	return own, slices.DeleteFunc(all, func(c teleportContext) bool { return !own.sameTeleport(c.method) }), nil
}

// KubeTeleportClusters lists the Kubernetes clusters of the named context's Teleport proxy that
// the stored kubeconfig reaches, one context each, with whether each has a certificate. JSON
// teleportClusters; the demo cluster has none.
func KubeTeleportClusters(storedYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(storedYAML, contextName)

	if isDemoContext(storedYAML, contextName) {
		return toJSON(teleportClusters{Clusters: []teleportCluster{}})
	}

	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return "", err
	}

	own, siblings, err := teleportSiblings(doc, contextName)
	if err != nil {
		return "", err
	}

	list := teleportClusters{
		Proxy: own.proxy, TeleportCluster: own.teleportCluster, User: own.sharedIdentity().User,
		Clusters: make([]teleportCluster, 0, len(siblings)),
	}

	for _, c := range siblings {
		state := kubeAuth.load(c.key)
		entry := teleportCluster{Name: c.method.kubeCluster, Context: c.name}

		if state.Method == authTeleport && state.session(teleportSessionCert) != "" {
			entry.SignedIn, entry.Expires = true, state.SessionExpires
		}

		list.Clusters = append(list.Clusters, entry)
	}

	return toJSON(list)
}

// teleportAdded is KubeTeleportAddCluster's answer: the kubeconfig to store and the new
// context in it, to sign in next.
type teleportAdded struct {
	Kubeconfig string `json:"kubeconfig"`
	Context    string `json:"context"`
}

// KubeTeleportAddCluster adds a context for the Kubernetes cluster kubeCluster through the
// named context's Teleport proxy: a copy of it whose tsh asks for that cluster, named as tsh
// names it (<teleport cluster>-<kube cluster>). It returns the stored kubeconfig with it, as a
// JSON teleportAdded; the apps store it like an import, then sign the new context in.
func KubeTeleportAddCluster(storedYAML, contextName, kubeCluster string) (out string, err error) {
	// The result is a kubeconfig the app stores: only the error is masked.
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)
	kubeCluster = strings.TrimSpace(kubeCluster)

	if !teleportKubeLabel.MatchString(kubeCluster) {
		return "", errors.New("a Kubernetes cluster name is lowercase letters, digits and dashes, " +
			"starting and ending with a letter or a digit")
	}

	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return "", err
	}

	own, siblings, err := teleportSiblings(doc, contextName)
	if err != nil {
		return "", err
	}

	if i := slices.IndexFunc(siblings, func(c teleportContext) bool { return c.method.kubeCluster == kubeCluster }); i >= 0 {
		return "", fmt.Errorf("the Kubernetes cluster %s is already in the list, as %s", kubeCluster, siblings[i].name)
	}

	name := teleportContextName(doc, own, kubeCluster)

	added, err := teleportContextFor(doc, contextName, name, kubeCluster)
	if err != nil {
		return "", err
	}

	doc.Clusters = append(doc.Clusters, added.Clusters[0])
	doc.Users = append(doc.Users, added.Users[0])
	doc.Contexts = append(doc.Contexts, added.Contexts[0])

	yaml, err := encodeKubeconfigDoc(doc)
	if err != nil {
		return "", err
	}

	return toJSON(teleportAdded{Kubeconfig: yaml, Context: name})
}

// teleportContextName is tsh's name for kubeCluster's context (teleport.ContextName), with a
// -2, -3… suffix when the store already has a context, a cluster or a user of that name.
func teleportContextName(doc *kubeconfigDoc, own *teleportMethod, kubeCluster string) string {
	prefix := own.teleportCluster
	if prefix == "" {
		prefix, _, _ = net.SplitHostPort(own.proxy)
	}

	base := prefix + "-" + kubeCluster

	taken := func(name string) bool {
		_, c := doc.context(name)
		_, k := doc.cluster(name)
		_, u := doc.user(name)

		return c || k || u
	}

	name := base
	for n := 2; taken(name); n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}

	return name
}

// teleportContextFor is the named context alone, renamed to name, with its tsh asking for
// kubeCluster.
func teleportContextFor(doc *kubeconfigDoc, contextName, name, kubeCluster string) (*kubeconfigDoc, error) {
	single, err := doc.single(contextName)
	if err != nil {
		return nil, err
	}

	out := renamedKubeContext(single, name)

	user := out.Users[0]
	exec := *user.User.Exec
	exec.Args = withFlagValue(exec.Args, "--kube-cluster", kubeCluster)
	user.User.Exec = &exec
	out.Users[0] = user

	return out, nil
}

// withFlagValue is args with flag set to value (--flag=value or --flag value), added when
// absent; args itself is left as is.
func withFlagValue(args []string, flag, value string) []string {
	out := slices.Clone(args)

	for i, a := range out {
		switch {
		case a == flag && i+1 < len(out):
			out[i+1] = value

			return out
		case strings.HasPrefix(a, flag+"="):
			out[i] = flag + "=" + value

			return out
		}
	}

	return append(out, flag+"="+value)
}

// KubeTeleportSignOutAll signs out of the named context's Teleport proxy: the shared sign-in
// and the certificate of every stored context of that proxy and Teleport cluster.
func KubeTeleportSignOutAll(storedYAML, contextName string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return err
	}

	own, siblings, err := teleportSiblings(doc, contextName)
	if err != nil {
		return err
	}

	kubeAuth.forget(own.identityKey())

	for _, c := range siblings {
		kubeAuth.forget(c.key)
		kubeClients.forgetConfig(storedYAML, c.name)
	}

	return nil
}
