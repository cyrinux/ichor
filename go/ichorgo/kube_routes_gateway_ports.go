package ichorgo

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"sync"
)

// A Gateway listener's port is the one its controller listens on in the pod (Traefik's
// websecure entrypoint, 8443); clients reach it through the controller's LoadBalancer Service
// (443 → websecure). The URLs show that public port.

// lbPortService is a LoadBalancer Service, the fields that map its ports to the pods'.
type lbPortService struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Ports []struct {
			Name       string          `json:"name"`
			Port       int32           `json:"port"`
			Protocol   string          `json:"protocol"`
			TargetPort json.RawMessage `json:"targetPort"` // a number, a container port name, or unset (= port)
		} `json:"ports"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// portSlice is an EndpointSlice's ports: named like the Service's, numbered like the pods'.
type portSlice struct {
	Ports []struct {
		Name string `json:"name"`
		Port int32  `json:"port"`
	} `json:"ports"`
}

// publishedPort is a LoadBalancer Service port and the pod port it sends to.
type publishedPort struct {
	public, target int32
	addresses      []string
}

// withPublicPorts returns gateways, each with the public port of its listener ports that
// LoadBalancer Services expose under one port only. Best effort: without Services, the
// listener ports stay.
func withPublicPorts(ctx context.Context, k *kubeClient, gateways []gatewayObject) []gatewayObject {
	if !slices.ContainsFunc(gateways, hasNonDefaultListener) {
		return gateways
	}

	services, err := listObjects[lbPortService](ctx, k, "/api/v1/services?fieldSelector=spec.type%3DLoadBalancer")
	if err != nil || len(services) == 0 {
		return gateways
	}

	published := readPublishedPorts(ctx, k, services)
	out := slices.Clone(gateways)

	for i := range out {
		out[i].publicPorts = gatewayPublicPorts(out[i], published)
	}

	return out
}

// hasNonDefaultListener tells a Gateway with a listener off 80 and 443, the only ones whose
// public port can differ in practice.
func hasNonDefaultListener(gw gatewayObject) bool {
	return slices.ContainsFunc(gw.Spec.Listeners, func(l gatewayListener) bool { return l.Port != 80 && l.Port != 443 })
}

// readPublishedPorts lists the TCP ports of services with the pod port each sends to; a named
// target port is read from the Service's EndpointSlices.
func readPublishedPorts(ctx context.Context, k *kubeClient, services []lbPortService) []publishedPort {
	var (
		mu  sync.Mutex
		out []publishedPort
	)

	forEachNode(services, func(_ int, svc lbPortService) {
		var named map[string]int32

		var addresses []string
		for _, in := range svc.Status.LoadBalancer.Ingress {
			addresses = append(addresses, in.IP, in.Hostname)
		}

		for _, p := range svc.Spec.Ports {
			if p.Protocol != "" && p.Protocol != "TCP" {
				continue
			}

			target, name := p.Port, ""
			if len(p.TargetPort) > 0 && json.Unmarshal(p.TargetPort, &target) != nil && json.Unmarshal(p.TargetPort, &name) == nil {
				if named == nil {
					named = slicePortsOf(ctx, k, svc.Metadata.Namespace, svc.Metadata.Name)
				}

				if target = named[p.Name]; target == 0 {
					continue
				}
			}

			mu.Lock()
			out = append(out, publishedPort{public: p.Port, target: target, addresses: addresses})
			mu.Unlock()
		}
	})

	return out
}

// slicePortsOf maps the port names of a Service's EndpointSlices to the pod ports.
func slicePortsOf(ctx context.Context, k *kubeClient, namespace, service string) map[string]int32 {
	path := "/apis/discovery.k8s.io/v1/namespaces/" + url.PathEscape(namespace) + "/endpointslices?labelSelector=" +
		url.QueryEscape("kubernetes.io/service-name="+service)

	list, _ := listObjects[portSlice](ctx, k, path)
	ports := map[string]int32{}

	for _, s := range list {
		for _, p := range s.Ports {
			ports[p.Name] = p.Port
		}
	}

	return ports
}

// gatewayPublicPorts maps the listener ports of gw to the one public port the Services give
// each, those at the Gateway's addresses when it has some. A port published under several
// public ports (or none) is left out.
func gatewayPublicPorts(gw gatewayObject, published []publishedPort) map[int32]int32 {
	var addresses []string
	for _, a := range gw.Status.Addresses {
		addresses = append(addresses, a.Value)
	}

	candidates := map[int32][]int32{}

	for _, p := range published {
		if len(addresses) > 0 && !slices.ContainsFunc(p.addresses, func(a string) bool { return a != "" && slices.Contains(addresses, a) }) {
			continue
		}

		if !slices.Contains(candidates[p.target], p.public) {
			candidates[p.target] = append(candidates[p.target], p.public)
		}
	}

	ports := map[int32]int32{}

	for _, l := range gw.Spec.Listeners {
		if public := candidates[l.Port]; len(public) == 1 {
			ports[l.Port] = public[0]
		}
	}

	return ports
}
