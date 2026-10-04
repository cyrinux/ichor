package ichorgo

import (
	"strconv"
	"strings"
)

// cnpObject is a CiliumNetworkPolicy or CiliumClusterwideNetworkPolicy: one rule in spec,
// several in specs, or both.
type cnpObject struct {
	Metadata kubeMeta  `json:"metadata"`
	Spec     *cnpSpec  `json:"spec"`
	Specs    []cnpSpec `json:"specs"`
}

type cnpSpec struct {
	EndpointSelector  *labelSelector `json:"endpointSelector"`
	NodeSelector      *labelSelector `json:"nodeSelector"`
	Description       string         `json:"description"`
	Ingress           []cnpRule      `json:"ingress"`
	IngressDeny       []cnpRule      `json:"ingressDeny"`
	Egress            []cnpRule      `json:"egress"`
	EgressDeny        []cnpRule      `json:"egressDeny"`
	EnableDefaultDeny struct {
		Ingress *bool `json:"ingress"`
		Egress  *bool `json:"egress"`
	} `json:"enableDefaultDeny"`
}

// cnpRule holds the fields of an ingress and an egress rule (from* for one, to* for the other).
type cnpRule struct {
	FromEndpoints []labelSelector `json:"fromEndpoints"`
	ToEndpoints   []labelSelector `json:"toEndpoints"`
	FromCIDR      []string        `json:"fromCIDR"`
	ToCIDR        []string        `json:"toCIDR"`
	FromCIDRSet   []cidrRule      `json:"fromCIDRSet"`
	ToCIDRSet     []cidrRule      `json:"toCIDRSet"`
	FromEntities  []string        `json:"fromEntities"`
	ToEntities    []string        `json:"toEntities"`
	FromNodes     []labelSelector `json:"fromNodes"`
	ToNodes       []labelSelector `json:"toNodes"`
	ToFQDNs       []struct {
		MatchName    string `json:"matchName"`
		MatchPattern string `json:"matchPattern"`
	} `json:"toFQDNs"`
	ToServices []struct {
		K8sService *struct {
			ServiceName string `json:"serviceName"`
			Namespace   string `json:"namespace"`
		} `json:"k8sService"`
		K8sServiceSelector *struct {
			Selector  labelSelector `json:"selector"`
			Namespace string        `json:"namespace"`
		} `json:"k8sServiceSelector"`
	} `json:"toServices"`
	ToPorts []struct {
		Ports []struct {
			Port     string `json:"port"`
			EndPort  int    `json:"endPort"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
		Rules *struct {
			HTTP []struct {
				Method string `json:"method"`
				Path   string `json:"path"`
				Host   string `json:"host"`
			} `json:"http"`
			DNS []struct {
				MatchName    string `json:"matchName"`
				MatchPattern string `json:"matchPattern"`
			} `json:"dns"`
		} `json:"rules"`
	} `json:"toPorts"`
	// Peers the app does not describe, decoded only so a rule using them is not shown as "any".
	FromGroups []any `json:"fromGroups"`
	ToGroups   []any `json:"toGroups"`
	ICMPs      []struct {
		Fields []struct {
			Family string `json:"family"`
			Type   any    `json:"type"`
		} `json:"fields"`
	} `json:"icmps"`
}

type cidrRule struct {
	CIDR              string         `json:"cidr"`
	CIDRGroupRef      string         `json:"cidrGroupRef"`
	CIDRGroupSelector *labelSelector `json:"cidrGroupSelector"`
	Except            []string       `json:"except"`
}

func mapCiliumPolicy(o cnpObject, clusterwide bool) netPolicy {
	p := netPolicy{
		Kind: kindCiliumPolicy, Namespace: o.Metadata.Namespace, Name: o.Metadata.Name,
		Created: unixMilli(o.Metadata.CreationTimestamp), IngressRules: []netRule{}, EgressRules: []netRule{},
	}

	if clusterwide {
		p.Kind, p.Namespace = kindCiliumClusterPolicy, ""
	}

	specs := o.Specs
	if o.Spec != nil {
		specs = append([]cnpSpec{*o.Spec}, specs...)
	}

	var subjects []string

	for _, s := range specs {
		if p.Description == "" {
			p.Description = policyDescription(o.Metadata, s.Description)
		}

		ingress := s.isolates(s.Ingress, s.IngressDeny, s.EnableDefaultDeny.Ingress)
		egress := s.isolates(s.Egress, s.EgressDeny, s.EnableDefaultDeny.Egress)

		switch {
		case s.NodeSelector != nil:
			// A host policy: it selects nodes, never pods.
			p.Nodes = true
			subjects = append(subjects, s.NodeSelector.String())
		case s.EndpointSelector != nil:
			p.subjects = append(p.subjects, policySubject{selector: s.EndpointSelector, ingress: ingress, egress: egress})
			subjects = append(subjects, s.EndpointSelector.String())

			if ns := s.EndpointSelector.namespaceOf(); ns != "" && clusterwide {
				p.SubjectNS = ns
			}
		}

		p.Ingress, p.Egress = p.Ingress || ingress, p.Egress || egress
		p.IngressRules = appendCiliumRules(p.IngressRules, s.Ingress, false, true)
		p.IngressRules = appendCiliumRules(p.IngressRules, s.IngressDeny, true, true)
		p.EgressRules = appendCiliumRules(p.EgressRules, s.Egress, false, false)
		p.EgressRules = appendCiliumRules(p.EgressRules, s.EgressDeny, true, false)
	}

	p.Subject = strings.Join(subjects, " | ")

	return p
}

// isolates: a Cilium rule puts its endpoints in default-deny for a direction when it has
// rules for it (an empty rule {} counts, an empty list does not), unless enableDefaultDeny
// says otherwise.
func (cnpSpec) isolates(allow, deny []cnpRule, defaultDeny *bool) bool {
	if defaultDeny != nil {
		return *defaultDeny
	}

	return len(allow) > 0 || len(deny) > 0
}

func appendCiliumRules(out []netRule, rules []cnpRule, deny, ingress bool) []netRule {
	for _, r := range rules {
		rule := r.toRule(ingress)
		rule.Deny = deny
		out = append(out, rule)
	}

	return out
}

func (r cnpRule) toRule(ingress bool) netRule {
	endpoints, cidrs, cidrSets, entities, nodes := r.ToEndpoints, r.ToCIDR, r.ToCIDRSet, r.ToEntities, r.ToNodes
	if ingress {
		endpoints, cidrs, cidrSets, entities, nodes = r.FromEndpoints, r.FromCIDR, r.FromCIDRSet, r.FromEntities, r.FromNodes
	}

	out := netRule{Peers: []netPeer{}, Ports: []netPort{}}

	for _, e := range endpoints {
		peer := netPeer{Kind: "pods", Selector: e.String(), Namespace: e.namespaceOf()}
		out.Peers = append(out.Peers, peer)
	}

	for _, c := range cidrs {
		out.Peers = append(out.Peers, netPeer{Kind: "cidr", Value: c})
	}

	for _, c := range cidrSets {
		switch {
		case c.CIDR != "" || c.CIDRGroupRef != "":
			out.Peers = append(out.Peers, netPeer{Kind: "cidr", Value: c.CIDR + c.CIDRGroupRef, Except: c.Except})
		default:
			out.Peers = append(out.Peers, netPeer{Kind: "other", Value: "cidrGroupSelector", Selector: c.CIDRGroupSelector.String()})
		}
	}

	for _, e := range entities {
		out.Peers = append(out.Peers, netPeer{Kind: "entity", Value: e})
	}

	for _, n := range nodes {
		out.Peers = append(out.Peers, netPeer{Kind: "nodes", Selector: n.String()})
	}

	groups, groupsField := r.ToGroups, "toGroups"
	if ingress {
		groups, groupsField = r.FromGroups, "fromGroups"
	}

	if len(groups) > 0 {
		out.Peers = append(out.Peers, netPeer{Kind: "other", Value: groupsField})
	}

	if !ingress {
		out.Peers = append(out.Peers, r.egressOnlyPeers()...)
	}

	out.Ports, out.L7 = r.ports()

	// Unlike a Kubernetes one, an empty Cilium rule selects no peer: it allows (or denies) nothing.
	if len(out.Peers) == 0 && len(out.Ports) == 0 && len(out.L7) == 0 {
		out.Peers = append(out.Peers, netPeer{Kind: "none"})
	}

	return out
}

func (r cnpRule) egressOnlyPeers() []netPeer {
	var out []netPeer

	for _, f := range r.ToFQDNs {
		value := f.MatchName
		if value == "" {
			value = f.MatchPattern
		}

		out = append(out, netPeer{Kind: "fqdn", Value: value})
	}

	for _, s := range r.ToServices {
		switch {
		case s.K8sService != nil:
			out = append(out, netPeer{Kind: "service", Value: strings.Trim(s.K8sService.Namespace+"/"+s.K8sService.ServiceName, "/")})
		case s.K8sServiceSelector != nil:
			out = append(out, netPeer{Kind: "service", Namespace: s.K8sServiceSelector.Namespace, Selector: s.K8sServiceSelector.Selector.String()})
		}
	}

	return out
}

func (r cnpRule) ports() ([]netPort, []string) {
	ports, l7 := []netPort{}, []string(nil)

	for _, tp := range r.ToPorts {
		for _, p := range tp.Ports {
			protocol := strings.ToUpper(p.Protocol)
			if protocol == "" {
				protocol = "ANY"
			}

			port := p.Port
			if port == "0" {
				port = ""
			}

			ports = append(ports, netPort{Protocol: protocol, Port: port, EndPort: p.EndPort})
		}

		if tp.Rules == nil {
			continue
		}

		for _, h := range tp.Rules.HTTP {
			l7 = append(l7, strings.Join(strings.Fields("HTTP "+h.Method+" "+h.Host+h.Path), " "))
		}

		for _, d := range tp.Rules.DNS {
			l7 = append(l7, "DNS "+d.MatchName+d.MatchPattern)
		}
	}

	for _, icmp := range r.ICMPs {
		for _, f := range icmp.Fields {
			family := f.Family
			if family == "" {
				family = "IPv4"
			}

			ports = append(ports, netPort{Protocol: "ICMP" + strings.TrimPrefix(family, "IP"), Port: strings.TrimSpace(anyString(f.Type))})
		}
	}

	return ports, l7
}

// anyString prints an int-or-string JSON value.
func anyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return t
	default:
		return ""
	}
}
