package talosmobile

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// Screenshot mode: while enabled, everything the app displays goes through a mask that
// swaps IP addresses, hostnames, domains, context names and user-chosen words for neutral
// fakes (10.0.0.1, cp-1, homelab.lan...). The mapping is reversible so the app can pass
// masked values (context names, node addresses) back in; it lives in memory only and is
// never persisted or logged.

const (
	maskedDomain  = "homelab.lan"
	maskedCluster = "homelab"
	maskedUser    = "user"
	maskedWord    = "redacted"
)

// talosRoleWords may stay visible in a context's user part ("admin@prod" -> "admin@homelab").
var talosRoleWords = []string{"admin", "reader", "operator"}

// genericNames say nothing about the user and are too common to replace inside arbitrary
// text ("default" is also a route destination, "talos" is in image names): contexts,
// context parts and endpoint hosts with these names are left visible.
var genericNames = []string{
	"admin", "api", "cluster", "controlplane", "default", "dev", "endpoint", "homelab", "k8s",
	"kube", "kubernetes", "lab", "lb", "local", "main", "master", "node", "operator", "prod",
	"production", "reader", "staging", "talos", "test", "user", "vip", "worker",
}

// Domains that say nothing about the user.
var publicDomains = []string{"local", "localdomain", "localhost", "cluster.local", "home.arpa"}

