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
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
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

// FindEndpoints looks for the clusters of configYAML on the networks in cidrs (comma-separated
// IPv4 or IPv6 prefixes): hosts with the Talos API port open (that of the contexts' endpoints, 50000
// by default) are asked their version with each context's credentials. A host only matches
// the contexts whose CA it presents and whose client certificate it accepts, so the result
// is nodes of the talosconfig's clusters, never another Talos on the network. Control plane
// nodes are preferred: only they proxy the API to the other nodes (see preferControlPlanes).
func FindEndpoints(configYAML, cidrs string) (out string, err error) {
	defer maskResult(&out, &err)

	privacy.learnConfig(configYAML)

	cfg, err := loadConfig(configYAML)
	if err != nil {
		return "", err
	}

	hosts, err := scanHosts(cidrs)
	if err != nil {
		return "", err
	}

	contexts := make(map[string]*clientconfig.Context, len(cfg.Contexts))
	for name, c := range cfg.Contexts {
		if c != nil && !slices.Contains(c.Endpoints, demoEndpoint) {
			contexts[name] = c
		}
	}

	sweepCtx, cancelSweep := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancelSweep()

	open := scanPorts(sweepCtx, hosts, contextPorts(contexts), scanDialTimeout)

	matchCtx, cancelMatch := context.WithTimeout(context.Background(), matchTimeout)
	defer cancelMatch()

	matches := preferControlPlanes(matchEndpoints(matchCtx, contexts, open, probeEndpoint))
	if len(matches) == 0 && (sweepCtx.Err() != nil || matchCtx.Err() != nil) {
		return "", errSearchTimeout
	}

	return toJSON(matches)
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
		return "", demoUnavailable
	}

	probe, err := probeEndpoint(context.Background(), cfgCtx, endpoint)
	if err != nil {
		return "", errors.New(friendlyError(err))
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

	return editContext(storedYAML, contextName, func(c *clientconfig.Context) {
		c.Endpoints = endpoints
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

	return editContext(storedYAML, contextName, func(c *clientconfig.Context) {
		if len(c.Nodes) == 0 {
			c.Nodes = []string{endpointHost(endpoint)}
		}

		// The same endpoint written with or without the default port is listed once.
		c.Endpoints = append([]string{endpoint}, slices.DeleteFunc(slices.Clone(c.Endpoints), func(e string) bool {
			return sameEndpoint(e, endpoint)
		})...)
	})
}

// editContext returns storedYAML with a copy of contextName changed by edit, the other
// contexts untouched.
func editContext(storedYAML, contextName string, edit func(*clientconfig.Context)) (string, error) {
	stored, err := parseTalosconfig(storedYAML)
	if err != nil {
		return "", fmt.Errorf("stored talosconfig: %w", err)
	}

	target, ok := stored.Contexts[contextName]
	if !ok || target == nil {
		return "", fmt.Errorf("context %q not found in the stored talosconfig", contextName)
	}

	updated := *target
	edit(&updated)

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

// scanHosts lists the hosts of cidrs (comma-separated IPv4 or IPv6 prefixes), each once,
// without the IPv4 network and broadcast addresses nor the IPv6 subnet-router anycast one.
// An IPv6 network is at most a /116: a /64 holds far too many addresses to sweep.
func scanHosts(cidrs string) ([]netip.Addr, error) {
	seen := map[netip.Addr]bool{}

	var hosts []netip.Addr

	for _, c := range strings.Split(cidrs, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}

		prefix, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, fmt.Errorf("invalid network %q", c)
		}

		prefix = prefix.Masked()
		if prefix.Addr().BitLen()-prefix.Bits() > maxScanHostBits {
			return nil, fmt.Errorf("network %s is too large to scan (at most %d hosts)", c, maxScanHosts)
		}

		for a := prefix.Addr(); prefix.Contains(a); a = a.Next() {
			if notAHost(prefix, a) {
				continue
			}

			if !seen[a] {
				seen[a] = true
				hosts = append(hosts, a)
			}

			if !a.Next().IsValid() {
				break
			}
		}
	}

	if len(hosts) > maxScanHosts {
		return nil, fmt.Errorf("too many hosts to scan (at most %d)", maxScanHosts)
	}

	return hosts, nil
}

// notAHost: a is prefix's IPv4 network or broadcast address, or the subnet-router anycast
// address of its IPv6 /64 (all-zero interface ID), which the router answers.
func notAHost(prefix netip.Prefix, a netip.Addr) bool {
	if a.Is4() {
		return prefix.Bits() <= 30 && (a == prefix.Addr() || !prefix.Contains(a.Next()))
	}

	return !a.Is4In6() && netip.PrefixFrom(a, 64).Masked().Addr() == a
}

// contextPorts lists the ports the contexts' endpoints use: the Talos API default plus any
// endpoint's explicit one.
func contextPorts(contexts map[string]*clientconfig.Context) []uint16 {
	ports := []uint16{talosAPIPort}

	for _, c := range contexts {
		for _, e := range c.Endpoints {
			_, port, err := net.SplitHostPort(e)
			if err != nil {
				continue
			}

			if p, err := strconv.ParseUint(port, 10, 16); err == nil && p > 0 && !slices.Contains(ports, uint16(p)) {
				ports = append(ports, uint16(p))
			}
		}
	}

	return ports
}

// scanPorts returns the host:port pairs that accept a TCP connection within timeout, sorted.
func scanPorts(ctx context.Context, hosts []netip.Addr, ports []uint16, timeout time.Duration) []netip.AddrPort {
	var (
		mu   sync.Mutex
		open []netip.AddrPort
		wg   sync.WaitGroup
	)

	sem := make(chan struct{}, scanConcurrency)
	dialer := net.Dialer{Timeout: timeout}

	for _, host := range hosts {
		for _, port := range ports {
			ap := netip.AddrPortFrom(host, port)

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()

				return sortedAddrPorts(open)
			}

			wg.Add(1)

			go func() {
				defer func() { <-sem; wg.Done() }()

				conn, err := dialer.DialContext(ctx, "tcp", ap.String())
				if err != nil {
					return
				}

				_ = conn.Close() //nolint:errcheck

				mu.Lock()
				open = append(open, ap)
				mu.Unlock()
			}()
		}
	}

	wg.Wait()

	return sortedAddrPorts(open)
}

