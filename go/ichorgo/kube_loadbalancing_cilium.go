package ichorgo

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// Cilium's L2 announcements: CiliumL2AnnouncementPolicies say which Services' addresses the
// nodes answer ARP and NDP for; the node holding the Service's lease answers. They only work
// when cilium-config turns them on (enable-l2-announcements), and a pool may also be
// announced by Cilium's BGP control plane, which is not read here.

// The namespaces Cilium is usually installed in, where cilium-config and the leases are,
// when its agents cannot be found.
var ciliumNamespaces = []string{"kube-system", "cilium"}

// ciliumL2LeasePrefix + "<namespace>-<name>" is the lease of a Service's announcer.
const ciliumL2LeasePrefix = "cilium-l2announce-"

// ciliumL2Class is the only class, besides none, the L2 announcer takes.
const ciliumL2Class = "io.cilium/l2-announcer"

// Why a CiliumL2AnnouncementPolicy announces nothing (lbAdvertisement.Inactive).
const (
	lbAdvertL2Disabled      = "l2Disabled"
	lbAdvertNoLBIPs         = "noLoadBalancerIPs"
	lbAdvertNoServiceMatch  = "noService"
	ciliumConfigL2Enabled   = "enable-l2-announcements"
	ciliumConfigBGPEnabled  = "enable-bgp-control-plane"
	ciliumConfigTrueSetting = "true"
)

type ciliumL2Policy struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		ServiceSelector *labelSelector `json:"serviceSelector"`
		NodeSelector    *labelSelector `json:"nodeSelector"`
		Interfaces      []string       `json:"interfaces"`
		ExternalIPs     bool           `json:"externalIPs"`
		LoadBalancerIPs bool           `json:"loadBalancerIPs"`
	} `json:"spec"`
}

// ciliumL2State is what Cilium says of L2 announcements. configKnown is false when
// cilium-config could not be read: nothing is then said to be off. holders maps
// "namespace/name" to the node holding the Service's lease.
type ciliumL2State struct {
	policies    []ciliumL2Policy
	holders     map[string]string
	configKnown bool
	l2Enabled   bool
	bgpEnabled  bool
}

// readCiliumL2 reads the policies, then cilium-config and the announcers' leases in Cilium's
// namespace; the policies' error is the only one returned (none installed is not one).
func readCiliumL2(ctx context.Context, k *kubeClient) (*ciliumL2State, error) {
	var (
		s          ciliumL2State
		namespaces []string
		err        error
		wg         sync.WaitGroup
	)

	wg.Go(func() { s.policies, err = readCiliumL2Policies(ctx, k) })
	wg.Go(func() { namespaces = ciliumNamespacesOf(ctx, k) })
	wg.Wait()

	namespace := s.readConfig(ctx, k, namespaces)
	s.holders = readCiliumL2Holders(ctx, k, namespace)

	return &s, err
}

// ciliumNamespacesOf lists where Cilium may be installed: its agents' namespace first (Helm
// installs it anywhere), then the usual ones.
func ciliumNamespacesOf(ctx context.Context, k *kubeClient) []string {
	agents, err := readAgentPods(ctx, k, ciliumAgentSelector, ciliumAgentContainer, flowCNICilium)
	if err != nil || agents.Namespace == "" || slices.Contains(ciliumNamespaces, agents.Namespace) {
		return ciliumNamespaces
	}

	return append([]string{agents.Namespace}, ciliumNamespaces...)
}

// readCiliumL2Policies reads the policies, v2 first, v2alpha1 on an older Cilium.
func readCiliumL2Policies(ctx context.Context, k *kubeClient) ([]ciliumL2Policy, error) {
	policies, err := listObjects[ciliumL2Policy](ctx, k, "/apis/"+groupCilium+"/v2/ciliuml2announcementpolicies")
	if isNotFound(err) {
		policies, err = listObjects[ciliumL2Policy](ctx, k, "/apis/"+groupCilium+"/v2alpha1/ciliuml2announcementpolicies")
	}

	return policies, ignoreNotFound(err)
}

// readConfig reads cilium-config's L2 and BGP switches from the first of namespaces holding
// it; that namespace, or the first one when none does.
func (s *ciliumL2State) readConfig(ctx context.Context, k *kubeClient, namespaces []string) string {
	for _, ns := range namespaces {
		var cm struct {
			Data map[string]string `json:"data"`
		}

		if k.get(ctx, "/api/v1/namespaces/"+ns+"/configmaps/"+ciliumConfigMap, &cm) != nil {
			continue
		}

		s.configKnown = true
		s.l2Enabled = strings.TrimSpace(cm.Data[ciliumConfigL2Enabled]) == ciliumConfigTrueSetting
		s.bgpEnabled = strings.TrimSpace(cm.Data[ciliumConfigBGPEnabled]) == ciliumConfigTrueSetting

		return ns
	}

	return namespaces[0]
}

