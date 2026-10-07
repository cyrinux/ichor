package ichorgo

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// Calico's policies: a NetworkPolicy of projectcalico.org (namespaced) or a
// GlobalNetworkPolicy. Both carry the same spec; the app reads them from Calico's CRDs
// (crd.projectcalico.org, its storage with the Kubernetes datastore) or, without them, from
// the Calico API server (projectcalico.org/v3).

// calicoMetadataAnnotation is where the CRDs keep a policy's own annotations and labels
// when calicoctl or the API server wrote it (JSON: {"annotations":{...},"labels":{...}}).
const calicoMetadataAnnotation = "projectcalico.org/metadata"

// calicoPolicyObject is the spec both kinds share.
type calicoPolicyObject struct {
	Metadata kubeMeta `json:"metadata"`
	Spec     struct {
		Tier                   string       `json:"tier"`
		Order                  *float64     `json:"order"`
		Selector               string       `json:"selector"`
		NamespaceSelector      string       `json:"namespaceSelector"`
		ServiceAccountSelector string       `json:"serviceAccountSelector"`
		Types                  []string     `json:"types"`
		Ingress                []calicoRule `json:"ingress"`
		Egress                 []calicoRule `json:"egress"`
	} `json:"spec"`
}

// calicoRule is one rule: what it does (Allow, Deny, Pass, Log) to the traffic its source,
// destination and protocol match.
type calicoRule struct {
	Action      string           `json:"action"`
	Protocol    any              `json:"protocol"` // a name or a number
	NotProtocol any              `json:"notProtocol"`
	ICMP        *calicoICMP      `json:"icmp"`
	NotICMP     *calicoICMP      `json:"notICMP"`
	Source      calicoEntityRule `json:"source"`
	Destination calicoEntityRule `json:"destination"`
	HTTP        *struct {
		Methods []string `json:"methods"`
		Paths   []struct {
			Exact  string `json:"exact"`
			Prefix string `json:"prefix"`
		} `json:"paths"`
	} `json:"http"`
}

type calicoICMP struct {
	Type *int `json:"type"`
	Code *int `json:"code"`
}

