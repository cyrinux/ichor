package ichorgo

// The demo's Services, as the checkup and routes demos describe them: hello-ichor's public
// load balancer still without an address (Cilium's pool is exhausted), a worker with no
// ready pod, the Traefik entry point and the routed apps.
func demoServices(namespace string) func() kubeServices {
	return func() kubeServices {
		svc := func(ns, name, typ, clusterIP string, ready, total int, ports ...string) serviceRow {
			return serviceRow{
				Namespace: ns, Name: name, Type: typ, ClusterIP: clusterIP, Ports: append([]string{}, ports...),
				Addresses: []string{}, Selector: true, EndpointsKnown: true, Endpoints: total, ReadyEndpoints: ready,
				Routes: []kubeRoute{},
			}
		}

		routed := func(s serviceRow) serviceRow {
			for _, d := range demoRouteTable {
				if d.route.Namespace == s.Namespace && d.route.Service == s.Name {
					s.Routes = append(s.Routes, d.route)
				}
			}

			return s
		}

		public := svc("demo", "hello-ichor-public", "LoadBalancer", "10.96.40.17", 2, 2, "80:31080/TCP")
		public.Pending = true

		traefik := svc("networking", "traefik", "LoadBalancer", "10.96.0.80", 1, 1, "80:30080/TCP", "443:30443/TCP")
		traefik.Addresses = []string{"192.0.2.240"}

		services := []serviceRow{
			routed(svc("demo", "hello-ichor", "ClusterIP", "10.96.12.40", 2, 2, "80/TCP")),
			public,
			svc("demo", "postgres", "ClusterIP", "None", 1, 1, "5432/TCP"),
			svc("demo", "worker", "ClusterIP", "10.96.33.9", 0, 1, "8080/TCP"),
			routed(svc("media", "jellyfin", "ClusterIP", "10.96.51.2", 1, 1, "8096/TCP")),
			routed(svc("media", "immich-server", "ClusterIP", "10.96.51.8", 1, 1, "2283/TCP")),
			routed(svc("default", "vaultwarden", "ClusterIP", "10.96.70.4", 1, 1, "80/TCP")),
			traefik,
		}

		for i := range services {
			services[i].Level = serviceLevel(services[i], true)
		}

		services = inNamespace(services, namespace, func(s serviceRow) string { return s.Namespace })
		sortServices(services)

		out := kubeServices{Services: services}
		if anyPending(services) {
			out.LBController = lbControllerCilium
		}

		return out
	}
}
