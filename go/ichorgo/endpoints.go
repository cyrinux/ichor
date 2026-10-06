package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

const (
	// talosAPIPort is apid's port, the one a talosconfig endpoint without a port means.
	talosAPIPort = 50000
	// A LAN host answers a TCP connect in milliseconds: anything slower is not there.
	scanDialTimeout = 500 * time.Millisecond
	scanConcurrency = 128
	// maxScanHosts bounds a scan to a few /22s, a few seconds on a phone.
	maxScanHosts     = 4096
	probeTimeout     = 6 * time.Second
	probeConcurrency = 16
	// Each phase has its own budget, so a slow sweep does not starve the probes.
	sweepTimeout = 60 * time.Second
	matchTimeout = 60 * time.Second
	// maxScanHostBits: the host part of the widest network scanHosts takes (maxScanHosts
	// addresses): an IPv4 /20, an IPv6 /116.
	maxScanHostBits = 12
)

// errSearchTimeout: the probes ran out of time with nothing found, which is not "no node".
var errSearchTimeout = errors.New("the search took too long: search fewer networks, or add a node's address as an endpoint")

// endpointMatch is a host that answered the Talos API with the credentials of Contexts.
type endpointMatch struct {
	Endpoint string   `json:"endpoint"` // as it goes in a talosconfig: address, or address:port
	Hostname string   `json:"hostname"`
	Version  string   `json:"version"`
	Role     string   `json:"role"` // machine type: controlplane, init or worker ("" when unknown)
	Contexts []string `json:"contexts"`
}

// endpointProbe is what a Talos API endpoint says about itself.
type endpointProbe struct {
	Endpoint string `json:"endpoint"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
	Role     string `json:"role"`
}

// ProbeEndpoint asks node (an endpoint: address or hostname, with an optional port) its
// version with contextName's credentials, e.g. before adding it to the context.
func ProbeEndpoint(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)
	endpoint := normalizeEndpoint(node)

	if !validEndpoint(endpoint) {
		return "", fmt.Errorf("invalid endpoint %q", endpoint)
	}

	_, cfgCtx, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	if slices.Contains(cfgCtx.Endpoints, demoEndpoint) {
		return "", errDemoUnavailable
	}

	probe, err := probeEndpoint(context.Background(), cfgCtx, endpoint)
	if err != nil {
		return "", friendlyErr(err)
	}

	privacy.learnHost(probe.Hostname, probe.Role)

	return toJSON(probe)
}

// SetContextEndpoints returns storedYAML with contextName's endpoints replaced by nodes
// (comma-separated, in order). Its nodes are kept: a context without nodes targets its
// endpoints, so it then targets the new ones, as talosctl would.
func SetContextEndpoints(storedYAML, contextName, nodes string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	contextName, nodes = unmaskTargets(storedYAML, contextName, nodes)

	endpoints, err := parseEndpoints(nodes)
	if err != nil {
		return "", err
	}

	if len(endpoints) == 0 {
		return "", errors.New("a cluster needs at least one endpoint")
	}

	return editContext(storedYAML, contextName, func(c *clientconfig.Context) error {
		c.Endpoints = endpoints

		return nil
	})
}

// AddContextEndpoint returns storedYAML with node (an endpoint found on the network) first
// among contextName's endpoints. The others stay, as they may answer from elsewhere (over a
// VPN). A context without nodes targeted its endpoints: it now targets node, the one known
// to be a cluster member, and node discovery finds the others.
func AddContextEndpoint(storedYAML, contextName, node string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	contextName, node = unmaskTarget(storedYAML, contextName, node)
	endpoint := normalizeEndpoint(node)

	if !validEndpoint(endpoint) {
		return "", fmt.Errorf("invalid endpoint %q", endpoint)
	}

	return editContext(storedYAML, contextName, func(c *clientconfig.Context) error {
		if len(c.Nodes) == 0 {
			c.Nodes = []string{endpointHost(endpoint)}
		}

		// The same endpoint written with or without the default port is listed once.
		c.Endpoints = append([]string{endpoint}, slices.DeleteFunc(slices.Clone(c.Endpoints), func(e string) bool {
			return sameEndpoint(e, endpoint)
		})...)

		return nil
	})
}

// editContext returns storedYAML with a copy of contextName changed by edit, the other
// contexts untouched.
func editContext(storedYAML, contextName string, edit func(*clientconfig.Context) error) (string, error) {
	stored, err := parseTalosconfig(storedYAML)
	if err != nil {
		return "", fmt.Errorf("stored talosconfig: %w", err)
	}

	target, ok := stored.Contexts[contextName]
	if !ok || target == nil {
		return "", fmt.Errorf("context %q not found in the stored talosconfig", contextName)
	}

	updated := *target
	if err := edit(&updated); err != nil {
		return "", err
	}

	contexts := make(map[string]*clientconfig.Context, len(stored.Contexts))
	for name, c := range stored.Contexts {
		contexts[name] = c
	}

	contexts[contextName] = &updated

	result := *stored
	result.Contexts = contexts

	return encodeConfig(&result)
}

// parseEndpoints splits a comma-separated endpoint list, normalized, dropping blanks and
// duplicates.
func parseEndpoints(list string) ([]string, error) {
	var out []string

	for _, e := range strings.Split(list, ",") {
		e = normalizeEndpoint(e)
		if e == "" || slices.Contains(out, e) {
			continue
		}

		if !validEndpoint(e) {
			return nil, fmt.Errorf("invalid endpoint %q", e)
		}

		out = append(out, e)
	}

	return out, nil
}

// normalizeEndpoint writes an endpoint the way talosctl dials it: an IPv6 address bare
// without a port and bracketed with one ("[fd00::1]" alone would be dialed as
// "[[fd00::1]]:50000"), every address in its canonical form so that one address written two
// ways is one endpoint. Anything else is only trimmed, for validEndpoint to judge.
func normalizeEndpoint(s string) string {
	s = strings.TrimSpace(s)

	inner := s
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner = s[1 : len(s)-1]
	}

	if a, err := netip.ParseAddr(inner); err == nil {
		return a.String()
	}

	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return s
	}

	if a, err := netip.ParseAddr(host); err == nil {
		return net.JoinHostPort(a.String(), port)
	}

	return s
}

// validEndpoint: an address or hostname, with an optional port, as talosconfig endpoints are
// (a bracketed IPv6 address without a port too: normalizeEndpoint unwraps it).
func validEndpoint(s string) bool {
	s = normalizeEndpoint(s)

	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}

	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return validHostname(s)
	}

	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return false
	}

	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}

	return validHostname(host)
}

// sameEndpoint: a and b reach the same host and port (no port meaning the Talos API's),
// however their addresses are written.
func sameEndpoint(a, b string) bool {
	return strings.EqualFold(endpointHost(a), endpointHost(b)) && endpointPort(a) == endpointPort(b)
}

func endpointPort(endpoint string) string {
	if _, port, err := net.SplitHostPort(normalizeEndpoint(endpoint)); err == nil {
		return port
	}

	return strconv.Itoa(talosAPIPort)
}

// endpointHost is the address (canonical, never bracketed) or hostname of an endpoint,
// without its port: what a talosconfig node is.
func endpointHost(endpoint string) string {
	endpoint = normalizeEndpoint(endpoint)
	if host, _, err := net.SplitHostPort(endpoint); err == nil {
		return host
	}

	return endpoint
}

// formatEndpoint writes a found host as a talosconfig endpoint: the port only when not the default.
func formatEndpoint(ap netip.AddrPort) string {
	if ap.Port() == talosAPIPort {
		return ap.Addr().String()
	}

	return ap.String()
}
