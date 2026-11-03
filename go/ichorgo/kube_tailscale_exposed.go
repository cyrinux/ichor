package ichorgo

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// Which way an exposed row carries traffic: tailnet → cluster (ingress) or cluster → tailnet
// (egress).
const (
	tsDirectionIngress = "ingress"
	tsDirectionEgress  = "egress"
)

// The annotations and class names the operator reads on Services and Ingresses.
const (
	tsClassTailscale      = "tailscale"
	tsAnnotationExpose    = "tailscale.com/expose"
	tsAnnotationFQDN      = "tailscale.com/tailnet-fqdn"
	tsAnnotationIP        = "tailscale.com/tailnet-ip"
	tsAnnotationGroup     = "tailscale.com/proxy-group"
	tsAnnotationTags      = "tailscale.com/tags"
	tsAnnotationHostname  = "tailscale.com/hostname"
	tsIngressHTTPSPort    = "443"
	tsServiceTypeLB       = "LoadBalancer"
	tsKindService         = "Service"
	tsKindIngress         = "Ingress"
	tsReasonGroupMissing  = "ProxyGroupMissing"
	tsReasonGroupDown     = "ProxyGroupUnavailable"
	tsReasonNoTailnetName = "NoTailnetName"
)

// The conditions the operator writes on the Services it serves (k8s-operator/apis/v1alpha1).
// A Service on a ProxyGroup gets the *Svc* ones; a Service with a proxy of its own gets
// TailscaleProxyReady. Ingresses carry no condition: their tailnet name is the signal.
const (
	tsCondProxyReady           = "TailscaleProxyReady"
	tsCondIngressSvcValid      = "TailscaleIngressSvcValid"
	tsCondIngressSvcConfigured = "TailscaleIngressSvcConfigured"
	tsCondEgressSvcValid       = "TailscaleEgressSvcValid"
	tsCondEgressSvcConfigured  = "TailscaleEgressSvcConfigured"
	tsCondEgressSvcReady       = "TailscaleEgressSvcReady"
)

// tsExposed is one Service or Ingress the operator serves: reachable on the tailnet under
// TailnetNames (ingress), or a tailnet target the cluster reaches through it (egress).
type tsExposed struct {
	Kind      string `json:"kind"` // Service or Ingress
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Direction string `json:"direction"` // ingress or egress
	// TailnetNames is the name and IPs on the tailnet (ingress), or the target (egress).
	TailnetNames []string `json:"tailnetNames"`
	Ports        []string `json:"ports"`      // "443", "53/UDP"
	ProxyGroup   string   `json:"proxyGroup"` // "" when the object has a proxy of its own
	Tags         []string `json:"tags"`
	tsState
}

