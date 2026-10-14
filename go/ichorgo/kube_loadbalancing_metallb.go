package ichorgo

import (
	"context"
	"slices"
	"sync"
)

// groupMetalLB is MetalLB's API group: pools, advertisements, BGP peers and their statuses.
const groupMetalLB = "metallb.io"

// The labels MetalLB puts on a ConfigurationState: which component reports, on which node.
const (
	metallbComponentLabel = "metallb.io/component-type"
	metallbNodeLabel      = "metallb.io/node-name"
)

// The annotations that pin a Service to a pool, and the one MetalLB sets once it allocated.
var (
	metallbPoolAnnotations = []string{"metallb.io/address-pool", "metallb.universe.tf/address-pool"}
	metallbAllocatedFrom   = "metallb.io/ip-allocated-from-pool"
)

type metallbPool struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Addresses         []string `json:"addresses"`
		AutoAssign        *bool    `json:"autoAssign"`
		ServiceAllocation struct {
			Priority           int             `json:"priority"`
			Namespaces         []string        `json:"namespaces"`
			NamespaceSelectors []labelSelector `json:"namespaceSelectors"`
			ServiceSelectors   []labelSelector `json:"serviceSelectors"`
		} `json:"serviceAllocation"`
	} `json:"spec"`
	Status *struct {
		AssignedIPv4  int `json:"assignedIPv4"`
		AssignedIPv6  int `json:"assignedIPv6"`
		AvailableIPv4 int `json:"availableIPv4"`
		AvailableIPv6 int `json:"availableIPv6"`
	} `json:"status"`
}

// metallbAdvertisement is an L2Advertisement or a BGPAdvertisement (Kind set when read).
type metallbAdvertisement struct {
	Kind     string    `json:"-"`
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		IPAddressPools         []string        `json:"ipAddressPools"`
		IPAddressPoolSelectors []labelSelector `json:"ipAddressPoolSelectors"`
		NodeSelectors          []labelSelector `json:"nodeSelectors"`
		Interfaces             []string        `json:"interfaces"`
		Peers                  []string        `json:"peers"`
		Communities            []string        `json:"communities"`
		LocalPref              int             `json:"localPref"`
		AggregationLength      int             `json:"aggregationLength"`
	} `json:"spec"`
}

type metallbPeer struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		PeerAddress   string          `json:"peerAddress"`
		Interface     string          `json:"interface"`
		PeerASN       int64           `json:"peerASN"`
		MyASN         int64           `json:"myASN"`
		DynamicASN    string          `json:"dynamicASN"`
		NodeSelectors []labelSelector `json:"nodeSelectors"`
		BFDProfile    string          `json:"bfdProfile"`
		EBGPMultiHop  bool            `json:"ebgpMultiHop"`
		HoldTime      string          `json:"holdTime"`
	} `json:"spec"`
}

// metallbServiceStatus is a ServiceL2Status or ServiceBGPStatus: who announces a Service.
type metallbServiceStatus struct {
	Status struct {
		Node             string `json:"node"`
		ServiceName      string `json:"serviceName"`
		ServiceNamespace string `json:"serviceNamespace"`
		Interfaces       []struct {
			Name string `json:"name"`
		} `json:"interfaces"`
		Peers []string `json:"peers"`
	} `json:"status"`
}

type metallbConfigState struct {
	Metadata checkMeta `json:"metadata"`
	Status   struct {
		Result       string `json:"result"` // Valid, Invalid, Unknown
		ErrorSummary string `json:"errorSummary"`
	} `json:"status"`
}

// metallbState is what MetalLB has; partial when some of it could not be read.
type metallbState struct {
	pools          []metallbPool
	advertisements []metallbAdvertisement
	peers          []metallbPeer
	serviceStatus  []metallbServiceStatus
	configStates   []metallbConfigState
	partial        bool
}

// readMetalLB reads MetalLB's objects together; kinds an older MetalLB lacks are skipped.
func readMetalLB(ctx context.Context, k *kubeClient) *metallbState {
	const base = "/apis/" + groupMetalLB + "/v1beta1/"

	var (
		m                   metallbState
		l2, bgp             []metallbAdvertisement
		l2Status, bgpStatus []metallbServiceStatus
		errs                [7]error
		wg                  sync.WaitGroup
	)

	wg.Go(func() { m.pools, errs[0] = listObjects[metallbPool](ctx, k, base+"ipaddresspools") })
	wg.Go(func() { l2, errs[1] = listObjects[metallbAdvertisement](ctx, k, base+"l2advertisements") })
	wg.Go(func() { bgp, errs[2] = listObjects[metallbAdvertisement](ctx, k, base+"bgpadvertisements") })
	wg.Go(func() { m.peers, errs[3] = readMetalLBPeers(ctx, k) })
	wg.Go(func() { l2Status, errs[4] = listObjects[metallbServiceStatus](ctx, k, base+"servicel2statuses") })
	wg.Go(func() { bgpStatus, errs[5] = listObjects[metallbServiceStatus](ctx, k, base+"servicebgpstatuses") })
	wg.Go(func() { m.configStates, errs[6] = listObjects[metallbConfigState](ctx, k, base+"configurationstates") })
	wg.Wait()

	for i := range l2 {
		l2[i].Kind = lbAdvertL2
	}

	for i := range bgp {
		bgp[i].Kind = lbAdvertBGP
	}

	m.advertisements = append(l2, bgp...)
	m.serviceStatus = append(l2Status, bgpStatus...)
	m.partial = slices.ContainsFunc(errs[:], func(err error) bool { return ignoreNotFound(err) != nil })

	return &m
}

