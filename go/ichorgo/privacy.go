package ichorgo

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
	// resets counts how often the mapping was forgotten (mask switched, words changed), so
	// text masked before a reset can be told from text masked after.
	resets int
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
	// namespacesBack maps the masked form of the Kubernetes namespaces handed to the app
	// back to the real ones (see learnNamespaces): extra words cannot be revealed otherwise.
	namespacesBack map[string]string
	// namesBack does the same for the names of pods and workloads (see learnNames).
	namesBack map[string]string
	terms     []maskTerm // built lazily from the maps above, longest first

	// avoid is text no fake may be found in. It makes the fakes unambiguous where they are
	// turned back into the real values (reveal): a fake never equals something real.
	avoid string
}

var privacy = &privacyMask{}

// SetPrivacyMask turns screenshot mode on or off. extraWords is a comma-separated list of
// words (case-insensitive) to hide as well, e.g. a username appearing in namespaces.
// Turning the mask off or changing the words forgets the current mapping.
func SetPrivacyMask(enabled bool, extraWords string) {
	privacy.set(enabled, parseMaskWords(extraWords))
}

func parseMaskWords(csv string) []string {
	var words []string

	for _, w := range splitCSV(csv) {
		if w = strings.ToLower(w); !slices.Contains(words, w) {
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
	m.resets++
	m.maskState = maskState{
		ips: map[string]string{}, ipsBack: map[string]string{},
		hosts: map[string]string{}, hostsBack: map[string]string{}, targets: map[string]string{}, roleCount: map[string]int{},
		contexts: map[string]string{}, contextsBack: map[string]string{}, contextParts: map[string]string{},
		domains: map[string]bool{}, configs: map[string]bool{}, namespacesBack: map[string]string{},
		namesBack: map[string]string{},
	}
}

func (m *privacyMask) isEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enabled
}

// state tells whether the mask is on and which mapping it holds (see resets).
func (m *privacyMask) state() (enabled bool, resets int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enabled, m.resets
}

// setAvoid makes the mask pick its fakes outside text (see maskState.avoid).
func (m *privacyMask) setAvoid(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.avoid = strings.ToLower(text)
}

// takenLocked tells whether a candidate fake already means something.
func (m *privacyMask) takenLocked(fake string) bool {
	return m.avoid != "" && strings.Contains(m.avoid, strings.ToLower(fake))
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

	var fake string

	for fake == "" || m.takenLocked(fake) {
		n := m.nextV4
		m.nextV4++
		// 10.0.0.1 ... 10.0.0.254, then 10.0.1.1 ... (never .0 or .255).
		third := n / 254
		fake = fmt.Sprintf("10.%d.%d.%d", third/256%256, third%256, n%254+1)
	}

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

	var fake string

	for fake == "" || m.takenLocked(fake) {
		m.nextV6++
		fake = fmt.Sprintf("fd00::%x", m.nextV6)
	}

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

	sortTerms(terms)

	m.terms = terms

	return terms
}

// sortTerms puts the longest matches first (then by text), so that a term wins over the
// shorter ones it contains.
func sortTerms(terms []maskTerm) {
	slices.SortFunc(terms, func(a, b maskTerm) int {
		return cmp.Or(cmp.Compare(len(b.match), len(a.match)), cmp.Compare(a.match, b.match))
	})
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