// tsServiceObject is a Service, enough to tell whether the operator serves it.
type tsServiceObject struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Generation  int64             `json:"generation"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Type              string `json:"type"`
		LoadBalancerClass string `json:"loadBalancerClass"`
		Ports             []struct {
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []lbIngress `json:"ingress"`
		} `json:"loadBalancer"`
		Conditions []tsCondition `json:"conditions"`
	} `json:"status"`
}

// tsExposedRows maps the Services and Ingresses the operator serves; the rest are skipped.
func tsExposedRows(services []tsServiceObject, ingresses []ingressObject) []tsExposed {
	rows := []tsExposed{}

	for _, s := range services {
		if row, ok := tsServiceRow(s); ok {
			rows = append(rows, row)
		}
	}

	for _, ing := range ingresses {
		if ing.Spec.IngressClassName == tsClassTailscale {
			rows = append(rows, tsIngressRow(ing))
		}
	}

	return rows
}

// tsServiceRow is an egress Service (a tailnet-fqdn or tailnet-ip target), an ingress one
// (loadBalancerClass tailscale or tailscale.com/expose), or not the operator's.
func tsServiceRow(s tsServiceObject) (tsExposed, bool) {
	a := s.Metadata.Annotations
	row := tsExposed{
		Kind: tsKindService, Namespace: s.Metadata.Namespace, Name: s.Metadata.Name,
		ProxyGroup: a[tsAnnotationGroup], Tags: tsTags(a[tsAnnotationTags]), Ports: []string{},
	}

	for _, p := range s.Spec.Ports {
		port := strconv.Itoa(p.Port)
		if p.Protocol != "" && p.Protocol != "TCP" {
			port += "/" + p.Protocol
		}
		row.Ports = append(row.Ports, port)
	}

	switch target := cmp.Or(a[tsAnnotationFQDN], a[tsAnnotationIP]); {
	case target != "":
		row.Direction, row.TailnetNames = tsDirectionEgress, []string{target}
	case s.Spec.LoadBalancerClass == tsClassTailscale || a[tsAnnotationExpose] == "true":
		row.Direction = tsDirectionIngress
		row.TailnetNames = tsAddressNames(s.Status.LoadBalancer.Ingress)
		// A ClusterIP exposed with an annotation has no load-balancer status: its hostname.
		if len(row.TailnetNames) == 0 && s.Spec.Type != tsServiceTypeLB && a[tsAnnotationHostname] != "" {
			row.TailnetNames = []string{a[tsAnnotationHostname]}
		}
	default:
		return tsExposed{}, false
	}

	row.tsState = tsServiceState(s, row.Direction, row.ProxyGroup)

	// Configured, but the name that tells where to reach it is not written yet.
	if row.Direction == tsDirectionIngress && s.Spec.Type == tsServiceTypeLB && len(row.TailnetNames) == 0 {
		tsNoName(&row.tsState)
	}

	return row, true
}

// tsServiceState is the Service's readiness from the operator's conditions: a Valid
// condition that is False is critical; otherwise the configured (or ready) one decides.
func tsServiceState(s tsServiceObject, direction, group string) tsState {
	conds, gen := s.Status.Conditions, s.Metadata.Generation

	valid, decide := "", []string{tsCondProxyReady}
	switch {
	case group != "" && direction == tsDirectionIngress:
		valid, decide = tsCondIngressSvcValid, []string{tsCondIngressSvcConfigured}
	case group != "":
		valid, decide = tsCondEgressSvcValid, []string{tsCondEgressSvcReady, tsCondEgressSvcConfigured}
	}

	if c, ok := tsConditionOf(conds, valid); ok && c.Status == "False" {
		return tsState{Reason: c.Reason, Message: c.Message, Health: tsHealthCritical}
	}

	for _, typ := range decide {
		if _, ok := tsConditionOf(conds, typ); ok {
			return tsStateOf(conds, typ, gen, tsHealthCritical)
		}
	}

	return tsStateOf(conds, decide[0], gen, tsHealthCritical)
}

// tsIngressRow is an Ingress of class tailscale: it serves HTTPS on its tailnet name, and the
// operator writes no condition, so no name yet is the only sign it is not served.
func tsIngressRow(ing ingressObject) tsExposed {
	a := ing.Metadata.Annotations

	var names []lbIngress
	for _, in := range ing.Status.LoadBalancer.Ingress {
		names = append(names, lbIngress{IP: in.IP, Hostname: in.Hostname})
	}

	row := tsExposed{
		Kind: tsKindIngress, Namespace: ing.Metadata.Namespace, Name: ing.Metadata.Name, Direction: tsDirectionIngress,
		TailnetNames: tsAddressNames(names), Ports: []string{tsIngressHTTPSPort},
		ProxyGroup: a[tsAnnotationGroup], Tags: tsTags(a[tsAnnotationTags]),
		tsState: tsState{Ready: true, Health: tsHealthOK},
	}
	if len(row.TailnetNames) == 0 {
		tsNoName(&row.tsState)
	}

	return row
}

// tsNoName lowers a row that would be ok to a warning: it has no tailnet name yet.
func tsNoName(s *tsState) {
	if s.Health == tsHealthOK {
		*s = tsState{Reason: tsReasonNoTailnetName, Health: tsHealthWarning}
	}
}

// tsAddressNames is the hostnames first, then the IPs, each once.
func tsAddressNames(addrs []lbIngress) []string {
	names := []string{}
	add := func(v string) {
		if v != "" && !slices.Contains(names, v) {
			names = append(names, v)
		}
	}

	for _, a := range addrs {
		add(a.Hostname)
	}
	for _, a := range addrs {
		add(a.IP)
	}

	return names
}

// tsTags splits the tailscale.com/tags annotation ("tag:a,tag:b").
func tsTags(v string) []string {
	tags := []string{}
	for t := range strings.SplitSeq(v, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}

	return tags
}

// linkProxyGroups counts what each ProxyGroup serves and, when the ProxyGroups could be read,
// raises an exposed row naming a missing one to a warning and one whose ProxyGroup runs no
// proxy to critical.
func linkProxyGroups(r *tsReport, groupsRead bool) {
	serves := map[string]int{}
	groups := map[string]tsProxyGroup{}

	for _, g := range r.ProxyGroups {
		groups[g.Name] = g
	}
	for _, e := range r.Exposed {
		if e.ProxyGroup != "" {
			serves[e.ProxyGroup]++
		}
	}
	for i := range r.ProxyGroups {
		r.ProxyGroups[i].Serves = serves[r.ProxyGroups[i].Name]
	}

	if !groupsRead {
		return
	}

	for i := range r.Exposed {
		e := &r.Exposed[i]
		if e.ProxyGroup == "" {
			continue
		}

		g, known := groups[e.ProxyGroup]

		switch {
		case !known && e.Health == tsHealthOK:
			e.Health, e.Reason = tsHealthWarning, tsReasonGroupMissing
		case known && !g.Available && e.Health != tsHealthCritical:
			e.Health, e.Reason, e.Message = tsHealthCritical, tsReasonGroupDown, g.Message
		}
	}
}
