package ichorgo

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"go.yaml.in/yaml/v4"
)

// NormalizeKubeServer checks a Kubernetes API server address the user typed to use instead
// of the one in the kubeconfig Talos issues (a port forward, a load balancer, a public name)
// and returns it as an https URL: "host", "host:port" and "https://host[:port][/path]" are
// accepted. Without a port, the kubeconfig's one is used. Blank: "" (no override).
func NormalizeKubeServer(input string) (out string, err error) {
	// The result is the address the app stores: only the error is masked.
	defer maskErr(&err)

	u, err := parseKubeServer(input)
	if err != nil || u == nil {
		return "", err
	}

	return u.String(), nil
}

func parseKubeServer(input string) (*url.URL, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	if !strings.Contains(input, "://") {
		input = "https://" + input
	}

	u, err := url.Parse(bracketIPv6Host(input))
	if err != nil {
		return nil, fmt.Errorf("not a valid address: %q", input)
	}

	switch {
	case u.Scheme != "https":
		return nil, errors.New("the Kubernetes API address must use https")
	case u.Hostname() == "":
		return nil, errors.New("the Kubernetes API address has no host")
	case u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return nil, errors.New("the Kubernetes API address takes only a host, a port and a path")
	}

	if strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("the Kubernetes API address has an empty port")
	}

	if port := u.Port(); port != "" {
		if n, err := net.LookupPort("tcp", port); err != nil || n == 0 {
			return nil, fmt.Errorf("bad port %q", port)
		}
	}

	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""

	// One address, one way of writing it: canonical, an IPv6 one bracketed.
	if a, err := netip.ParseAddr(u.Hostname()); err == nil {
		switch {
		case u.Port() != "":
			u.Host = net.JoinHostPort(a.String(), u.Port())
		case a.Is6():
			u.Host = "[" + a.String() + "]"
		default:
			u.Host = a.String()
		}
	}

	return u, nil
}

// bracketIPv6Host brackets the host of rawURL when it is a bare IPv6 address, which a URL
// parser reads as a host and a port ("fd00::1": host "fd00:", port "1"). An IPv6 address
// with a port must be typed bracketed: "fd00::1:6443" is an address.
func bracketIPv6Host(rawURL string) string {
	scheme, rest, _ := strings.Cut(rawURL, "://")

	host, path := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		host, path = rest[:i], rest[i:]
	}

	// A zone ("fe80::1%eth0") is written %25 in a URL.
	if a, err := netip.ParseAddr(host); err == nil && a.Is6() {
		return scheme + "://[" + strings.Replace(host, "%", "%25", 1) + "]" + path
	}

	return rawURL
}

// applyKubeServer points creds at server (see NormalizeKubeServer), with the kubeconfig's
// port (and path) when server has none. The certificate is still checked against the
// kubeconfig's own host, which Talos puts in it: a port forward or a TCP proxy under another
// name works. A kubeconfig that skips verification is refused: its credentials would go to
// whoever answers at the typed address.
func (c *kubeCredentials) applyKubeServer(server string) error {
	u, err := parseKubeServer(server)
	if err != nil || u == nil {
		return err
	}

	if c.tls.InsecureSkipVerify {
		return errors.New("the kubeconfig skips TLS verification: another Kubernetes API address cannot be used with it")
	}

	if u.Path == "" {
		u.Path = c.server.Path
	}

	if u.Port() == "" {
		port := c.server.Port()
		if port == "" {
			port = "443"
		}

		u.Host = net.JoinHostPort(u.Hostname(), port)
	}

	if c.tls.ServerName == "" && u.Hostname() != c.server.Hostname() {
		c.tls.ServerName = c.server.Hostname()
	}

	c.server = u

	return nil
}

// withKubeServer returns kubeconfig with its current context's cluster pointed at server,
// like the app's own client does (see applyKubeServer), so the exported file works from
// where the app does. A blank server leaves it as it is.
func withKubeServer(kubeconfig, server string) (string, error) {
	if strings.TrimSpace(server) == "" {
		return kubeconfig, nil
	}

	creds, err := parseKubeconfig(kubeconfig)
	if err != nil {
		return "", err
	}

	original := creds.tls.ServerName
	if err := creds.applyKubeServer(server); err != nil {
		return "", err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(kubeconfig), &doc); err != nil || len(doc.Content) == 0 {
		return "", errors.New("read kubeconfig: not a YAML document")
	}

	cluster := kubeconfigCluster(doc.Content[0])
	if cluster == nil {
		return "", errors.New("kubeconfig cluster not found")
	}

	setMappingValue(cluster, "server", creds.server.String())

	if creds.tls.ServerName != original {
		setMappingValue(cluster, "tls-server-name", creds.tls.ServerName)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("write kubeconfig: %w", err)
	}

	return string(out), nil
}

// kubeconfigCluster is the "cluster" mapping of the current context's cluster (the first
// context's when unset, as parseKubeconfig reads it).
func kubeconfigCluster(root *yaml.Node) *yaml.Node {
	current := ""
	if v := mappingValue(root, "current-context"); v != nil {
		current = v.Value
	}

	clusterName := ""

	if contexts := mappingValue(root, "contexts"); contexts != nil {
		for i, c := range contexts.Content {
			name := mappingValue(c, "name")
			if i == 0 || (name != nil && name.Value == current) {
				if v := mappingValue(mappingValue(c, "context"), "cluster"); v != nil {
					clusterName = v.Value
				}
			}
		}
	}

	clusters := mappingValue(root, "clusters")
	if clusters == nil {
		return nil
	}

	for _, c := range clusters.Content {
		if name := mappingValue(c, "name"); name != nil && name.Value == clusterName {
			return mappingValue(c, "cluster")
		}
	}

	return nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

func setMappingValue(node *yaml.Node, key, value string) {
	if v := mappingValue(node, key); v != nil {
		v.Kind, v.Tag, v.Value, v.Style = yaml.ScalarNode, "!!str", value, 0

		return
	}

	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}
