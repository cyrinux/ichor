package ichorgo

import (
	"slices"
	"strings"
)

// incidentNames names things in an incident summary: the real names, or, anonymised,
// placeholders in the order HistoryExport hands them out (node-1 for a node's address and
// hostname alike, volume-1 for a user volume) and "cluster" for the context.
type incidentNames struct {
	anonymize bool
	cluster   string
	nodes     map[string]string // address or hostname -> placeholder
	// hostTerms replace the addresses, hostnames and context name; volumeTerms the user
	// volume names. Both are sorted longest first, matches lowercase (see replaceTerms).
	hostTerms   []maskTerm
	volumeTerms []maskTerm
	// mask is the summary's own mask, for what no record named (another address, a domain).
	mask *privacyMask
}

func newIncidentNames(contextName string, ring historyRing, anonymize bool) *incidentNames {
	names := &incidentNames{anonymize: anonymize, cluster: contextName}
	if !anonymize {
		return names
	}

	names.cluster = "cluster"
	names.nodes = map[string]string{}
	names.mask = &privacyMask{}
	names.mask.set(true, nil)

	var anon historyAnonymizer

	hosts := map[string]string{}
	volumes := map[string]string{}

	addHost := func(value, placeholder string) {
		value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if value == "" {
			return
		}

		hosts[value] = placeholder

		if short, domain, ok := strings.Cut(value, "."); ok && !isIPLiteral(value) {
			hosts[short] = placeholder
			names.mask.learnDomains(domain)
		}
	}

	// The same walk as exportHistory, so node-N is the same machine in both.
	for _, rec := range ring.records {
		for _, n := range rec.Nodes {
			p := anon.placeholder("node", n.Node)
			names.nodes[n.Node] = p
			addHost(n.Node, p)

			if n.Hostname != "" {
				anon.names["node"][n.Hostname] = p
				names.nodes[n.Hostname] = p
				addHost(n.Hostname, p)
			}
		}

		for _, v := range rec.Volumes {
			if !historySystemVolume(v.Name) {
				volumes[strings.ToLower(v.Name)] = anon.placeholder("volume", v.Name)
			}

			p := anon.placeholder("node", v.Node)
			names.nodes[v.Node] = p
			addHost(v.Node, p)
		}
	}

	addContext(hosts, contextName)

	names.hostTerms = incidentTerms(hosts)
	names.volumeTerms = incidentTerms(volumes)

	return names
}

// addContext replaces the context name, and the parts of "user@cluster" that name someone.
func addContext(terms map[string]string, contextName string) {
	terms[strings.ToLower(contextName)] = "cluster"

	user, cluster, ok := strings.Cut(contextName, "@")
	if !ok {
		return
	}

	for part, repl := range map[string]string{user: maskedUser, cluster: "cluster"} {
		part = strings.ToLower(part)
		if len(part) >= 2 && !slices.Contains(genericNames, part) && !slices.Contains(talosRoleWords, part) {
			terms[part] = repl
		}
	}
}

func isIPLiteral(s string) bool {
	return strings.Trim(s, "0123456789.") == "" || strings.Contains(s, ":")
}

func incidentTerms(m map[string]string) []maskTerm {
	terms := make([]maskTerm, 0, len(m))

	for match, repl := range m {
		if len(match) >= 2 {
			terms = append(terms, maskTerm{match: match, repl: repl})
		}
	}

	sortTerms(terms)

	return terms
}

// node is how the summary names a node: its placeholder, or "hostname (address)".
func (n *incidentNames) node(address, hostname string) string {
	if n.anonymize {
		if p, ok := n.nodes[address]; ok {
			return p
		}

		return n.text(address)
	}

	if hostname == "" || hostname == address {
		return address
	}

	return hostname + " (" + address + ")"
}

// text anonymises free text: known names, then whatever address or domain is left.
func (n *incidentNames) text(s string) string {
	if !n.anonymize || s == "" {
		return s
	}

	s = replaceTerms(s, n.hostTerms)
	s = replaceTerms(s, n.volumeTerms)

	return n.mask.maskPlain(s)
}

// alert names an alert: its title, kept anonymised only when it names no node or cluster
// (user volume names are replaced), else its track.
func (n *incidentNames) alert(a historyAlertSpan) string {
	title := a.Title

	if n.anonymize && title != "" {
		if replaceTerms(title, n.hostTerms) != title {
			title = ""
		} else {
			title = n.text(title)
		}
	}

	switch {
	case title != "":
		return title
	case a.Track != "":
		return "a " + a.Track + " alert"
	default:
		return "an alert"
	}
}