// The kinds of advertisement.
const (
	lbAdvertL2  = "l2"
	lbAdvertBGP = "bgp"
)

// readMetalLBPeers reads the BGP peers, v1beta2 first, v1beta1 on an older MetalLB.
func readMetalLBPeers(ctx context.Context, k *kubeClient) ([]metallbPeer, error) {
	peers, err := listObjects[metallbPeer](ctx, k, "/apis/"+groupMetalLB+"/v1beta2/bgppeers")
	if isNotFound(err) {
		peers, err = listObjects[metallbPeer](ctx, k, "/apis/"+groupMetalLB+"/v1beta1/bgppeers")
	}

	return peers, err
}

// advertises tells whether a advertises pool: by name, by label, or every pool when it
// names none.
func (a metallbAdvertisement) advertises(pool metallbPool) bool {
	if a.allPools() {
		return true
	}

	if slices.Contains(a.Spec.IPAddressPools, pool.Metadata.Name) {
		return true
	}

	return slices.ContainsFunc(a.Spec.IPAddressPoolSelectors, func(s labelSelector) bool {
		return s.matches(labelSet(pool.Metadata.Labels))
	})
}

func (a metallbAdvertisement) allPools() bool {
	return len(a.Spec.IPAddressPools) == 0 && len(a.Spec.IPAddressPoolSelectors) == 0
}

// advertisers lists who advertises pool ("l2:<name>", "bgp:<name>").
func (m *metallbState) advertisers(pool metallbPool) []string {
	out := []string{}

	for _, a := range m.advertisements {
		if a.advertises(pool) {
			out = append(out, a.Kind+":"+a.Metadata.Name)
		}
	}

	return out
}

// configErrors lists the components whose configuration MetalLB refused.
func (m *metallbState) configErrors() []lbConfigError {
	out := []lbConfigError{}

	if m == nil {
		return out
	}

	for _, s := range m.configStates {
		if s.Status.Result != "Invalid" {
			continue
		}

		component := s.Metadata.Labels[metallbComponentLabel]
		if component == "" {
			component = s.Metadata.Name
		}

		out = append(out, lbConfigError{Component: component, Node: s.Metadata.Labels[metallbNodeLabel], Message: s.Status.ErrorSummary})
	}

	return out
}

func lbConfigErrors(m *metallbState) []lbConfigError { return m.configErrors() }

// lbAdvertisementRows is MetalLB's advertisements as the screen lists them.
func lbAdvertisementRows(m *metallbState) []lbAdvertisement {
	out := []lbAdvertisement{}

	if m == nil {
		return out
	}

	for _, a := range m.advertisements {
		out = append(out, lbAdvertisement{
			Name: a.Metadata.Name, Kind: a.Kind, Pools: nonNil(a.Spec.IPAddressPools), AllPools: a.allPools(),
			NodeSelectors: len(a.Spec.NodeSelectors), Interfaces: nonNil(a.Spec.Interfaces), Peers: nonNil(a.Spec.Peers),
			Communities: nonNil(a.Spec.Communities), LocalPref: a.Spec.LocalPref, AggregationLength: a.Spec.AggregationLength,
		})
	}

	return out
}

// lbPeerRows is MetalLB's BGP peers with the sessions the speakers report.
func lbPeerRows(m *metallbState, sessions bgpSessions) []lbBGPPeer {
	out := []lbBGPPeer{}

	if m == nil {
		return out
	}

	for _, p := range m.peers {
		row := lbBGPPeer{
			Name: p.Metadata.Name, PeerAddress: p.Spec.PeerAddress, Interface: p.Spec.Interface, PeerASN: p.Spec.PeerASN,
			MyASN: p.Spec.MyASN, DynamicASN: p.Spec.DynamicASN, NodeSelectors: len(p.Spec.NodeSelectors),
			BFDProfile: p.Spec.BFDProfile, EBGPMultiHop: p.Spec.EBGPMultiHop, HoldTime: p.Spec.HoldTime,
			Sessions: []lbBGPSession{}, SessionsKnown: sessions != nil, Level: serviceOK,
		}

		if sessions != nil {
			row.Sessions = sessions.of(p.Spec.PeerAddress)
		}

		if slices.ContainsFunc(row.Sessions, func(s lbBGPSession) bool { return !s.Up }) {
			row.Level = serviceCritical
		}

		out = append(out, row)
	}

	return out
}
