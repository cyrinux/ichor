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
	// Ingress / Egress isolate the selected pods when true: only what the rules allow passes.
	Ingress      bool      `json:"ingress"`
	Egress       bool      `json:"egress"`
	IngressRules []netRule `json:"ingressRules"`
	EgressRules  []netRule `json:"egressRules"`
	// Pods the policy selects now ("namespace/name"), at most netPolicyMaxPods, and their count.
	Pods     []string `json:"pods"`
	PodCount int      `json:"podCount"`

	subjects []*labelSelector // what selects the pods (several for a Cilium policy with specs)
}

// netRule is one rule of a direction: traffic from/to any of Peers on any of Ports passes
// (or is denied when Deny). No peers means any peer, no ports any port.
type netRule struct {
	Deny  bool      `json:"deny,omitempty"`
	Peers []netPeer `json:"peers"`
	Ports []netPort `json:"ports"`
	L7    []string  `json:"l7,omitempty"` // "HTTP GET /api", "DNS *.example.org"
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

// selects tells whether p applies to an endpoint with labels in namespace.
func (p *netPolicy) selects(namespace string, labels labelSet) bool {
	if p.Nodes || (p.Namespace != "" && p.Namespace != namespace) {
		return false
	}

	return slices.ContainsFunc(p.subjects, func(s *labelSelector) bool { return s.matches(labels) })
}

// isolates tells whether p turns default-deny on for direction (INGRESS or EGRESS).
func (p *netPolicy) isolates(direction string) bool {
	switch direction {
	case "INGRESS":
		return p.Ingress
	case "EGRESS":
		return p.Egress
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
		subjects: []*labelSelector{&s.PodSelector},
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
