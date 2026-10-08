package ichorgo

import (
	"cmp"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

// kubeContextSummary is contextSummary for a kubeconfig context, plus what the import
// preview shows: how it signs in, and why it cannot be added when it cannot.
type kubeContextSummary struct {
	contextSummary

	Namespace string `json:"namespace,omitempty"`
	// Auth is how the context signs in: cert, token, or a sign-in method the app recognises
	// (eks, gke, oidc, azure, digitalocean, rancher), exec, auth-provider, basic, none.
	Auth string `json:"auth"`
	// AuthDetail names what the method signs in to (EKS cluster, OIDC issuer), if anything.
	AuthDetail string `json:"authDetail,omitempty"`
	// User is who the credentials are: the certificate's common name or the token's subject.
	User     string `json:"user,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
	// Problem is a kubeProblem code when the context cannot be added; the apps localize it.
	Problem       string `json:"problem,omitempty"`
	ProblemDetail string `json:"problemDetail,omitempty"`
	// SignIn is the method the app signs in with ("" for static credentials); see
	// KubeSignInInfo for how and whether it is signed in.
	SignIn string `json:"signIn,omitempty"`
}

type kubeconfigSummary struct {
	Current  string               `json:"current"`
	Contexts []kubeContextSummary `json:"contexts"`
}

// Why a kubeconfig context cannot be added. The apps map the codes to localized text.
const (
	KubeProblemClusterMissing = "kube-cluster-missing"
	KubeProblemUserMissing    = "kube-user-missing"
	KubeProblemNotHTTPS       = "kube-not-https"
	KubeProblemFilePath       = "kube-file-path"
	KubeProblemProxy          = "kube-proxy-url"
	KubeProblemBasicAuth      = "kube-basic-auth"
	KubeProblemSignInLater    = "kube-sign-in-later"
	KubeProblemExec           = "kube-exec-unsupported"
	KubeProblemNoCredentials  = "kube-no-credentials"
	KubeProblemInvalid        = "kube-invalid"
)

// ParseKubeconfig validates a kubeconfig and returns a JSON kubeconfigSummary: one row per
// context, sorted by name, each importable or carrying the reason it is not. It reads both an
// imported file and the app's stored kubeconfig. No key material leaves.
func ParseKubeconfig(configYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	privacy.learnConfig(configYAML)

	doc, err := loadKubeconfigDoc(configYAML)
	if err != nil {
		return "", err
	}

	summary := kubeconfigSummary{Current: doc.currentName(), Contexts: []kubeContextSummary{}}
	for _, name := range doc.sortedNames() {
		summary.Contexts = append(summary.Contexts, summarizeKubeContext(doc, name))
	}

	return toJSON(summary)
}

func summarizeKubeContext(doc *kubeconfigDoc, name string) kubeContextSummary {
	ctx, _ := doc.context(name)
	cluster, hasCluster := doc.cluster(ctx.Context.Cluster)
	user, hasUser := doc.user(ctx.Context.User)

	s := kubeContextSummary{
		contextSummary: contextSummary{
			Name: name, Kind: kindKube, Fingerprint: kubeFingerprint(name, cluster), ClusterID: kubeClusterID(cluster),
			Endpoints: []string{}, Nodes: []string{}, Roles: []string{},
		},
		Namespace: ctx.Context.Namespace,
		Auth:      authNone,
	}

	if hasCluster {
		s.Endpoints = []string{cluster.Cluster.Server}
		s.Insecure = cluster.Cluster.InsecureSkipTLSVerify
	}

	if hasUser {
		s.Auth, s.AuthDetail = kubeAuthMethod(user)
		s.describeCredentials(user)
	}

	s.Problem, s.ProblemDetail = kubeContextProblem(doc, name, cluster, user)
	if s.Problem == "" && supportedSignIn(s.Auth) {
		s.SignIn = s.Auth
	}

	return s
}

const (
	kindTalos = "talos"
	kindKube  = "kube"

	authCert         = "cert"
	authToken        = "token"
	authBasic        = "basic"
	authNone         = "none"
	authExec         = "exec"
	authAuthProvider = "auth-provider"
	authEKS          = "eks"
	authGKE          = "gke"
	authOIDC         = "oidc"
	authAzure        = "azure"
	authDigitalOcean = "digitalocean"
	authRancher      = "rancher"
)

// kubeAuthMethod names how a user signs in. Static credentials win, as in parseKubeconfig.
func kubeAuthMethod(u *kubeStoreUser) (method, detail string) {
	switch {
	case u.User.ClientCertificateData != "" || u.User.ClientCertificate != "":
		return authCert, ""
	case u.User.Token != "" || u.User.TokenFile != "":
		return authToken, ""
	case u.User.Exec != nil:
		return execAuthMethod(u.User.Exec)
	case u.User.AuthProvider != nil:
		return authProviderMethod(u.User.AuthProvider)
	case u.User.Username != "":
		return authBasic, ""
	}

	return authNone, ""
}

// execAuthMethod recognises the credential plugins of the common clouds and of kubelogin.
func execAuthMethod(e *kubeStoreExec) (method, detail string) {
	cmd := strings.TrimSuffix(path.Base(strings.ReplaceAll(e.Command, `\`, "/")), ".exe")

	switch {
	case cmd == "aws" && slices.Contains(e.Args, "eks"):
		return authEKS, flagValue(e.Args, "--cluster-name", "--cluster-id")
	case cmd == "aws-iam-authenticator":
		return authEKS, flagValue(e.Args, "-i", "--cluster-id")
	case cmd == "gke-gcloud-auth-plugin" || cmd == "gcloud":
		return authGKE, ""
	case cmd == "kubectl-oidc_login" || (cmd == "kubectl" && slices.Contains(e.Args, "oidc-login")):
		return authOIDC, flagValue(e.Args, "--oidc-issuer-url")
	case cmd == "kubelogin" && flagValue(e.Args, "--oidc-issuer-url") != "":
		return authOIDC, flagValue(e.Args, "--oidc-issuer-url")
	case cmd == "kubelogin":
		// Azure's kubelogin (an int128 kubelogin always names its issuer).
		return authAzure, flagValue(e.Args, "--tenant-id", "-t")
	case cmd == "doctl":
		return authDigitalOcean, ""
	case cmd == "rancher":
		return authRancher, flagValue(e.Args, "--server")
	}

	return authExec, cmd
}

func authProviderMethod(p map[string]any) (method, detail string) {
	name, _ := p["name"].(string)
	cfg, _ := p["config"].(map[string]any)

	switch name {
	case "oidc":
		issuer, _ := cfg["idp-issuer-url"].(string)

		return authOIDC, issuer
	case "gcp":
		return authGKE, ""
	case "azure":
		tenant, _ := cfg["tenant-id"].(string)

		return authAzure, tenant
	}

	return authAuthProvider, name
}

// flagValue is the value of the first of names in args, written "--flag value" or "--flag=value".
func flagValue(args []string, names ...string) string {
	for i, arg := range args {
		for _, name := range names {
			if value, ok := strings.CutPrefix(arg, name+"="); ok {
				return value
			}

			if arg == name && i+1 < len(args) {
				return args[i+1]
			}
		}
	}

	return ""
}

// describeCredentials fills who the credentials are and when they expire, as far as they
// tell: a client certificate always does, a token only when it is a JWT.
func (s *kubeContextSummary) describeCredentials(u *kubeStoreUser) {
	if data := u.User.ClientCertificateData; data != "" {
		if cert := firstCertificate(data); cert != nil {
			s.User = cert.Subject.CommonName
			s.Roles = slices.Clone(cert.Subject.Organization)
			s.CertNotAfter = cert.NotAfter.Unix()
		}

		return
	}

	if claims, ok := jwtClaims(u.User.Token); ok {
		s.User = claims.Subject
		s.CertNotAfter = claims.Expires
	}
}

func firstCertificate(data string) *x509.Certificate {
	der := pemCertificateDER(data)
	if der == nil {
		return nil
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil
	}

	return cert
}

type tokenClaims struct {
	Subject string `json:"sub"`
	Expires int64  `json:"exp"`
}

// jwtClaims reads a JWT's subject and expiry without checking its signature: the API server
// does that, the app only shows them.
func jwtClaims(token string) (tokenClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return tokenClaims{}, false
	}

	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}, false
	}

	return claims, true
}

// kubeContextProblem is why the context cannot be added, "" when it can.
func kubeContextProblem(doc *kubeconfigDoc, name string, cluster *kubeStoreCluster, user *kubeStoreUser) (problem, detail string) {
	if cluster == nil {
		return KubeProblemClusterMissing, ""
	}

	if user == nil {
		return KubeProblemUserMissing, ""
	}

	c, u := cluster.Cluster, user.User

	switch server, err := url.Parse(c.Server); {
	case err != nil || server.Scheme != "https" || server.Host == "":
		return KubeProblemNotHTTPS, c.Server
	case c.CertificateAuthority != "" && c.CertificateAuthorityData == "":
		return KubeProblemFilePath, c.CertificateAuthority
	case c.ProxyURL != "":
		return KubeProblemProxy, c.ProxyURL
	}

	switch method, _ := kubeAuthMethod(user); method {
	case authCert:
		if u.ClientCertificateData == "" || u.ClientKeyData == "" {
			return KubeProblemFilePath, cmp.Or(u.ClientCertificate, u.ClientKey)
		}
	case authToken:
		if u.Token == "" {
			return KubeProblemFilePath, u.TokenFile
		}
	case authBasic:
		return KubeProblemBasicAuth, ""
	case authNone:
		return KubeProblemNoCredentials, ""
	case authExec:
		return KubeProblemExec, u.Exec.Command
	default:
		if !supportedSignIn(method) {
			return KubeProblemSignInLater, method
		}

		if _, err := newSignInMethod(method, user, cluster); err != nil {
			return KubeProblemInvalid, err.Error()
		}

		return "", ""
	}

	single, err := doc.single(name)
	if err == nil {
		var data string
		if data, err = encodeKubeconfigDoc(single); err == nil {
			_, err = parseKubeconfig(data)
		}
	}

	if err != nil {
		return KubeProblemInvalid, err.Error()
	}

	return "", ""
}

// KubeImportConflicts is ImportConflicts for a kubeconfig: the contexts of addedYAML named
// like a stored cluster, Talos (talosYAML) or kubeconfig (storedYAML), as JSON
// [{index, suggested, sameAs}] with index the position in ParseKubeconfig's list. Names are
// unique across both stores: the app shows them in one list.
func KubeImportConflicts(storedYAML, talosYAML, addedYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	stored, added, taken, err := loadKubeMergeInputs(storedYAML, talosYAML, addedYAML)
	if err != nil {
		return "", err
	}

	reserved := kubeNames(added)
	conflicts := []importConflict{}

	for i, name := range added.sortedNames() {
		if !taken[name] {
			taken[name] = true

			continue
		}

		suggested := freeName(taken, reserved, name)
		taken[suggested] = true

		conflicts = append(conflicts, importConflict{
			Index:     i,
			Suggested: suggested,
			SameAs:    sameKubeClusterName(stored, name, clusterOf(added, name)),
		})
	}

	return toJSON(conflicts)
}

// MergeKubeconfig returns storedYAML with the importable contexts of addedYAML added (each
// with a cluster and a user of its own), except those choicesJSON skips. Names follow
// MergeConfig: a free name stays, a clash takes the user's name, replaces the stored context
// of the same cluster, or becomes name-N. A context with a problem (see ParseKubeconfig) is
// left out; importing none is an error.
func MergeKubeconfig(storedYAML, talosYAML, addedYAML, choicesJSON string) (out string, err error) {
	// The result is a kubeconfig the app stores: only the error is masked.
	defer maskErr(&err)

	stored, added, taken, err := loadKubeMergeInputs(storedYAML, talosYAML, addedYAML)
	if err != nil {
		return "", err
	}

	choices, err := parseImportChoices(choicesJSON)
	if err != nil {
		return "", err
	}

	merged := *stored
	reserved := kubeNames(added)
	current, imported := "", 0

	for i, name := range added.sortedNames() {
		if choices[i].Skip {
			continue
		}

		if s := summarizeKubeContext(added, name); s.Problem != "" {
			continue
		}

		final, err := kubeMergedName(stored, taken, reserved, name, clusterOf(added, name), choices[i])
		if err != nil {
			return "", err
		}

		single, err := added.single(name)
		if err != nil {
			return "", err
		}

		taken[final] = true
		merged = merged.without(final)
		merged = merged.with(renamedKubeContext(single, final))
		imported++

		if name == added.currentName() || current == "" {
			current = final
		}
	}

	if imported == 0 {
		return "", errors.New("no context of this kubeconfig can be added")
	}

	merged.CurrentContext = current

	return encodeKubeconfigDoc(&merged)
}

func loadKubeMergeInputs(storedYAML, talosYAML, addedYAML string) (stored, added *kubeconfigDoc, taken map[string]bool, err error) {
	stored, err = loadStoredKubeconfig(storedYAML)
	if err != nil {
		return nil, nil, nil, err
	}

	added, err = loadKubeconfigDoc(addedYAML)
	if err != nil {
		return nil, nil, nil, err
	}

	taken = kubeNames(stored)

	if strings.TrimSpace(talosYAML) != "" {
		talos, err := parseTalosconfig(talosYAML)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("stored talosconfig: %w", err)
		}

		for name := range takenNames(talos) {
			taken[name] = true
		}
	}

	return stored, added, taken, nil
}

func kubeNames(doc *kubeconfigDoc) map[string]bool {
	names := make(map[string]bool, len(doc.Contexts))
	for _, c := range doc.Contexts {
		names[c.Name] = true
	}

	return names
}

func clusterOf(doc *kubeconfigDoc, name string) *kubeStoreCluster {
	ctx, ok := doc.context(name)
	if !ok {
		return nil
	}

	cluster, _ := doc.cluster(ctx.Context.Cluster)

	return cluster
}

// kubeMergedName is mergedName for a kubeconfig context.
func kubeMergedName(stored *kubeconfigDoc, taken, reserved map[string]bool, name string, cluster *kubeStoreCluster, choice importChoice) (string, error) {
	if !taken[name] {
		return name, nil
	}

	switch {
	case choice.Replace:
		same := sameKubeClusterName(stored, name, cluster)
		if same == "" {
			return "", fmt.Errorf("context %q is another cluster than the stored one: it cannot replace it", name)
		}

		return same, nil
	case choice.Name != "":
		if taken[choice.Name] || (reserved[choice.Name] && choice.Name != name) {
			return "", fmt.Errorf("a cluster named %q is already imported", choice.Name)
		}

		return choice.Name, nil
	default:
		return freeName(taken, reserved, name), nil
	}
}

// sameKubeClusterName is the stored context named name, or name-N, of the same cluster.
func sameKubeClusterName(stored *kubeconfigDoc, name string, cluster *kubeStoreCluster) string {
	if sameKubeCluster(clusterOf(stored, name), cluster) {
		return name
	}

	for _, candidate := range stored.sortedNames() {
		suffix, ok := strings.CutPrefix(candidate, name+"-")
		if ok && isDigits(suffix) && sameKubeCluster(clusterOf(stored, candidate), cluster) {
			return candidate
		}
	}

	return ""
}

// renamedKubeContext is a single-context kubeconfig with its context, cluster and user named name.
func renamedKubeContext(single *kubeconfigDoc, name string) *kubeconfigDoc {
	out := *single
	out.Clusters = slices.Clone(single.Clusters)
	out.Users = slices.Clone(single.Users)
	out.Contexts = slices.Clone(single.Contexts)

	out.Clusters[0].Name, out.Users[0].Name, out.Contexts[0].Name = name, name, name
	out.Contexts[0].Context.Cluster, out.Contexts[0].Context.User = name, name
	out.CurrentContext = name

	return &out
}

// with returns d plus the entries of single (no name clash: the caller removed it first).
func (d kubeconfigDoc) with(single *kubeconfigDoc) kubeconfigDoc {
	d.Clusters = append(slices.Clone(d.Clusters), single.Clusters...)
	d.Users = append(slices.Clone(d.Users), single.Users...)
	d.Contexts = append(slices.Clone(d.Contexts), single.Contexts...)

	return d
}

// without returns d without the named context and the cluster and user no other context uses.
func (d kubeconfigDoc) without(name string) kubeconfigDoc {
	ctx, ok := d.context(name)
	if !ok {
		return d
	}

	clusterName, userName := ctx.Context.Cluster, ctx.Context.User
	d.Contexts = slices.DeleteFunc(slices.Clone(d.Contexts), func(c kubeStoreContext) bool { return c.Name == name })

	clusterUsed := slices.ContainsFunc(d.Contexts, func(c kubeStoreContext) bool { return c.Context.Cluster == clusterName })
	userUsed := slices.ContainsFunc(d.Contexts, func(c kubeStoreContext) bool { return c.Context.User == userName })

	if !clusterUsed {
		d.Clusters = slices.DeleteFunc(slices.Clone(d.Clusters), func(c kubeStoreCluster) bool { return c.Name == clusterName })
	}

	if !userUsed {
		d.Users = slices.DeleteFunc(slices.Clone(d.Users), func(u kubeStoreUser) bool { return u.Name == userName })
	}

	if d.CurrentContext == name {
		d.CurrentContext = ""
		d.CurrentContext = d.currentName()
	}

	return d
}

// RemoveKubeContext returns storedYAML without contextName, "" once no context is left (the
// app then deletes its stored kubeconfig).
func RemoveKubeContext(storedYAML, contextName string) (out string, err error) {
	// The result is a kubeconfig the app stores: only the error is masked.
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	stored, err := loadStoredKubeconfig(storedYAML)
	if err != nil {
		return "", err
	}

	if _, ok := stored.context(contextName); !ok {
		return "", fmt.Errorf("context %q not found in the stored kubeconfig", contextName)
	}

	remaining := stored.without(contextName)
	if len(remaining.Contexts) == 0 {
		return "", nil
	}

	return encodeKubeconfigDoc(&remaining)
}

// ExportKubeContext returns the named context alone as a kubeconfig, for the user to save:
// a credential, written only where they ask.
func ExportKubeContext(storedYAML, contextName string) (out string, err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	return exportKubeContext(storedYAML, contextName, "")
}

// ExportKubeContextFor is ExportKubeContext with the cluster pointed at kubeServer (the API
// address set for the cluster, see KubePods; "" for the kubeconfig's own), like Kubeconfig
// does for a Talos cluster, so the file works from where the app does.
func ExportKubeContextFor(storedYAML, contextName, kubeServer string) (out string, err error) {
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	return exportKubeContext(storedYAML, contextName, kubeServer)
}

func exportKubeContext(storedYAML, contextName, kubeServer string) (string, error) {
	kubeconfig, err := kubeContextYAML(storedYAML, contextName)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(kubeServer) == "" {
		return kubeconfig, nil
	}

	// A sign-in user (exec, auth-provider) is not one the client reads: the address is
	// computed from the same context with a placeholder user, and written to the real one.
	parseable := kubeconfig
	if _, perr := parseKubeconfig(kubeconfig); perr != nil {
		if parseable, err = kubeContextYAMLWithoutUser(storedYAML, contextName); err != nil {
			return "", err
		}
	}

	return rewriteKubeServer(kubeconfig, parseable, kubeServer)
}
