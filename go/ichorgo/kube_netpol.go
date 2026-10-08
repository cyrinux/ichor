package ichorgo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The policy kinds the app reads.
const (
	kindNetworkPolicy       = "NetworkPolicy"
	kindCiliumPolicy        = "CiliumNetworkPolicy"
	kindCiliumClusterPolicy = "CiliumClusterwideNetworkPolicy"
	// Calico's own NetworkPolicy shares its name with the Kubernetes one: its group tells them apart.
	kindCalicoPolicy       = "NetworkPolicy.projectcalico.org"
	kindCalicoGlobalPolicy = "GlobalNetworkPolicy"
)

// netPolicy is a network policy as the app shows it, whatever its kind: who it applies to
// and, per direction, whether it isolates them and what it lets through or denies.
type netPolicy struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"` // "" for a cluster-wide policy
	Name      string `json:"name"`
	Created   int64  `json:"created"`
	// Subject is who the policy applies to: "" = every pod (of the namespace for a namespaced
	// policy), else a selector ("app=db"); Nodes is set for a Cilium host policy.
	Subject     string `json:"subject"`
	SubjectNS   string `json:"subjectNamespace,omitempty"` // a cluster-wide policy pinned to a namespace
	Nodes       bool   `json:"nodes,omitempty"`
	Description string `json:"description,omitempty"`
	// Tier and Order rank a Calico policy: lower orders apply first, in the tier's turn.
	Tier  string   `json:"tier,omitempty"`
	Order *float64 `json:"order,omitempty"`
	// Ingress / Egress isolate the selected pods when true: only what the rules allow passes.
	Ingress      bool      `json:"ingress"`
	Egress       bool      `json:"egress"`
	IngressRules []netRule `json:"ingressRules"`
	EgressRules  []netRule `json:"egressRules"`
	// Pods the policy selects now ("namespace/name"), at most netPolicyMaxPods, and their count.
	Pods     []string `json:"pods"`
	PodCount int      `json:"podCount"`

	subjects []policySubject // what selects the pods (several for a Cilium policy with specs)
}

// policySubject is one selector of a policy and the directions it isolates: the specs of a
// Cilium policy each have their own.
type policySubject struct {
	selector        endpointMatcher
	ingress, egress bool
}

// endpointMatcher tells whether a policy's selector picks an endpoint with these labels: a
// Kubernetes or Cilium LabelSelector, or a Calico selector expression.
type endpointMatcher interface {
	matches(labelSet) bool
}

// netRule is one rule of a direction: traffic from/to any of Peers on any of Ports passes
// (or is denied when Deny). No peers means any peer, no ports any port; a "none" peer
// means the rule matches nothing (Cilium's empty rule, a deny-all). Action is set for a
// Calico rule that neither allows nor denies: "pass" hands the traffic to the next tier or
// the profiles (which allow it), "log" only logs it.
type netRule struct {
	Deny   bool      `json:"deny,omitempty"`
	Action string    `json:"action,omitempty"`
	Peers  []netPeer `json:"peers"`
	Ports  []netPort `json:"ports"`
	L7     []string  `json:"l7,omitempty"` // "HTTP GET /api", "DNS *.example.org"
}

// netPeer is one side of a rule. Kind is one of:
//   - pods: Selector ("" = all) in Namespace ("" = the policy's own namespace, or any for a
//     cluster-wide policy; "*" = any namespace), or in the namespaces NamespaceSelector picks
//   - namespaces: every pod of the namespaces NamespaceSelector picks ("" = all)
//   - cidr: Value with Except
//   - entity: Value (world, cluster, host, remote-node, kube-apiserver, all…)
//   - fqdn: Value (a name or a pattern, "*.example.org")
//   - service: Value ("namespace/name")
//   - nodes: Selector
//   - none: nothing (the rule allows or denies no traffic)
//   - other: a peer the app does not describe; Value is its field (toGroups, cidrGroupSelector…)
type netPeer struct {
	Kind              string   `json:"kind"`
	Namespace         string   `json:"namespace,omitempty"`
	NamespaceSelector string   `json:"namespaceSelector,omitempty"`
	Selector          string   `json:"selector,omitempty"`
	Value             string   `json:"value,omitempty"`
	Except            []string `json:"except,omitempty"`
}

type netPort struct {
	Protocol string `json:"protocol"` // TCP, UDP, SCTP, ICMP, ANY
	Port     string `json:"port"`     // a number or a named port; "" = every port
	EndPort  int    `json:"endPort,omitempty"`
}

const netPolicyMaxPods = 50

// ref names p for a drop's attribution.
func (p *netPolicy) ref() policyRef {
	return policyRef{Kind: p.Kind, Namespace: p.Namespace, Name: p.Name}
}