// calicoEntityRule names one side of a rule. Its fields add up: the endpoints must match
// every one that is set (the nets AND the selector AND the namespaces…).
type calicoEntityRule struct {
	Nets              []string `json:"nets"`
	NotNets           []string `json:"notNets"`
	Selector          string   `json:"selector"`
	NotSelector       string   `json:"notSelector"`
	NamespaceSelector string   `json:"namespaceSelector"`
	ServiceAccounts   *struct {
		Names    []string `json:"names"`
		Selector string   `json:"selector"`
	} `json:"serviceAccounts"`
	Ports    []any `json:"ports"` // numbers, "8080:9000" ranges or named ports
	NotPorts []any `json:"notPorts"`
	Services *struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"services"`
	Domains []string `json:"domains"`
}

// calicoSubject is who a Calico policy applies to: its selector on the pod's labels and,
// for a global policy, on the namespace's labels and the service account's. The last two
// are rekeyed to the pod's label set (see calicoSelector.rekey), so one lookup serves all.
type calicoSubject struct {
	selector, namespaces, serviceAccounts *calicoSelector
}

func (s calicoSubject) matches(labels labelSet) bool {
	return s.selector.matches(labels) &&
		(s.namespaces == nil || s.namespaces.matches(labels)) &&
		(s.serviceAccounts == nil || s.serviceAccounts.matches(labels))
}

func mapCalicoPolicy(o calicoPolicyObject, global bool) netPolicy {
	s := o.Spec
	p := netPolicy{
		Kind: kindCalicoPolicy, Namespace: o.Metadata.Namespace, Name: o.Metadata.Name,
		Created: unixMilli(o.Metadata.CreationTimestamp), Description: calicoDescription(o.Metadata),
		Tier: s.Tier, Order: s.Order, IngressRules: []netRule{}, EgressRules: []netRule{},
	}

	if global {
		p.Kind, p.Namespace = kindCalicoGlobalPolicy, ""
	}

	subject := calicoSubject{selector: parseCalicoSelector(s.Selector)}
	parts := []string{subject.selector.String()}

	if s.NamespaceSelector != "" && global {
		subject.namespaces = parseCalicoSelector(s.NamespaceSelector).rekey(namespaceLabelKey)
		if ns := subject.namespaces.pins(ciliumNamespaceLabel); ns != "" {
			p.SubjectNS = ns
		} else {
			parts = append(parts, "namespaces: "+s.NamespaceSelector)
		}
	}

	if global && p.SubjectNS == "" {
		p.SubjectNS = subject.selector.pins(calicoNamespaceLabel)
	}

	if s.ServiceAccountSelector != "" {
		subject.serviceAccounts = parseCalicoSelector(s.ServiceAccountSelector).rekey(serviceAccountLabelKey)
		parts = append(parts, "service accounts: "+s.ServiceAccountSelector)
	}

	p.Subject = strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), " | ")

	// Without types: Ingress unless only egress rules; Egress when there are egress rules.
	types := s.Types
	if len(types) == 0 {
		if len(s.Egress) > 0 {
			types = append(types, "Egress")
		}

		if len(s.Ingress) > 0 || len(s.Egress) == 0 {
			types = append(types, "Ingress")
		}
	}

	p.Ingress, p.Egress = slices.Contains(types, "Ingress"), slices.Contains(types, "Egress")
	p.subjects = []policySubject{{selector: subject, ingress: p.Ingress, egress: p.Egress}}

	if p.Ingress {
		for _, r := range s.Ingress {
			p.IngressRules = append(p.IngressRules, r.toRule(true, global))
		}
	}

	if p.Egress {
		for _, r := range s.Egress {
			p.EgressRules = append(p.EgressRules, r.toRule(false, global))
		}
	}

	return p
}

// calicoDescription is the policy's description annotation, on the object or inside the
// metadata annotation the CRDs keep it in.
func calicoDescription(meta kubeMeta) string {
	if d := policyDescription(meta, ""); d != "" {
		return d
	}

	var inner struct {
		Annotations map[string]string `json:"annotations"`
	}

	if raw := meta.Annotations[calicoMetadataAnnotation]; raw != "" && json.Unmarshal([]byte(raw), &inner) == nil {
		return strings.TrimSpace(inner.Annotations["description"])
	}

	return ""
}

// toRule maps a rule: its peer is the source of an ingress rule, the destination of an
// egress one; the ports are always the destination's. The other side only narrows the
// policy's own endpoints, which the app notes as an "other" peer, like the negations.
func (r calicoRule) toRule(ingress, global bool) netRule {
	peer, local := r.Destination, r.Source
	if ingress {
		peer, local = r.Source, r.Destination
	}

	out := netRule{Peers: peer.peers(global), Ports: r.ports()}

	switch strings.ToLower(r.Action) {
	case "deny":
		out.Deny = true
	case "pass", "log":
		out.Action = strings.ToLower(r.Action)
	}

	if narrowing := local.narrowing(ingress); narrowing != "" {
		out.Peers = append(out.Peers, netPeer{Kind: "other", Value: narrowing})
	}

	for _, note := range r.notes() {
		out.Peers = append(out.Peers, netPeer{Kind: "other", Value: note})
	}

	if r.HTTP != nil {
		out.L7 = append(out.L7, r.httpLine())
	}

	return out
}

// notes words what the rule excludes, and the source ports it needs (rarely set).
func (r calicoRule) notes() []string {
	var out []string

	if len(r.Source.Ports) > 0 {
		out = append(out, "source ports "+joinAny(r.Source.Ports))
	}

	if len(r.Source.NotPorts) > 0 {
		out = append(out, "not source ports "+joinAny(r.Source.NotPorts))
	}

	if len(r.Destination.NotPorts) > 0 {
		out = append(out, "not ports "+joinAny(r.Destination.NotPorts))
	}

	if proto := anyString(r.NotProtocol); proto != "" {
		out = append(out, "not protocol "+proto)
	}

	if icmp := r.NotICMP.String(); icmp != "" {
		out = append(out, "not ICMP "+icmp)
	}

	return out
}

// peers names the endpoints an entity rule selects. A rule that selects by labels and by
// nets at once (Calico ANDs them) is one peer in Calico's own words: the app's peers are
// alternatives.
func (e calicoEntityRule) peers(global bool) []netPeer {
	out := []netPeer{}
	pods, hasPods := e.podsPeer(global)
	cidrs := e.cidrPeers()
	accounts := ""

	if e.ServiceAccounts != nil && e.ServiceAccounts.Selector != "" {
		accounts = "service accounts " + e.ServiceAccounts.Selector
	}

	switch {
	case hasPods && (len(cidrs) > 0 || accounts != ""), accounts != "" && len(cidrs) > 0:
		var conjunction []string

		if hasPods {
			conjunction = append(conjunction, pods.calicoText())
		}

		if len(cidrs) > 0 {
			conjunction = append(conjunction, e.netsText())
		}

		if accounts != "" {
			conjunction = append(conjunction, accounts)
		}

		out = append(out, netPeer{Kind: "other", Value: strings.Join(conjunction, " && ")})
	case hasPods:
		out = append(out, pods)
	case accounts != "":
		out = append(out, netPeer{Kind: "other", Value: accounts})
	default:
		out = append(out, cidrs...)
	}

	if e.Services != nil {
		out = append(out, netPeer{Kind: "service", Value: strings.Trim(e.Services.Namespace+"/"+e.Services.Name, "/")})
	}

	for _, d := range e.Domains {
		out = append(out, netPeer{Kind: "fqdn", Value: d})
	}

	return out
}

// cidrPeers are the nets, each minus the notNets; the notNets alone are a note.
func (e calicoEntityRule) cidrPeers() []netPeer {
	if len(e.Nets) == 0 && len(e.NotNets) > 0 {
		return []netPeer{{Kind: "other", Value: "not " + strings.Join(e.NotNets, ", ")}}
	}

	out := make([]netPeer, 0, len(e.Nets))
	for _, n := range e.Nets {
		out = append(out, netPeer{Kind: "cidr", Value: n, Except: e.NotNets})
	}

	return out
}

// netsText words the nets and notNets as one: "10.0.0.0/8 except 10.1.0.0/16".
func (e calicoEntityRule) netsText() string {
	text := strings.Join(e.Nets, ", ")
	if len(e.Nets) > 1 {
		text = "in {" + text + "}"
	}

	if len(e.NotNets) > 0 {
		text = strings.TrimSpace(text + " except " + strings.Join(e.NotNets, ", "))
	}

	return text
}

// podsPeer is the pods an entity rule selects by labels, service account name or namespace,
// if it names any. The selector is completed the way Calico would write it (a notSelector
// is "&& !(…)"); a namespace selector pinned to one name is shown as that namespace.
func (e calicoEntityRule) podsPeer(global bool) (netPeer, bool) {
	selector := e.Selector
	if e.ServiceAccounts != nil && len(e.ServiceAccounts.Names) > 0 {
		names := "'" + strings.Join(e.ServiceAccounts.Names, "', '") + "'"
		selector = joinSelectors(selector, calicoServiceAccountLabel+" in {"+names+"}")
	}

	if e.NotSelector != "" {
		selector = joinSelectors(selector, "!("+e.NotSelector+")")
	}

	if selector == "" && e.NamespaceSelector == "" {
		return netPeer{}, false
	}

	peer := netPeer{Kind: "pods", Selector: selector}

	switch namespaces := parseCalicoSelector(e.NamespaceSelector); {
	case namespaces.isGlobal():
		// global(): host endpoints and global network sets, never a pod.
		return netPeer{Kind: "other", Value: strings.TrimSpace(selector + " global()")}, true
	case e.NamespaceSelector == "":
		peer.Namespace = parseCalicoSelector(selector).pins(calicoNamespaceLabel)
		if peer.Namespace == "" && global {
			peer.Namespace = "*"
		}
	default:
		peer.Namespace = namespaces.pins(calicoNameLabel)
		if peer.Namespace == "" {
			peer.NamespaceSelector = e.NamespaceSelector
		}
	}

	return peer, true
}

// joinSelectors joins two selector expressions, the first in parentheses when it has any
// alternative ("(a || b) && c").
func joinSelectors(left, right string) string {
	switch {
	case left == "":
		return right
	case strings.Contains(left, "||"):
		return "(" + left + ") && " + right
	default:
		return left + " && " + right
	}
}

// calicoText words a pods peer in Calico's idiom, for a conjunction with its nets.
func (p netPeer) calicoText() string {
	text := p.Selector
	if text == "" {
		text = "all()"
	}

	switch {
	case p.NamespaceSelector != "":
		return text + " in namespaces " + p.NamespaceSelector
	case p.Namespace != "" && p.Namespace != "*":
		return text + " in " + p.Namespace
	}

	return text
}

// narrowing words the side of a rule that is the policy's own endpoints, when it restricts
// them ("only to app == 'api'"); "" when it does not. Its ports are read apart.
func (e calicoEntityRule) narrowing(ingress bool) string {
	var parts []string

	if pods, ok := e.podsPeer(false); ok {
		parts = append(parts, pods.calicoText())
	}

	if len(e.Nets) > 0 || len(e.NotNets) > 0 {
		parts = append(parts, e.netsText())
	}

	if e.ServiceAccounts != nil && e.ServiceAccounts.Selector != "" {
		parts = append(parts, "service accounts "+e.ServiceAccounts.Selector)
	}

	if e.Services != nil {
		parts = append(parts, "Service "+strings.Trim(e.Services.Namespace+"/"+e.Services.Name, "/"))
	}

	if len(e.Domains) > 0 {
		parts = append(parts, strings.Join(e.Domains, ", "))
	}

	if len(parts) == 0 {
		return ""
	}

	side := "only from "
	if ingress {
		side = "only to "
	}

	return side + strings.Join(parts, " && ")
}

// ports maps the rule's protocol and destination ports: "8080:9000" is a range, a name a
// named port; ICMP carries its type and code instead.
func (r calicoRule) ports() []netPort {
	protocol := strings.ToUpper(anyString(r.Protocol))
	out := []netPort{}

	if icmp := r.ICMP.String(); icmp != "" {
		family := "v4"
		if protocol == "ICMPV6" {
			family = "v6"
		}

		return append(out, netPort{Protocol: "ICMP" + family, Port: icmp})
	}

	if protocol == "ICMPV6" {
		protocol = "ICMPv6"
	}

	for _, p := range r.Destination.Ports {
		port, endPort := calicoPort(p)
		out = append(out, netPort{Protocol: protocol, Port: port, EndPort: endPort})
	}

	if len(out) == 0 && protocol != "" {
		out = append(out, netPort{Protocol: protocol})
	}

	return out
}

// String is the ICMP type and code: "8", "3/1", "" for none.
func (i *calicoICMP) String() string {
	if i == nil || i.Type == nil {
		return ""
	}

	s := strconv.Itoa(*i.Type)
	if i.Code != nil {
		s += "/" + strconv.Itoa(*i.Code)
	}

	return s
}

// calicoPort reads a port: 80, "80", "8080:9000" (a range) or "http" (a named port).
func calicoPort(v any) (port string, endPort int) {
	s := anyString(v)
	if first, last, ok := strings.Cut(s, ":"); ok {
		if n, err := strconv.Atoi(last); err == nil {
			return first, n
		}
	}

	return s, 0
}

// httpLine words an HTTP match: "HTTP GET|POST /api/* /health".
func (r calicoRule) httpLine() string {
	parts := []string{"HTTP"}

	if len(r.HTTP.Methods) > 0 {
		parts = append(parts, strings.Join(r.HTTP.Methods, "|"))
	}

	for _, p := range r.HTTP.Paths {
		switch {
		case p.Exact != "":
			parts = append(parts, p.Exact)
		case p.Prefix != "":
			parts = append(parts, p.Prefix+"*")
		}
	}

	return strings.Join(parts, " ")
}

func joinAny(values []any) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, anyString(v))
	}

	return strings.Join(parts, ", ")
}

// calicoPolicyName tells whether a policy listed by the Calico API server is Calico's own:
// it also mirrors Kubernetes NetworkPolicies ("knp.default.<name>"), read apart, and
// staged policies, which enforce nothing yet.
func calicoPolicyName(name string) bool {
	return !strings.HasPrefix(name, "knp.") && !strings.HasPrefix(name, "staged:")
}
