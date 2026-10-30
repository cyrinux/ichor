package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"time"
)

// The Load balancing screen: what gives LoadBalancer Services their address (MetalLB,
// Cilium's LB IPAM, kube-vip), its pools with their free addresses, who advertises them,
// MetalLB's BGP peers, kube-vip's ranges and leases, and why a Service still waits. Every
// read but the Services is best effort: what cannot be read is left out (PartialAccess).

// The controllers that hand out addresses, in the order they claim a Service without class.
const lbControllerKubeVip = "kube-vip"

// Why a LoadBalancer Service has no address, the first that applies (see lbCauseOf).
const (
	lbCauseNoController      = "noController"
	lbCauseConfigError       = "configError"
	lbCauseControllerDown    = "controllerDown"
	lbCauseNoPool            = "noPool"
	lbCausePoolNotMatched    = "poolNotMatched"
	lbCausePoolExhausted     = "poolExhausted"
	lbCausePoolNotAdvertised = "poolNotAdvertised"
	lbCauseUnknown           = "unknown"
)

// kubeLoadBalancing is KubeLoadBalancing's answer. Controller is the one that takes a
// Service without class ("" when none), Controllers every one found.
type kubeLoadBalancing struct {
	Controller     string            `json:"controller"`
	Controllers    []string          `json:"controllers"`
	Pools          []lbPool          `json:"pools"`
	Advertisements []lbAdvertisement `json:"advertisements"`
	BGPPeers       []lbBGPPeer       `json:"bgpPeers"`
	ConfigErrors   []lbConfigError   `json:"configErrors"`
	Services       []lbService       `json:"services"`
	KubeVip        *lbKubeVip        `json:"kubeVip"`
	PartialAccess  bool              `json:"partialAccess"`
}

// lbPool is an address pool. Assigned and Available come from the pool's status when
// CountsKnown, else from the Services whose address falls in it. AdvertisedBy: "l2:<name>",
// "bgp:<name>" (Cilium: its L2 policies only). Services: "namespace/name" of those it gave an
// address to.
type lbPool struct {
	Name         string   `json:"name"`
	Controller   string   `json:"controller"`
	Addresses    []string `json:"addresses"`
	Total        int      `json:"total"`
	Assigned     int      `json:"assigned"`
	Available    int      `json:"available"`
	CountsKnown  bool     `json:"countsKnown"`
	AutoAssign   bool     `json:"autoAssign"`
	Namespaces   []string `json:"namespaces"`
	Priority     int      `json:"priority,omitempty"`
	AdvertisedBy []string `json:"advertisedBy"`
	Services     []string `json:"services"`
	Level        string   `json:"level"`
}

// lbAdvertisement is a MetalLB L2Advertisement or BGPAdvertisement, or a
// CiliumL2AnnouncementPolicy (Controller "cilium"). Pools: the pools it names (Cilium: those
// of the Services it announces), empty when it covers every one (AllPools). Inactive: why a
// Cilium policy announces nothing (l2Disabled, noLoadBalancerIPs, noService).
type lbAdvertisement struct {
	Name              string   `json:"name"`
	Controller        string   `json:"controller"`
	Inactive          string   `json:"inactive,omitempty"`
	Kind              string   `json:"kind"` // l2 or bgp
	Pools             []string `json:"pools"`
	AllPools          bool     `json:"allPools"`
	NodeSelectors     int      `json:"nodeSelectors"`
	Interfaces        []string `json:"interfaces"`
	Peers             []string `json:"peers"`
	Communities       []string `json:"communities"`
	LocalPref         int      `json:"localPref,omitempty"`
	AggregationLength int      `json:"aggregationLength,omitempty"`
}

