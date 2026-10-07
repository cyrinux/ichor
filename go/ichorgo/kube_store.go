package ichorgo

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Clusters added from a kubeconfig, without Talos, live in one stored kubeconfig next to the
// talosconfig. The app passes it where it passes the talosconfig (configYAML), with the
// context name, so every Kubernetes function works unchanged; the Talos ones refuse it with
// errTalosUnavailable.

var errTalosUnavailable = errors.New("This cluster was added from a kubeconfig: it has no Talos API")

// kubeconfigDoc is a kubeconfig as the app stores it: every context names a cluster and a
// user of its own, all credentials inline. exec and auth-provider are kept as written for
// the sign-in methods to come; contexts using them are not imported yet.
type kubeconfigDoc struct {
	APIVersion     string             `yaml:"apiVersion"`
	Kind           string             `yaml:"kind"`
	CurrentContext string             `yaml:"current-context,omitempty"`
	Clusters       []kubeStoreCluster `yaml:"clusters"`
	Users          []kubeStoreUser    `yaml:"users"`
	Contexts       []kubeStoreContext `yaml:"contexts"`
}

type kubeStoreCluster struct {
	Name    string `yaml:"name"`
	Cluster struct {
		Server                   string `yaml:"server"`
		CertificateAuthority     string `yaml:"certificate-authority,omitempty"`
		CertificateAuthorityData string `yaml:"certificate-authority-data,omitempty"`
		InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify,omitempty"`
		TLSServerName            string `yaml:"tls-server-name,omitempty"`
		ProxyURL                 string `yaml:"proxy-url,omitempty"`
	} `yaml:"cluster"`
}

type kubeStoreUser struct {
	Name string `yaml:"name"`
	User struct {
		ClientCertificate     string         `yaml:"client-certificate,omitempty"`
		ClientCertificateData string         `yaml:"client-certificate-data,omitempty"`
		ClientKey             string         `yaml:"client-key,omitempty"`
		ClientKeyData         string         `yaml:"client-key-data,omitempty"`
		Token                 string         `yaml:"token,omitempty"`
		TokenFile             string         `yaml:"tokenFile,omitempty"`
		Username              string         `yaml:"username,omitempty"`
		Password              string         `yaml:"password,omitempty"`
		Exec                  *kubeStoreExec `yaml:"exec,omitempty"`
		AuthProvider          map[string]any `yaml:"auth-provider,omitempty"`
	} `yaml:"user"`
}

type kubeStoreExec struct {
	APIVersion string   `yaml:"apiVersion,omitempty"`
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args,omitempty"`
	Env        []struct {
		Name  string `yaml:"name"`
		Value string `yaml:"value"`
	} `yaml:"env,omitempty"`
	InteractiveMode    string `yaml:"interactiveMode,omitempty"`
	ProvideClusterInfo bool   `yaml:"provideClusterInfo,omitempty"`
}

type kubeStoreContext struct {
	Name    string `yaml:"name"`
	Context struct {
		Cluster   string `yaml:"cluster"`
		User      string `yaml:"user"`
		Namespace string `yaml:"namespace,omitempty"`
	} `yaml:"context"`
}

// kubeconfigShape tells a kubeconfig from a talosconfig without decoding either: a
// kubeconfig lists its contexts, a talosconfig maps them by name.
type kubeconfigShape struct {
	Kind     string    `yaml:"kind"`
	Clusters yaml.Node `yaml:"clusters"`
	Contexts yaml.Node `yaml:"contexts"`
}

// isKubeconfig reports whether configYAML is a kubeconfig rather than a talosconfig.
func isKubeconfig(configYAML string) bool {
	if !strings.Contains(configYAML, "clusters") {
		return false
	}

	var shape kubeconfigShape
	if err := yaml.Unmarshal([]byte(configYAML), &shape); err != nil {
		return false
	}

	return shape.Kind == "Config" || shape.Clusters.Kind == yaml.SequenceNode || shape.Contexts.Kind == yaml.SequenceNode
}

// IsKubeconfig tells the import screen which flow a pasted, scanned or opened file takes.
func IsKubeconfig(configYAML string) bool {
	return isKubeconfig(configYAML)
}

func loadKubeconfigDoc(configYAML string) (*kubeconfigDoc, error) {
	if strings.TrimSpace(configYAML) == "" {
		return nil, errors.New("kubeconfig is empty")
	}

	var doc kubeconfigDoc
	if err := yaml.Unmarshal([]byte(configYAML), &doc); err != nil {
		return nil, fmt.Errorf("invalid kubeconfig YAML: %w", err)
	}

	if len(doc.Contexts) == 0 {
		return nil, errors.New("kubeconfig has no contexts")
	}

	seen := map[string]bool{}
	for _, c := range doc.Contexts {
		if c.Name == "" {
			return nil, errors.New("kubeconfig has a context without a name")
		}

		if seen[c.Name] {
			return nil, fmt.Errorf("kubeconfig names context %q twice", c.Name)
		}

		seen[c.Name] = true
	}

	return &doc, nil
}

// emptyKubeconfigDoc is the store before the first kubeconfig cluster.
func emptyKubeconfigDoc() *kubeconfigDoc {
	return &kubeconfigDoc{APIVersion: "v1", Kind: "Config"}
}