func sortedAddrPorts(aps []netip.AddrPort) []netip.AddrPort {
	out := slices.Clone(aps)
	slices.SortFunc(out, func(a, b netip.AddrPort) int { return a.Compare(b) })

	return out
}

type probeFunc func(ctx context.Context, cfgCtx *clientconfig.Context, endpoint string) (endpointProbe, error)

// matchEndpoints probes each open host with each context's credentials and keeps those that
// answer, with the contexts they answered for (sorted).
func matchEndpoints(ctx context.Context, contexts map[string]*clientconfig.Context, open []netip.AddrPort, probe probeFunc) []endpointMatch {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		matches = make([]endpointMatch, len(open))
	)

	sem := make(chan struct{}, probeConcurrency)

	for i, ap := range open {
		endpoint := formatEndpoint(ap)
		matches[i].Endpoint = endpoint

		for name, cfgCtx := range contexts {
			wg.Add(1)

			go func() {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}

				var p endpointProbe

				err := safeCall(func() (err error) {
					p, err = probe(ctx, cfgCtx, endpoint)

					return err
				})
				if err != nil {
					return
				}

				mu.Lock()
				defer mu.Unlock()

				matches[i].Contexts = append(matches[i].Contexts, name)
				matches[i].Hostname, matches[i].Version, matches[i].Role = p.Hostname, p.Version, p.Role
			}()
		}
	}

	wg.Wait()

	out := slices.DeleteFunc(matches, func(m endpointMatch) bool { return len(m.Contexts) == 0 })
	for i := range out {
		slices.Sort(out[i].Contexts)
		// Talosconfigs list addresses: the hostname is new to screenshot mode, learnt to be masked.
		privacy.learnHost(out[i].Hostname, out[i].Role)
	}

	return out
}

// preferControlPlanes keeps, for a context with a control plane node among the matches, only
// its control plane nodes: talosctl and the app go through endpoints to reach every node,
// and only control plane nodes proxy the API to the others. A context with only workers
// found keeps them, which beats no endpoint at all (the node itself answers).
func preferControlPlanes(matches []endpointMatch) []endpointMatch {
	hasControlPlane := map[string]bool{}

	for _, m := range matches {
		if isControlPlane(m.Role) {
			for _, c := range m.Contexts {
				hasControlPlane[c] = true
			}
		}
	}

	// Never nil: no match is encoded [], the list the apps decode, not null.
	out := []endpointMatch{}

	for _, m := range matches {
		if !isControlPlane(m.Role) {
			m.Contexts = slices.DeleteFunc(slices.Clone(m.Contexts), func(c string) bool { return hasControlPlane[c] })
		}

		if len(m.Contexts) > 0 {
			out = append(out, m)
		}
	}

	return out
}

func isControlPlane(role string) bool { return role == "controlplane" || role == "init" }

// probeEndpoint calls Version on endpoint itself (no node proxying) with cfgCtx's credentials,
// on a client of its own: the cached sessions are for the stored endpoints.
func probeEndpoint(ctx context.Context, cfgCtx *clientconfig.Context, endpoint string) (endpointProbe, error) {
	single := *cfgCtx
	single.Endpoints = []string{endpoint}
	single.Nodes = nil

	c, err := client.New(ctx, client.WithConfigContext(&single))
	if err != nil {
		return endpointProbe{}, fmt.Errorf("create Talos client: %w", err)
	}

	defer c.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	resp, err := c.Version(ctx)
	if err != nil {
		return endpointProbe{}, err
	}

	probe := endpointProbe{Endpoint: endpoint}
	if msgs := resp.GetMessages(); len(msgs) > 0 {
		probe.Hostname = msgs[0].GetMetadata().GetHostname()
		probe.Version = msgs[0].GetVersion().GetTag()
	}

	// Best effort: the role only orders the matches.
	if mt, err := safe.StateGetByID[*config.MachineType](ctx, c.COSI, config.MachineTypeID); err == nil {
		probe.Role = mt.MachineType().String()
	}

	return probe, nil
}