// privateDomain matches domains that name the user wherever they appear, even when no
// node reported them (e.g. a Tailscale tailnet in certificate SANs): "<tailnet>.ts.net".
var privateDomain = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9-]*\.ts\.net\b`)

type privacyMask struct {
	mu sync.Mutex
	maskState
}

type maskState struct {
	enabled bool
	words   []string // extra words, lowercase, sorted

	ips     map[string]string // real (canonical) -> fake
	ipsBack map[string]string // fake -> real, as first seen
	nextV4  int
	nextV6  int

	hosts     map[string]string // real hostname (lowercase) -> fake
	hostsBack map[string]string // fake -> real
	targets   map[string]string // real hostname -> the DNS name the talosconfig targets it by
	roleCount map[string]int

	contexts     map[string]string // real -> fake
	contextsBack map[string]string // fake -> real
	contextParts map[string]string // lowercase user/cluster part of "user@cluster" -> fake
	contextCount int

	domains map[string]bool // lowercase
	configs map[string]bool // cacheKey of talosconfigs already learned
	terms   []maskTerm      // built lazily from the maps above, longest first
}

var privacy = &privacyMask{}

// SetPrivacyMask turns screenshot mode on or off. extraWords is a comma-separated list of
// words (case-insensitive) to hide as well, e.g. a username appearing in namespaces.
// Turning the mask off or changing the words forgets the current mapping.
func SetPrivacyMask(enabled bool, extraWords string) {
	privacy.set(enabled, parseMaskWords(extraWords))
}

// PrivacyMaskEnabled reports whether screenshot mode is on.
func PrivacyMaskEnabled() bool {
	return privacy.isEnabled()
}

func parseMaskWords(csv string) []string {
	var words []string

	for w := range strings.SplitSeq(csv, ",") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" && !slices.Contains(words, w) {
			words = append(words, w)
		}
	}

	slices.Sort(words)

	return words
}

func (m *privacyMask) set(enabled bool, words []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if enabled && m.enabled && slices.Equal(words, m.words) {
		return
	}

	m.resetLocked()
	m.enabled = enabled

	if enabled {
		m.words = words
	}
}

func (m *privacyMask) resetLocked() {
	m.maskState = maskState{
		ips: map[string]string{}, ipsBack: map[string]string{},
		hosts: map[string]string{}, hostsBack: map[string]string{}, targets: map[string]string{}, roleCount: map[string]int{},
		contexts: map[string]string{}, contextsBack: map[string]string{}, contextParts: map[string]string{},
		domains: map[string]bool{}, configs: map[string]bool{},
	}
}

func (m *privacyMask) isEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enabled
}

// --- masking ---

// mask masks a result for display: JSON documents have their string values masked
// (keys and numbers untouched), anything else is masked as plain text.
func (m *privacyMask) mask(s string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || s == "" {
		return s
	}

	if json.Valid([]byte(s)) {
		return maskJSONStrings(s, m.maskLocked)
	}

	return m.maskLocked(s)
}

// maskPlain masks s as plain text (log lines, error messages).
func (m *privacyMask) maskPlain(s string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || s == "" {
		return s
	}

	return m.maskLocked(s)
}

func (m *privacyMask) maskLocked(s string) string {
	m.learnPrivateDomainsLocked(s)
	s = replaceTerms(s, m.termsLocked())
	s = replaceIPv4(s, m.fakeIPv4Locked)

	return replaceIPv6(s, m.fakeIPv6Locked)
}

func (m *privacyMask) fakeIPv4Locked(ip string) (string, bool) {
	if !maskableIPv4(ip) {
		return "", false
	}

	if fake, ok := m.ips[ip]; ok {
		return fake, true
	}

	n := m.nextV4
	m.nextV4++
	// 10.0.0.1 ... 10.0.0.254, then 10.0.1.1 ... (never .0 or .255).
	third := n / 254
	fake := fmt.Sprintf("10.%d.%d.%d", third/256%256, third%256, n%254+1)

	m.ips[ip], m.ipsBack[fake] = fake, ip

	return fake, true
}

// maskableIPv4 skips addresses that identify nothing: this-network, loopback, link-local,
// multicast, broadcast and netmasks (255.x).
func maskableIPv4(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}

	b := addr.As4()

	return b[0] != 0 && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() && b[0] < 224
}

func (m *privacyMask) fakeIPv6Locked(candidate string) (string, bool) {
	addr, err := netip.ParseAddr(candidate)
	if err != nil || !addr.Is6() || addr.Is4In6() || addr.Zone() != "" {
		return "", false
	}

	if addr.IsUnspecified() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() {
		return "", false
	}

	key := addr.String()
	if fake, ok := m.ips[key]; ok {
		return fake, true
	}

	m.nextV6++
	fake := fmt.Sprintf("fd00::%x", m.nextV6)

	m.ips[key], m.ipsBack[fake] = fake, candidate

	return fake, true
}

// termsLocked returns the whole-word replacements, longest first. When two sources share a
// word, hosts win over contexts, then context parts, domains and extra words.
func (m *privacyMask) termsLocked() []maskTerm {
	if m.terms != nil {
		return m.terms
	}

	seen := map[string]bool{}
	terms := []maskTerm{}
	add := func(t maskTerm) {
		if t.match != "" && !seen[t.match] {
			seen[t.match] = true
			terms = append(terms, t)
		}
	}

	for real, fake := range m.hosts {
		add(maskTerm{match: real, repl: fake})
	}

	for real, fake := range m.contexts {
		if real != fake {
			add(maskTerm{match: strings.ToLower(real), repl: fake})
		}
	}

	for part, fake := range m.contextParts {
		add(maskTerm{match: part, repl: fake})
	}

	for d := range m.domains {
		add(maskTerm{match: d, repl: maskedDomain, suffixOnly: !strings.Contains(d, ".")})
	}

	for _, w := range m.words {
		add(maskTerm{match: w, repl: maskedWord})
	}

	slices.SortFunc(terms, func(a, b maskTerm) int {
		return cmp.Or(cmp.Compare(len(b.match), len(a.match)), cmp.Compare(a.match, b.match))
	})

	m.terms = terms

	return terms
}

// --- reverse mapping ---

// unmaskContext returns the real context name for a masked one (unknown names pass through).
func (m *privacyMask) unmaskContext(name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if real, ok := m.contextsBack[name]; ok && m.enabled {
		return real
	}

	return name
}

// unmaskNode returns the real node for a masked address or hostname, keeping any port.
func (m *privacyMask) unmaskNode(node string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || node == "" {
		return node
	}

	if real, ok := m.unmaskHostLocked(node); ok {
		return real
	}

	back := func(fake string) (string, bool) {
		real, ok := m.ipsBack[fake]

		return real, ok
	}

	return replaceIPv6(replaceIPv4(node, back), back)
}

// unmaskHostLocked maps a fake hostname, with or without its domain and port, back to the
// name the talosconfig targets the node by (its hostname when it is targeted by IP).
func (m *privacyMask) unmaskHostLocked(node string) (string, bool) {
	host, port := node, ""
	if h, p, err := net.SplitHostPort(node); err == nil {
		host, port = h, p
	}

	short, _, _ := strings.Cut(strings.ToLower(host), ".")

	real, ok := m.hostsBack[short]
	if !ok {
		return "", false
	}

	if target, ok := m.targets[real]; ok {
		real = target
	}

	if port != "" {
		return net.JoinHostPort(real, port), true
	}

	return real, true
}

// unmaskNodes unmasks a comma-separated node list.
func (m *privacyMask) unmaskNodes(nodes string) string {
	parts := strings.Split(nodes, ",")
	for i, p := range parts {
		parts[i] = m.unmaskNode(strings.TrimSpace(p))
	}

	return strings.Join(parts, ",")
}

// --- learning ---

// learnConfig registers a talosconfig's contexts and addresses, in a stable order, so the
// masked values the app holds map back even before any data call made them visible.
func (m *privacyMask) learnConfig(configYAML string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := cacheKey(configYAML, "")
	if !m.enabled || m.configs[key] {
		return
	}

	cfg, err := clientconfig.FromString(configYAML)
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

// learnContextLocked gives a context a neutral name: "homelab", "homelab-2"... with the
// user part of "user@cluster" kept only when it is a Talos role. Generic names stay as they
// are, and fakes never reuse a real name of the config, so a name maps back unambiguously.
func (m *privacyMask) learnContextLocked(name string, realNames []string) {
	if _, ok := m.contexts[name]; ok {
		return
	}

	if slices.Contains(genericNames, strings.ToLower(name)) {
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

		if _, taken := m.contextsBack[fake]; !taken && !slices.Contains(realNames, fake) {
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

	m.roleCount[prefix]++
	fake := fmt.Sprintf("%s-%d", prefix, m.roleCount[prefix])

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
