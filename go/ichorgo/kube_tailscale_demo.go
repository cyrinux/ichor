package ichorgo

// demoTailscale is what the demo cluster's Tailscale operator answers: a subnet router, an
// exit node and an app connector whose ProxyClass is missing, an ingress and a kube-apiserver
// proxy group, a ProxyClass that does not validate, and the tailnet nameserver. Everything
// invented; nothing readable as a real tailnet.
func demoTailscale() tsReport {
	ok := tsState{Ready: true, Health: tsHealthOK}
	device := func(host, ip string) tsDevice {
		return tsDevice{Hostname: host, TailnetIPs: []string{ip}, StaticEndpoints: []string{}}
	}

	r := tsReport{
		Installed: true, Version: "v1alpha1",
		Operator: &tsOperator{
			Namespace: "tailscale", Name: "operator", Ready: 1, Desired: 1,
			Image: "tailscale/k8s-operator:v1.90.0", Health: tsHealthOK,
		},
		Connectors: []tsConnector{
			{
				Name: "office-router", Hostname: "office-router", TailnetIPs: []string{"100.64.0.11", "fd7a:115c:a1e0::b"},
				SubnetRoutes: "10.0.0.0/24", AdvertisedRoutes: []string{"10.0.0.0/24"}, AppConnectorRoutes: []string{},
				Tags: []string{"tag:k8s"}, ProxyClass: "prod", Replicas: 2,
				Devices: []tsDevice{device("office-router-0", "100.64.0.11"), device("office-router-1", "100.64.0.12")},
				tsState: ok,
			},
			{
				Name: "exit-eu", Hostname: "exit-eu", TailnetIPs: []string{"100.64.0.21"}, ExitNode: true,
				AdvertisedRoutes: []string{}, AppConnectorRoutes: []string{}, Tags: []string{"tag:k8s"}, Replicas: 1,
				Devices: []tsDevice{device("exit-eu", "100.64.0.21")}, tsState: ok,
			},
			{
				Name: "saas-connector", Hostname: "saas-connector", TailnetIPs: []string{}, AppConnector: true,
				AdvertisedRoutes: []string{}, AppConnectorRoutes: []string{"203.0.113.0/24"}, Tags: []string{"tag:k8s"},
				ProxyClass: "saas", Replicas: 1, Devices: []tsDevice{},
				tsState: tsState{
					Reason: tsReasonProxyClassMissing, Message: `ProxyClass "saas" does not exist`, Health: tsHealthCritical,
				},
			},
		},
		ProxyGroups: []tsProxyGroup{
			{
				Name: "ingress-proxies", Type: "ingress", Replicas: 2, ProxyClass: "prod", Tags: []string{"tag:k8s"},
				Devices:   []tsDevice{device("ingress-proxies-0", "100.64.0.31"), device("ingress-proxies-1", "100.64.0.32")},
				Available: true, tsState: ok,
			},
			{
				Name: "api-proxies", Type: "kube-apiserver", Replicas: 2, Tags: []string{"tag:k8s"},
				URL:       "https://api-proxies.example.ts.net",
				Devices:   []tsDevice{device("api-proxies-0", "100.64.0.41"), device("api-proxies-1", "100.64.0.42")},
				Available: true, tsState: ok,
			},
		},
		ProxyClasses: []tsProxyClass{
			{Name: "prod", tsState: ok},
			{Name: "metrics", tsState: tsState{
				Reason: "ProxyClassInvalid", Message: "spec.metrics.serviceMonitor needs the prometheus-operator CRDs", Health: tsHealthWarning,
			}},
		},
		DNSConfig: &tsDNSConfig{Name: "ts-dns", NameserverIP: "100.100.100.100", tsState: ok},
	}

	linkProxyClasses(&r, true)
	finishTailscale(&r)

	return r
}
