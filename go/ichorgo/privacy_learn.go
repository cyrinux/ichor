package ichorgo

import (
	"cmp"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
)

// What the mask learns about a cluster: the real names and addresses its results carry,
// so they can be replaced consistently and mapped back.

// learnConfig registers a talosconfig's contexts and addresses, in a stable order, so the
// masked values the app holds map back even before any data call made them visible.
func (m *privacyMask) learnConfig(configYAML string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := cacheKey(configYAML, "")
	if !m.enabled || m.configs[key] {
		return
	}

	if isKubeconfig(configYAML) {
		m.learnKubeconfigLocked(configYAML)
		m.configs[key] = true

		return
	}

	cfg, err := parseTalosconfig(configYAML)
	if err != nil {
		return
	}

	m.configs[key] = true

	names := sortedContextNames(cfg)

	for _, name := range names {
		m.learnContextLocked(name, names)

		ctx := cfg.Contexts[name]
		for _, target := range slices.Concat(ctx.Endpoints, ctx.Nodes) {
			m.learnTargetLocked(target)
		}
	}
}

// learnKubeconfigLocked is learnConfig for a kubeconfig: its context names and API servers.
func (m *privacyMask) learnKubeconfigLocked(configYAML string) {
	doc, err := loadKubeconfigDoc(configYAML)
	if err != nil {
		return
	}

	names := doc.sortedNames()

	for _, name := range names {
		m.learnContextLocked(name, names)

		if cluster := clusterOf(doc, name); cluster != nil {
			m.learnTargetLocked(cluster.Cluster.Server)
		}
	}
}

// learnContextLocked gives a context a neutral name: "homelab", "homelab-2"... with the
// user part of "user@cluster" kept only when it is a Talos role. Generic names stay as they
// are, and fakes never reuse a real name of the config, so a name maps back unambiguously.
func (m *privacyMask) learnContextLocked(name string, realNames []string) {
	if _, ok := m.contexts[name]; ok {
		return
	}

	// A generic name stays, unless another context (learned from another config: clusters
	// are added over time) is already shown under it.
	if _, shown := m.contextsBack[name]; !shown && slices.Contains(genericNames, strings.ToLower(name)) {
		m.contexts[name] = name

		return
	}

	user, clusterPart, hasUser := strings.Cut(name, "@")

	fakeUser := maskedUser
	if slices.Contains(talosRoleWords, strings.ToLower(user)) {
		fakeUser = strings.ToLower(user)
	}

	var cluster, fake string

	for {
		m.contextCount++

		cluster = maskedCluster
		if m.contextCount > 1 {
			cluster = fmt.Sprintf("%s-%d", maskedCluster, m.contextCount)
		}

		fake = cluster
		if hasUser {
			fake = fakeUser + "@" + cluster
		}

		// Nor a real name already learned (a generic one shown as itself), from any config.
		_, shownAsItself := m.contexts[fake]
		if _, taken := m.contextsBack[fake]; !taken && !shownAsItself && !slices.Contains(realNames, fake) {
			break
		}
	}

	if hasUser {
		m.learnContextPartLocked(user, fakeUser)
		m.learnContextPartLocked(clusterPart, cluster)
	}

	m.contexts[name], m.contextsBack[fake] = fake, name
	m.terms = nil
}

// learnContextPartLocked hides a context's user or cluster name wherever it appears
// (namespaces, machine config), unless it is a generic word.
func (m *privacyMask) learnContextPartLocked(part, fake string) {
	part = strings.ToLower(part)
	if len(part) < 3 || slices.Contains(genericNames, part) || part == fake {
		return
	}

	if _, ok := m.contextParts[part]; !ok {
		m.contextParts[part] = fake
	}
}

// learnTargetLocked registers an endpoint or node: IPs get their fake in config order,
// DNS names are learned as hosts.
func (m *privacyMask) learnTargetLocked(target string) {
	host := target
	if _, rest, ok := strings.Cut(host, "://"); ok {
		host = rest
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	host = strings.Trim(host, "[]")

	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Is4() {
			m.fakeIPv4Locked(addr.String())
		} else {
			m.fakeIPv6Locked(host)
		}

		return
	}

	m.learnHostLocked(host, "node")

	// The fake only stands for the short name: keep the name as written to map it back.
	short, _, _ := strings.Cut(strings.ToLower(strings.TrimSuffix(host, ".")), ".")
	if _, ok := m.targets[short]; !ok && short != "" {
		m.targets[short] = host
	}
}

// hostEntry is a node seen in an overview or member list.
type hostEntry struct {
	address  string
	hostname string
	role     string // controlplane | worker | anything else
}

// learnHosts names new hosts cp-N / worker-N / node-N, in address order so the same
// cluster always gets the same names.
func (m *privacyMask) learnHosts(entries []hostEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return
	}

	sorted := slices.Clone(entries)
	slices.SortStableFunc(sorted, func(a, b hostEntry) int { return compareAddresses(a.address, b.address) })

	for _, e := range sorted {
		if net.ParseIP(e.address) != nil {
			m.learnTargetLocked(e.address)
		}

		m.learnHostLocked(e.hostname, e.role)
	}
}

func compareAddresses(a, b string) int {
	pa, errA := netip.ParseAddr(a)
	pb, errB := netip.ParseAddr(b)

	switch {
	case errA == nil && errB == nil:
		return pa.Compare(pb)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	default:
		return cmp.Compare(a, b)
	}
}

func (m *privacyMask) learnHostLocked(hostname, role string) {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if hostname == "" || hostname == "localhost" || net.ParseIP(hostname) != nil {
		return
	}

	if short, domain, ok := strings.Cut(hostname, "."); ok {
		m.learnDomainLocked(domain)
		hostname = short
	}

	if _, ok := m.hosts[hostname]; ok || len(hostname) < 2 || slices.Contains(genericNames, hostname) {
		return
	}

	prefix := "node"

	switch role {
	case "controlplane":
		prefix = "cp"
	case "worker":
		prefix = "worker"
	}

	var fake string

	for fake == "" || m.takenLocked(fake) {
		m.roleCount[prefix]++
		fake = fmt.Sprintf("%s-%d", prefix, m.roleCount[prefix])
	}

	m.hosts[hostname], m.hostsBack[fake] = fake, hostname
	m.terms = nil
}

func (m *privacyMask) learnHost(hostname, role string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.enabled {
		m.learnHostLocked(hostname, role)
	}
}

func (m *privacyMask) learnDomains(domains ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return
	}

	for _, d := range domains {
		m.learnDomainLocked(d)
	}
}

// learnPrivateDomainsLocked learns the private domains found in s before it is masked.
func (m *privacyMask) learnPrivateDomainsLocked(s string) {
	if !strings.Contains(strings.ToLower(s), ".ts.net") {
		return
	}

	for _, d := range privateDomain.FindAllString(s, -1) {
		m.learnDomainLocked(d)
	}
}

func (m *privacyMask) learnDomainLocked(domain string) {
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	if domain == "" || slices.Contains(publicDomains, domain) || domain == maskedDomain || net.ParseIP(domain) != nil {
		return
	}

	if !m.domains[domain] {
		m.domains[domain] = true
		m.terms = nil
	}
}
