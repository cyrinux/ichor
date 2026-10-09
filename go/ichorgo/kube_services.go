package ichorgo

import (
	"context"
	"strings"
	"sync"
	"time"
)

// The Services screen: each Service with its type, cluster IP, load balancer address (or
// why it has none), how many endpoints are ready (EndpointSlices) and the URLs of the
// Ingresses and HTTPRoutes sending traffic to it, problems first. Only the Services are
// needed: an account that may not read slices or routes still gets its Services.

// Levels of a Service, as the Storage screen's (storageOK, storageWarning, storageCritical).
const (
	serviceOK       = storageOK
	serviceWarning  = storageWarning
	serviceCritical = storageCritical
)

// What gives LoadBalancer Services their address, for the hint of one without.
const (
	lbControllerMetalLB = "metallb"
	lbControllerCilium  = "cilium"
)

// kubeServices is KubeServices' answer. PartialAccess: EndpointSlices or routes could not be
// read, so some ready counts or URLs may be missing. LBController names what should give a
// LoadBalancer Service its address (metallb, cilium), set only when one waits for it.
type kubeServices struct {
	Services      []serviceRow `json:"services"`
	PartialAccess bool         `json:"partialAccess"`
	LBController  string       `json:"lbController,omitempty"`
}

// serviceRow is a Service as the screen shows it. Ports: kubectl's "80/TCP", with the node
// port for NodePort and LoadBalancer ("80:30080/TCP"). Addresses: the load balancer's IPs
// or hostnames and the external IPs. Endpoints and ReadyEndpoints count the slices' pods
// (EndpointsKnown: the slices were read); Selector: Kubernetes keeps them, not a person.
type serviceRow struct {
	Namespace      string      `json:"namespace"`
	Name           string      `json:"name"`
	Type           string      `json:"type"`
	ClusterIP      string      `json:"clusterIP,omitempty"`
	ExternalName   string      `json:"externalName,omitempty"`
	Ports          []string    `json:"ports"`
	Addresses      []string    `json:"addresses"`
	LBClass        string      `json:"lbClass,omitempty"`
	Pending        bool        `json:"pending"`
	Selector       bool        `json:"selector"`
	EndpointsKnown bool        `json:"endpointsKnown"`
	Endpoints      int         `json:"endpoints"`
	ReadyEndpoints int         `json:"readyEndpoints"`
	Routes         []kubeRoute `json:"routes"`
	// Level is critical (a load balancer without address), warning (no ready endpoint, some
	// not ready, an address still coming) or ok.
	Level string `json:"level"`
}

type serviceListObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Type              string            `json:"type"`
		ClusterIP         string            `json:"clusterIP"`
		ExternalName      string            `json:"externalName"`
		ExternalIPs       []string          `json:"externalIPs"`
		LoadBalancerClass string            `json:"loadBalancerClass"`
		Selector          map[string]string `json:"selector"`
		Ports             []struct {
			Port     int32  `json:"port"`
			NodePort int32  `json:"nodePort"`
			Protocol string `json:"protocol"`
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

// endpointSliceObject is an EndpointSlice: its Service (label), address family and endpoints.
type endpointSliceObject struct {
	Metadata    checkMeta `json:"metadata"`
	AddressType string    `json:"addressType"`
	Endpoints   []struct {
		Conditions struct {
			Ready *bool `json:"ready"` // unset means ready
		} `json:"conditions"`
	} `json:"endpoints"`
}

// serviceRoutes holds what serviceRows needs to find the URLs of a Service.
type serviceRoutes struct {
	ingresses  []ingressObject
	httpRoutes []httpRouteObject
	gateways   []gatewayObject
}

// KubeServices lists the Services of namespace ("" for all) with their addresses, ready
// endpoints and routes, problems first, as a JSON kubeServices.
func KubeServices(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoServices(namespace), func(ctx context.Context, k *kubeClient) (kubeServices, error) {
		return readServices(ctx, k, namespace, time.Now())
	})
}

func readServices(ctx context.Context, k *kubeClient, namespace string, now time.Time) (kubeServices, error) {
	services, err := listObjects[serviceListObject](ctx, k, scopedPath("/api/v1", namespace, "services"))
	if err != nil {
		return kubeServices{}, err
	}

	privacy.learnNamespaces(namespacesOf(services, func(s serviceListObject) string { return s.Metadata.Namespace }))

	// The rest only completes the rows: read together, each allowed to fail.
	var (
		endpointSlices          []endpointSliceObject
		routes                  serviceRoutes
		sliceErr, ingErr, hrErr error
		wg                      sync.WaitGroup
	)

	wg.Go(func() {
		endpointSlices, sliceErr = listObjects[endpointSliceObject](ctx, k, scopedPath("/apis/discovery.k8s.io/v1", namespace, "endpointslices"))
	})
	wg.Go(func() {
		routes.ingresses, ingErr = listObjects[ingressObject](ctx, k, scopedPath("/apis/networking.k8s.io/v1", namespace, "ingresses"))
	})
	// The Gateway API is optional: without its CRDs, no HTTPRoute.
	wg.Go(func() {
		routes.httpRoutes, hrErr = listObjects[httpRouteObject](ctx, k, scopedPath("/apis/"+groupGatewayAPI+"/v1", namespace, "httproutes"))
		hrErr = ignoreNotFound(hrErr)
	})
	// Gateways often live in their own namespace; only used for the scheme and port.
	wg.Go(func() {
		routes.gateways, _ = listObjects[gatewayObject](ctx, k, "/apis/"+groupGatewayAPI+"/v1/gateways")
	})
	wg.Wait()

	if len(routes.httpRoutes) > 0 {
		routes.gateways = withPublicPorts(ctx, k, routes.gateways)
	}

	var counts map[serviceRef]endpointCount
	if sliceErr == nil {
		counts = countEndpoints(endpointSlices)
	}

	out := kubeServices{
		Services:      joinServices(services, counts, routes, now),
		PartialAccess: sliceErr != nil || ingErr != nil || hrErr != nil,
	}

	if anyPending(out.Services) {
		out.LBController = readLBController(ctx, k)
	}

	return out, nil
}

// readLBController names the load balancer controller installed, by its API group: MetalLB
// first (it is installed for that alone), else Cilium's LB IPAM. "" when neither, or unknown.
func readLBController(ctx context.Context, k *kubeClient) string {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return ""
	}

	return lbControllerOf(groups)
}

func lbControllerOf(groups map[string]string) string {
	if _, ok := groups["metallb.io"]; ok {
		return lbControllerMetalLB
	}

	if _, ok := groups[groupCilium]; ok {
		return lbControllerCilium
	}

	return ""
}
