package ichorgo

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// The class prefixes of the controllers that take a Service by its loadBalancerClass.
const (
	kubeVipClassPrefix = "kube-vip.io/"
	ciliumClassPrefix  = "io.cilium/"
	// kubeVipIPsAnnotation pins a Service to addresses kube-vip announces without a range.
	kubeVipIPsAnnotation = "kube-vip.io/loadbalancerIPs"
)

// lbServiceRows lists the LoadBalancer Services: address, pool, announcer and, while one
// waits, why. Waiting past lbGrace is critical, a fresh one a warning.
func lbServiceRows(in lbInputs, now time.Time) []lbService {
	out := make([]lbService, 0, len(in.services))
	announcers := in.metallb.announcers()

	for _, s := range in.services {
		row := lbService{
			Namespace: s.Metadata.Namespace, Name: s.Metadata.Name, Class: s.Spec.LoadBalancerClass,
			Addresses: []string{}, Interfaces: []string{}, Peers: []string{}, Since: milli(s.Metadata.CreationTimestamp),
			Pool: in.poolOf(s), Level: serviceOK,
		}

		for _, ing := range s.Status.LoadBalancer.Ingress {
			row.Addresses = append(row.Addresses, cmp.Or(ing.IP, ing.Hostname))
		}

		if a, ok := announcers[s.Metadata.Namespace+"/"+s.Metadata.Name]; ok {
			row.AnnouncedBy, row.Interfaces, row.Peers = a.node, a.interfaces, a.peers
		}

		if row.Pending = len(row.Addresses) == 0 && s.Metadata.DeletionTimestamp == nil; row.Pending {
			row.Cause = lbCauseOf(in, s)
			row.Level = serviceWarning

			if olderThan(s.Metadata.CreationTimestamp, now, lbGrace) {
				row.Level = serviceCritical
			}
		}

		out = append(out, row)
	}

	return out
}

// poolOf is the pool a Service's address came from: MetalLB's annotation, else the first
// pool holding one of its addresses; "" when none.
func (in lbInputs) poolOf(s lbServiceObject) string {
	if p := s.Metadata.Annotations[metallbAllocatedFrom]; p != "" {
		return p
	}

	for _, ing := range s.Status.LoadBalancer.Ingress {
		if in.metallb != nil {
			for _, p := range in.metallb.pools {
				if rangesContain(poolRanges(p.Spec.Addresses), ing.IP) {
					return p.Metadata.Name
				}
			}
		}

		for _, p := range in.cilium {
			if rangesContain(poolRanges(p.addresses()), ing.IP) {
				return p.Metadata.Name
			}
		}
	}

	return ""
}

type announcer struct {
	node              string
	interfaces, peers []string
}

// announcers maps "namespace/name" to the node announcing the Service, from MetalLB's
// ServiceL2Status and ServiceBGPStatus.
func (m *metallbState) announcers() map[string]announcer {
	out := map[string]announcer{}

	if m == nil {
		return out
	}

	for _, s := range m.serviceStatus {
		key := s.Status.ServiceNamespace + "/" + s.Status.ServiceName
		a := out[key]
		a.node = cmp.Or(a.node, s.Status.Node)

		for _, i := range s.Status.Interfaces {
			a.interfaces = append(a.interfaces, i.Name)
		}

		a.peers = append(a.peers, s.Status.Peers...)
		a.interfaces, a.peers = nonNil(a.interfaces), nonNil(a.peers)
		out[key] = a
	}

	return out
}

// controllerOf is the controller that should give s its address: by its class, else the
// first one found. False when its class names a controller this app does not know.
func (in lbInputs) controllerOf(s lbServiceObject) (string, bool) {
	switch class := s.Spec.LoadBalancerClass; {
	case strings.HasPrefix(class, kubeVipClassPrefix):
		return lbControllerKubeVip, true
	case strings.HasPrefix(class, ciliumClassPrefix):
		return lbControllerCilium, true
	case class != "":
		return "", false
	}

	if c := in.controllers(); len(c) > 0 {
		return c[0], true
	}

	return "", true
}