// loadStoredKubeconfig reads the app's stored kubeconfig, "" being an empty store.
func loadStoredKubeconfig(storedYAML string) (*kubeconfigDoc, error) {
	if strings.TrimSpace(storedYAML) == "" {
		return emptyKubeconfigDoc(), nil
	}

	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return nil, fmt.Errorf("stored kubeconfig: %w", err)
	}

	return doc, nil
}

func encodeKubeconfigDoc(doc *kubeconfigDoc) (string, error) {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("encode kubeconfig: %w", err)
	}

	return string(out), nil
}

func (d *kubeconfigDoc) context(name string) (*kubeStoreContext, bool) {
	for i := range d.Contexts {
		if d.Contexts[i].Name == name {
			return &d.Contexts[i], true
		}
	}

	return nil, false
}

func (d *kubeconfigDoc) cluster(name string) (*kubeStoreCluster, bool) {
	for i := range d.Clusters {
		if d.Clusters[i].Name == name {
			return &d.Clusters[i], true
		}
	}

	return nil, false
}

func (d *kubeconfigDoc) user(name string) (*kubeStoreUser, bool) {
	for i := range d.Users {
		if d.Users[i].Name == name {
			return &d.Users[i], true
		}
	}

	return nil, false
}

// sortedNames lists the contexts by name, the order previews and import indexes use.
func (d *kubeconfigDoc) sortedNames() []string {
	names := make([]string, 0, len(d.Contexts))
	for _, c := range d.Contexts {
		names = append(names, c.Name)
	}

	slices.Sort(names)

	return names
}

// currentName is the current context, or the first one by name when it is unset or dangling.
func (d *kubeconfigDoc) currentName() string {
	if _, ok := d.context(d.CurrentContext); ok {
		return d.CurrentContext
	}

	if names := d.sortedNames(); len(names) > 0 {
		return names[0]
	}

	return ""
}

// single is a kubeconfig holding only the named context, its cluster and user renamed after
// it: the shape the store keeps and what an export writes.
func (d *kubeconfigDoc) single(name string) (*kubeconfigDoc, error) {
	ctx, ok := d.context(name)
	if !ok {
		return nil, fmt.Errorf("context %q not found in the kubeconfig", name)
	}

	cluster, ok := d.cluster(ctx.Context.Cluster)
	if !ok {
		return nil, fmt.Errorf("kubeconfig cluster %q not found", ctx.Context.Cluster)
	}

	user, ok := d.user(ctx.Context.User)
	if !ok {
		return nil, fmt.Errorf("kubeconfig user %q not found", ctx.Context.User)
	}

	out := emptyKubeconfigDoc()
	out.CurrentContext = name

	c, u, x := *cluster, *user, *ctx
	c.Name, u.Name = name, name
	x.Context.Cluster, x.Context.User = name, name
	u.User.AuthProvider = maps.Clone(u.User.AuthProvider)

	out.Clusters = []kubeStoreCluster{c}
	out.Users = []kubeStoreUser{u}
	out.Contexts = []kubeStoreContext{x}

	return out, nil
}

// kubeContextYAML is the named context alone, as a kubeconfig the Kubernetes client reads.
func kubeContextYAML(storedYAML, name string) (string, error) {
	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return "", err
	}

	single, err := doc.single(name)
	if err != nil {
		return "", err
	}

	return encodeKubeconfigDoc(single)
}

// kubeFingerprint is stable for a context name within a cluster (its CA, or its server when
// the kubeconfig has no CA), like contextFingerprint for Talos; the prefix keeps a kube
// context apart from a Talos one of the same name.
func kubeFingerprint(name string, cluster *kubeStoreCluster) string {
	return letterHash(sha256.Sum256([]byte("kube\x00" + name + "\x00" + kubeClusterIdentity(cluster))))
}

// kubeClusterID is the same for every context of a cluster: its CA certificate, else its server.
func kubeClusterID(cluster *kubeStoreCluster) string {
	return letterHash(sha256.Sum256([]byte(kubeClusterIdentity(cluster))))
}

func kubeClusterIdentity(cluster *kubeStoreCluster) string {
	if cluster == nil {
		return ""
	}

	if data := cluster.Cluster.CertificateAuthorityData; data != "" {
		if decoded, err := base64.StdEncoding.DecodeString(data); err == nil {
			if block, _ := pem.Decode(decoded); block != nil {
				return string(block.Bytes)
			}
		}

		return data
	}

	if u, err := url.Parse(cluster.Cluster.Server); err == nil && u.Host != "" {
		return "server\x00" + strings.ToLower(u.Host)
	}

	return "server\x00" + cluster.Cluster.Server
}

// sameKubeCluster: both clusters trust the same CA, or, without one, use the same server.
func sameKubeCluster(a, b *kubeStoreCluster) bool {
	return a != nil && b != nil && kubeClusterIdentity(a) != "" && kubeClusterIdentity(a) == kubeClusterIdentity(b)
}

// kubeContextYAMLWithoutUser is kubeContextYAML with the user's credentials replaced by a
// placeholder token: for a context whose token comes from a sign-in method.
func kubeContextYAMLWithoutUser(storedYAML, name string) (string, error) {
	doc, err := loadKubeconfigDoc(storedYAML)
	if err != nil {
		return "", err
	}

	single, err := doc.single(name)
	if err != nil {
		return "", err
	}

	single.Users[0].User = kubeStoreUser{}.User
	single.Users[0].User.Token = "signed-in"

	return encodeKubeconfigDoc(single)
}
