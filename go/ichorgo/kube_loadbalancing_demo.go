package ichorgo

import "time"

// demoLoadBalancing is the demo cluster's load balancing, as the Services and checkup demos
// tell it: MetalLB's default pool is exhausted, so hello-ichor's public Service waits; the
// lab pool is advertised by nothing; the upstream router's BGP session is down on one
// worker; kube-vip holds the control plane's address.
func demoLoadBalancing() kubeLoadBalancing {
	now := time.Now()

	return kubeLoadBalancing{
		Controller:  lbControllerMetalLB,
		Controllers: []string{lbControllerMetalLB, lbControllerKubeVip},
		Pools: []lbPool{
			{
				Name: "default-pool", Controller: lbControllerMetalLB, Addresses: []string{"192.0.2.240/31"},
				Total: 2, Assigned: 2, Available: 0, CountsKnown: true, AutoAssign: true, Namespaces: []string{},
				AdvertisedBy: []string{"l2:default"}, Services: []string{"dns/blocky", "networking/traefik"}, Level: serviceCritical,
			},
			{
				Name: "lab-pool", Controller: lbControllerMetalLB, Addresses: []string{"198.51.100.16-198.51.100.23"},
				Total: 8, Assigned: 0, Available: 8, CountsKnown: true, AutoAssign: false, Namespaces: []string{"lab"},
				AdvertisedBy: []string{}, Services: []string{}, Level: serviceWarning,
			},
		},
		Advertisements: []lbAdvertisement{
			{
				Name: "default", Controller: lbControllerMetalLB, Kind: lbAdvertL2, Pools: []string{"default-pool"}, Interfaces: []string{"eth0"},
				Peers: []string{}, Communities: []string{},
			},
		},
		BGPPeers: []lbBGPPeer{
			{
				Name: "upstream-router", PeerAddress: "192.0.2.1", PeerASN: 64512, MyASN: 64513, HoldTime: "90s",
				Sessions:      []lbBGPSession{{Node: "demo-worker-1", Up: true}, {Node: "demo-worker-2", Up: false}},
				SessionsKnown: true, Level: serviceCritical,
			},
		},
		ConfigErrors: []lbConfigError{},
		Services: []lbService{
			{
				Namespace: "demo", Name: "hello-ichor-public", Addresses: []string{}, Pending: true, Cause: lbCausePoolExhausted,
				Interfaces: []string{}, Peers: []string{}, Since: now.Add(-50 * time.Minute).UnixMilli(), Level: serviceCritical,
			},
			demoLBService("dns", "blocky", "192.0.2.241", "demo-worker-2"),
			demoLBService("networking", "traefik", "192.0.2.240", "demo-worker-1"),
		},
		KubeVip: &lbKubeVip{
			Ranges: []lbKubeVipRange{},
			Leases: []lbKubeVipLease{{Name: "plndr-cp-lock", Namespace: "kube-system", Holder: "demo-cp-1", Renewed: now.Add(-3 * time.Second).UnixMilli()}},
		},
	}
}

func demoLBService(namespace, name, address, node string) lbService {
	return lbService{
		Namespace: namespace, Name: name, Addresses: []string{address}, Pool: "default-pool", AnnouncedBy: node,
		Interfaces: []string{"eth0"}, Peers: []string{}, Level: serviceOK,
	}
}