// lbCauseOf tells why s has no address, the first that applies: no controller, a refused
// configuration, the controller not running, no pool, no pool for it, its pools full, its
// pools not advertised; unknown otherwise.
func lbCauseOf(in lbInputs, s lbServiceObject) string {
	controller, known := in.controllerOf(s)

	switch {
	case !known:
		return lbCauseUnknown
	case !slices.Contains(in.controllers(), controller):
		return lbCauseNoController
	case controller == lbControllerMetalLB:
		return in.metallbCause(s)
	case controller == lbControllerKubeVip:
		return in.kubeVipCause(s)
	default:
		return in.ciliumCause(s)
	}
}

func (in lbInputs) metallbCause(s lbServiceObject) string {
	m := in.metallb

	switch {
	case len(m.configErrors()) > 0:
		return lbCauseConfigError
	case !in.runs(lbControllerMetalLB):
		return lbCauseControllerDown
	case len(m.pools) == 0 && m.partial:
		return lbCauseUnknown
	case len(m.pools) == 0:
		return lbCauseNoPool
	}

	candidates := slices.DeleteFunc(slices.Clone(m.pools), func(p metallbPool) bool { return !in.metallbPoolServes(p, s) })
	rows := map[string]lbPool{}

	for _, p := range lbPoolRows(in, true) {
		rows[p.Name] = p
	}

	switch {
	case len(candidates) == 0:
		return lbCausePoolNotMatched
	case !slices.ContainsFunc(candidates, func(p metallbPool) bool { return rows[p.Metadata.Name].Available > 0 }):
		return lbCausePoolExhausted
	case !slices.ContainsFunc(candidates, func(p metallbPool) bool { return len(m.advertisers(p)) > 0 }):
		return lbCausePoolNotAdvertised
	default:
		return lbCauseUnknown
	}
}

// metallbPoolServes tells whether MetalLB may take an address for s from p: the pool it
// names, else a pool that assigns on its own and whose serviceAllocation lets s in.
func (in lbInputs) metallbPoolServes(p metallbPool, s lbServiceObject) bool {
	for _, a := range metallbPoolAnnotations {
		if names := s.Metadata.Annotations[a]; names != "" {
			return slices.Contains(strings.Split(names, ","), p.Metadata.Name)
		}
	}

	alloc := p.Spec.ServiceAllocation

	switch {
	case p.Spec.AutoAssign != nil && !*p.Spec.AutoAssign:
		return false
	case len(alloc.Namespaces) > 0 && !slices.Contains(alloc.Namespaces, s.Metadata.Namespace):
		return false
	case len(alloc.ServiceSelectors) > 0 && !slices.ContainsFunc(alloc.ServiceSelectors, func(sel labelSelector) bool {
		return sel.matches(labelSet(s.Metadata.Labels))
	}):
		return false
	}

	// Without the namespaces' labels, a namespace selector is given the benefit of the doubt.
	nsLabels, known := in.namespaces[s.Metadata.Namespace]

	return len(alloc.NamespaceSelectors) == 0 || !known || slices.ContainsFunc(alloc.NamespaceSelectors, func(sel labelSelector) bool {
		return sel.matches(nsLabels)
	})
}

func (in lbInputs) ciliumCause(s lbServiceObject) string {
	pools := slices.DeleteFunc(slices.Clone(in.cilium), func(p ciliumPoolObject) bool { return p.Spec.Disabled })

	switch {
	case len(pools) == 0:
		return lbCauseNoPool
	case !slices.ContainsFunc(pools, func(p ciliumPoolObject) bool { return p.serves(s) }):
		return lbCausePoolNotMatched
	}

	free := slices.ContainsFunc(pools, func(p ciliumPoolObject) bool {
		_, available, ok := p.counts()

		return p.serves(s) && (!ok || available > 0)
	})
	if !free {
		return lbCausePoolExhausted
	}

	return lbCauseUnknown
}

func (in lbInputs) kubeVipCause(s lbServiceObject) string {
	v := in.kubeVip

	switch {
	case !in.runs(lbControllerKubeVip):
		return lbCauseControllerDown
	case s.Metadata.Annotations[kubeVipIPsAnnotation] != "":
		return lbCauseUnknown
	case len(v.ranges) == 0:
		return lbCauseNoPool
	case !slices.ContainsFunc(v.ranges, func(r lbKubeVipRange) bool {
		return r.Scope == kubeVipGlobal || r.Scope == s.Metadata.Namespace
	}):
		return lbCausePoolNotMatched
	default:
		return lbCauseUnknown
	}
}