// lbBGPPeer is a MetalLB BGPPeer. Sessions come from the speakers' metrics; SessionsKnown is
// false when none could be read, and then nothing is said to be down.
type lbBGPPeer struct {
	Name          string         `json:"name"`
	PeerAddress   string         `json:"peerAddress,omitempty"`
	Interface     string         `json:"interface,omitempty"`
	PeerASN       int64          `json:"peerASN,omitempty"`
	MyASN         int64          `json:"myASN,omitempty"`
	DynamicASN    string         `json:"dynamicASN,omitempty"`
	NodeSelectors int            `json:"nodeSelectors"`
	BFDProfile    string         `json:"bfdProfile,omitempty"`
	EBGPMultiHop  bool           `json:"ebgpMultiHop"`
	HoldTime      string         `json:"holdTime,omitempty"`
	Sessions      []lbBGPSession `json:"sessions"`
	SessionsKnown bool           `json:"sessionsKnown"`
	Level         string         `json:"level"`
}

type lbBGPSession struct {
	Node string `json:"node"`
	Up   bool   `json:"up"`
}

// lbConfigError is a MetalLB component that refused its configuration.
type lbConfigError struct {
	Component string `json:"component"` // controller or speaker
	Node      string `json:"node,omitempty"`
	Message   string `json:"message"`
}

// lbService is a LoadBalancer Service: its addresses, the pool they came from, who announces
// them (MetalLB's ServiceL2Status / ServiceBGPStatus, the holder of Cilium's L2 lease) and,
// while it waits, why (Cause).
type lbService struct {
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	Class       string   `json:"class,omitempty"`
	Addresses   []string `json:"addresses"`
	Pool        string   `json:"pool,omitempty"`
	Pending     bool     `json:"pending"`
	Cause       string   `json:"cause,omitempty"`
	AnnouncedBy string   `json:"announcedBy,omitempty"`
	Interfaces  []string `json:"interfaces"`
	Peers       []string `json:"peers"`
	Since       int64    `json:"since,omitempty"` // unix ms, when it was made
	Level       string   `json:"level"`
}

// KubeLoadBalancing reads the load balancer controllers of the cluster (MetalLB, Cilium's LB
// IPAM, kube-vip), their pools, advertisements and BGP peers, and every LoadBalancer Service
// with why it has no address, as a JSON kubeLoadBalancing. It only reads. kubeServer: see KubePods.
func KubeLoadBalancing(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoLoadBalancing, func(ctx context.Context, k *kubeClient) (kubeLoadBalancing, error) {
		return readLoadBalancing(ctx, k, time.Now())
	})
}

func readLoadBalancing(ctx context.Context, k *kubeClient, now time.Time) (kubeLoadBalancing, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return kubeLoadBalancing{}, err
	}

	in, err := readLBInputs(ctx, k, groups, nil)
	if err != nil {
		return kubeLoadBalancing{}, err
	}

	return buildLoadBalancing(in, now), nil
}

// buildLoadBalancing turns what was read into the screen's rows, problems first.
func buildLoadBalancing(in lbInputs, now time.Time) kubeLoadBalancing {
	services := lbServiceRows(in, now)
	waiting := slices.ContainsFunc(services, func(s lbService) bool { return s.Pending })

	out := kubeLoadBalancing{
		Controllers:    in.controllers(),
		Pools:          lbPoolRows(in, waiting),
		Advertisements: append(lbAdvertisementRows(in.metallb), lbCiliumAdvertisementRows(in)...),
		BGPPeers:       lbPeerRows(in.metallb, in.sessions),
		ConfigErrors:   lbConfigErrors(in.metallb),
		Services:       services,
		KubeVip:        in.kubeVip.rows(),
		PartialAccess:  in.readErr != nil || in.podsErr != nil,
	}

	if len(out.Controllers) > 0 {
		out.Controller = out.Controllers[0]
	}

	sortByLevel(out.Pools, func(p lbPool) (string, string) { return p.Level, p.Name })
	sortByLevel(out.BGPPeers, func(p lbBGPPeer) (string, string) { return p.Level, p.Name })
	sortByLevel(out.Services, func(s lbService) (string, string) { return s.Level, s.Namespace + "/" + s.Name })

	return out
}

// sortByLevel puts the worst level first, then by name.
func sortByLevel[T any](items []T, key func(T) (level, name string)) {
	slices.SortStableFunc(items, func(a, b T) int {
		la, na := key(a)
		lb, nb := key(b)

		return cmp.Or(cmp.Compare(levelRank(lb), levelRank(la)), cmp.Compare(na, nb))
	})
}