// isolation tells whether p applies to an endpoint with labels in namespace, and whether it
// puts it in default-deny for ingress and egress.
func (p *netPolicy) isolation(namespace string, labels labelSet) (selected, ingress, egress bool) {
	if p.Namespace != "" && p.Namespace != namespace {
		return false, false, false
	}

	for _, s := range p.subjects {
		if s.selector.matches(labels) {
			selected, ingress, egress = true, ingress || s.ingress, egress || s.egress
		}
	}

	return selected, ingress, egress
}

// isolates tells whether p puts the endpoint in default-deny for direction (INGRESS or EGRESS).
func (p *netPolicy) isolates(namespace string, labels labelSet, direction string) bool {
	_, ingress, egress := p.isolation(namespace, labels)

	switch direction {
	case "INGRESS":
		return ingress
	case "EGRESS":
		return egress
	default:
		return false
	}
}

// --- Kubernetes NetworkPolicy ---

type npObject struct {
	Metadata kubeMeta `json:"metadata"`
	Spec     struct {
		PodSelector labelSelector `json:"podSelector"`
		PolicyTypes []string      `json:"policyTypes"`
		Ingress     []npRule      `json:"ingress"`
		Egress      []npRule      `json:"egress"`
	} `json:"spec"`
}

type kubeMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Annotations       map[string]string `json:"annotations"`
}

type npRule struct {
	From  []npPeer `json:"from"`
	To    []npPeer `json:"to"`
	Ports []struct {
		Protocol string          `json:"protocol"`
		Port     json.RawMessage `json:"port"`
		EndPort  int             `json:"endPort"`
	} `json:"ports"`
}

type npPeer struct {
	PodSelector       *labelSelector `json:"podSelector"`
	NamespaceSelector *labelSelector `json:"namespaceSelector"`
	IPBlock           *struct {
		CIDR   string   `json:"cidr"`
		Except []string `json:"except"`
	} `json:"ipBlock"`
}

func mapNetworkPolicy(o npObject) netPolicy {
	s := o.Spec
	p := netPolicy{
		Kind: kindNetworkPolicy, Namespace: o.Metadata.Namespace, Name: o.Metadata.Name,
		Created: unixMilli(o.Metadata.CreationTimestamp), Subject: s.PodSelector.String(),
	}

	// Without policyTypes: Ingress always, Egress when there are egress rules.
	types := s.PolicyTypes
	if len(types) == 0 {
		types = []string{"Ingress"}
		if len(s.Egress) > 0 {
			types = append(types, "Egress")
		}
	}

	p.Ingress, p.Egress = slices.Contains(types, "Ingress"), slices.Contains(types, "Egress")
	p.subjects = []policySubject{{selector: &s.PodSelector, ingress: p.Ingress, egress: p.Egress}}
	p.IngressRules, p.EgressRules = []netRule{}, []netRule{}

	if p.Ingress {
		for _, r := range s.Ingress {
			p.IngressRules = append(p.IngressRules, r.toRule(r.From))
		}
	}

	if p.Egress {
		for _, r := range s.Egress {
			p.EgressRules = append(p.EgressRules, r.toRule(r.To))
		}
	}

	return p
}

func (r npRule) toRule(peers []npPeer) netRule {
	out := netRule{Peers: []netPeer{}, Ports: []netPort{}}

	for _, peer := range peers {
		out.Peers = append(out.Peers, peer.toPeer())
	}

	for _, port := range r.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "TCP"
		}

		out.Ports = append(out.Ports, netPort{Protocol: protocol, Port: rawPort(port.Port), EndPort: port.EndPort})
	}

	return out
}

// rawPort reads an IntOrString port: "5432", "http", or "" when unset.
func rawPort(raw json.RawMessage) string {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return fmt.Sprint(n)
	}

	var s string
	_ = json.Unmarshal(raw, &s)

	return s
}

func (p npPeer) toPeer() netPeer {
	switch {
	case p.IPBlock != nil:
		return netPeer{Kind: "cidr", Value: p.IPBlock.CIDR, Except: p.IPBlock.Except}
	case p.NamespaceSelector != nil && p.PodSelector == nil:
		return netPeer{Kind: "namespaces", NamespaceSelector: p.NamespaceSelector.String()}
	case p.NamespaceSelector != nil:
		peer := netPeer{Kind: "pods", Selector: p.PodSelector.String(), NamespaceSelector: p.NamespaceSelector.String()}
		if peer.NamespaceSelector == "" {
			peer.Namespace = "*"
		}

		return peer
	default:
		return netPeer{Kind: "pods", Selector: p.PodSelector.String()}
	}
}

// policyDescription is the description a policy carries, if any.
func policyDescription(meta kubeMeta, specDescription string) string {
	if specDescription != "" {
		return strings.TrimSpace(specDescription)
	}

	return strings.TrimSpace(meta.Annotations["description"])
}