// readCiliumL2Holders maps each announced Service to the node holding its lease; empty when
// the leases cannot be read.
func readCiliumL2Holders(ctx context.Context, k *kubeClient, namespace string) map[string]string {
	out := map[string]string{}

	leases, _ := listObjects[leaseObject](ctx, k, "/apis/coordination.k8s.io/v1/namespaces/"+namespace+"/leases") //nolint:errcheck // none found

	for _, l := range leases {
		if strings.HasPrefix(l.Metadata.Name, ciliumL2LeasePrefix) && l.Spec.HolderIdentity != "" {
			out[l.Metadata.Name] = l.Spec.HolderIdentity
		}
	}

	return out
}

// works tells whether the policies can announce anything: off only when cilium-config says so.
func (s *ciliumL2State) works() bool {
	return s != nil && (!s.configKnown || s.l2Enabled)
}

// selects tells whether p announces svc's LoadBalancer addresses, Cilium's way: a class
// it takes, and a selector (if any) on the Service's labels and the name and namespace.
func (p ciliumL2Policy) selects(svc lbServiceObject) bool {
	if !p.Spec.LoadBalancerIPs {
		return false
	}

	if class := svc.Spec.LoadBalancerClass; class != "" && class != ciliumL2Class {
		return false
	}

	return p.allServices() || p.Spec.ServiceSelector.matches(ciliumServiceLabels(svc))
}

func (p ciliumL2Policy) allServices() bool { return p.Spec.ServiceSelector == nil }

// ciliumServiceLabels is the Service's labels with the name and namespace Cilium adds to them.
func ciliumServiceLabels(svc lbServiceObject) labelSet {
	labels := labelSet{"io.kubernetes.service.namespace": svc.Metadata.Namespace, "io.kubernetes.service.name": svc.Metadata.Name}
	for k, v := range svc.Metadata.Labels {
		labels[k] = v
	}

	return labels
}

// holderOf is the node announcing svc, "" when none holds its lease.
func (s *ciliumL2State) holderOf(svc lbServiceObject) string {
	if s == nil {
		return ""
	}

	return s.holders[ciliumL2LeasePrefix+svc.Metadata.Namespace+"-"+svc.Metadata.Name]
}

// advertisers lists the policies that announce the pool ("l2:<name>"): those taking every
// Service, and those selecting a Service holding one of its addresses. None while the
// policies cannot work.
func (s *ciliumL2State) advertisers(pool ciliumPoolObject, services []lbServiceObject) []string {
	out := []string{}

	if !s.works() {
		return out
	}

	ranges := poolRanges(pool.addresses())
	inPool := slices.DeleteFunc(slices.Clone(services), func(svc lbServiceObject) bool {
		return !slices.ContainsFunc(svc.Status.LoadBalancer.Ingress, func(ing lbIngress) bool { return rangesContain(ranges, ing.IP) })
	})

	for _, p := range s.policies {
		if p.Spec.LoadBalancerIPs && (p.allServices() || slices.ContainsFunc(inPool, p.selects)) {
			out = append(out, lbAdvertL2+":"+p.Metadata.Name)
		}
	}

	return out
}

// unannounced tells whether a pool holding Services' addresses should be announced by L2
// and is not: the cluster uses L2 announcements (a policy, or cilium-config turns them on),
// BGP is known off, and no policy announces it.
func (s *ciliumL2State) unannounced(pool ciliumPoolObject, services []lbServiceObject) bool {
	if s == nil || !s.configKnown || s.bgpEnabled || len(s.policies) == 0 && !s.l2Enabled {
		return false
	}

	held, _ := servicesInPool(services, "", poolRanges(pool.addresses()))

	return len(held) > 0 && len(s.advertisers(pool, services)) == 0
}

// lbCiliumAdvertisementRows is the policies as the screen lists them, with the pools of the
// Services they announce and, when they announce nothing, why.
func lbCiliumAdvertisementRows(in lbInputs) []lbAdvertisement {
	out := []lbAdvertisement{}

	if in.ciliumL2 == nil {
		return out
	}

	for _, p := range in.ciliumL2.policies {
		row := lbAdvertisement{
			Name: p.Metadata.Name, Kind: lbAdvertL2, Controller: lbControllerCilium, Pools: []string{}, AllPools: p.allServices(),
			Interfaces: nonNil(p.Spec.Interfaces), Peers: []string{}, Communities: []string{},
		}

		if p.Spec.NodeSelector != nil {
			row.NodeSelectors = 1
		}

		selected := slices.DeleteFunc(slices.Clone(in.services), func(svc lbServiceObject) bool { return !p.selects(svc) })
		for _, svc := range selected {
			if pool := in.poolOf(svc); pool != "" && !slices.Contains(row.Pools, pool) {
				row.Pools = append(row.Pools, pool)
			}
		}

		switch {
		case !in.ciliumL2.works():
			row.Inactive = lbAdvertL2Disabled
		case !p.Spec.LoadBalancerIPs:
			row.Inactive = lbAdvertNoLBIPs
		case !p.allServices() && len(selected) == 0:
			row.Inactive = lbAdvertNoServiceMatch
		}

		slices.Sort(row.Pools)
		out = append(out, row)
	}

	return out
}
