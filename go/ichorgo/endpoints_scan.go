package ichorgo

import (
	"context"
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

// The network scan behind FindEndpoints: candidate hosts, open apid ports, and which
// context each one answers for.

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
		probe.Hostname = metaHost(msgs[0].GetMetadata())
		probe.Version = msgs[0].GetVersion().GetTag()
	}

	// Best effort: the role only orders the matches.
	if mt, err := safe.StateGetByID[*config.MachineType](ctx, c.COSI, config.MachineTypeID); err == nil {
		probe.Role = mt.MachineType().String()
	}

	return probe, nil
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
