package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// kubeRoute is an address an app is reachable at from outside the cluster: a host of an
// Ingress or of a Gateway API HTTPRoute whose backend is a Service selecting its pods.
type kubeRoute struct {
	Kind      string `json:"kind"` // Ingress or HTTPRoute
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Service   string `json:"service"` // the backend Service, in Namespace
}

type kubeRouteList struct {
	Routes []kubeRoute `json:"routes"`
}

// routePod names one pod of an app, as the inventory gives it.
type routePod struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
}

// decodeRoutePods reads the pods the app sends ([{namespace,pod}]) and maps masked names back
// to the real ones, learned when they were handed out (the inventory): an extra mask word
// cannot be revealed from the fake alone.
func decodeRoutePods(pods string) ([]routePod, error) {
	var refs []routePod
	if err := json.Unmarshal([]byte(pods), &refs); err != nil {
		return nil, fmt.Errorf("invalid pod list: %w", err)
	}

	for i, r := range refs {
		refs[i] = routePod{Namespace: privacy.revealNamespace(r.Namespace), Pod: privacy.revealName(r.Pod)}
	}

	return refs, nil
}

// serviceRef is a Service by namespace and name.
type serviceRef struct{ namespace, name string }

// KubeAppRoutes lists the URLs the given pods are served at, through the Kubernetes API
// with the admin kubeconfig Talos issues (os:admin): the Services selecting them, then the
// Ingresses and HTTPRoutes (gateway.networking.k8s.io/v1, when installed) sending traffic to
// those Services. pods: [{namespace,pod}]. {"routes":[{kind,namespace,name,url,service}]}.
// kubeServer: see KubePods.
func KubeAppRoutes(configYAML, contextName, kubeServer, pods string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	refs, err := decodeRoutePods(pods)
	if err != nil {
		return "", err
	}

	demo := func() kubeRouteList { return kubeRouteList{Routes: demoRoutes(refs)} }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (kubeRouteList, error) {
		return appRoutes(ctx, k, refs)
	})
}

func appRoutes(ctx context.Context, k *kubeClient, pods []routePod) (kubeRouteList, error) {
	services, err := servicesOfPods(ctx, k, pods)
	if err != nil || len(services) == 0 {
		return kubeRouteList{Routes: []kubeRoute{}}, err
	}

	var (
		ingresses  kubeList[ingressObject]
		httpRoutes kubeList[httpRouteObject]
		gateways   kubeList[gatewayObject]
	)

	errs := make([]error, 3)

	var wg sync.WaitGroup

	wg.Go(func() { errs[0] = getList(ctx, k, "/apis/networking.k8s.io/v1/ingresses", &ingresses) })
	// The Gateway API is optional: without its CRDs, no HTTPRoute.
	wg.Go(func() {
		errs[1] = ignoreNotFound(getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/httproutes", &httpRoutes))
	})
	// Only used to tell http from https: a URL is still worth showing without it.
	wg.Go(func() { _ = getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/gateways", &gateways) })
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return kubeRouteList{}, err
	}

	routes := append(ingressRoutes(ingresses.Items, services), httpRouteRoutes(httpRoutes.Items, gateways.Items, services)...)

	return kubeRouteList{Routes: uniqueRoutes(routes)}, nil
}

type labeledObject struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

type serviceObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Selector map[string]string `json:"selector"`
	} `json:"spec"`
}

// servicesOfPods finds the Services whose selector matches the labels of one of pods.
func servicesOfPods(ctx context.Context, k *kubeClient, pods []routePod) (map[serviceRef]bool, error) {
	wanted := map[string]map[string]bool{} // namespace -> pod names

	for _, p := range pods {
		if validateKubeName("pod", p.Namespace, p.Pod) != nil {
			continue
		}

		if wanted[p.Namespace] == nil {
			wanted[p.Namespace] = map[string]bool{}
		}

		wanted[p.Namespace][p.Pod] = true
	}

	found := map[serviceRef]bool{}

	for _, ns := range slices.Sorted(maps.Keys(wanted)) {
		base := "/api/v1/namespaces/" + url.PathEscape(ns)

		podList, err := readLabeledPods(ctx, k, ns, wanted[ns])
		if err != nil {
			return nil, err
		}

		var svcList kubeList[serviceObject]
		if err := getList(ctx, k, base+"/services", &svcList); err != nil {
			return nil, err
		}

		for _, pod := range podList.Items {
			if !wanted[ns][pod.Metadata.Name] {
				continue
			}

			for _, svc := range svcList.Items {
				if selects(svc.Spec.Selector, pod.Metadata.Labels) {
					found[serviceRef{ns, svc.Metadata.Name}] = true
				}
			}
		}
	}

	return found, nil
}

// routePodsByName is how many pods of a namespace readLabeledPods reads one by one before
// it lists the namespace instead.
const routePodsByName = 8

// readLabeledPods reads the labels of the pods names of namespace: one GET each when they are
// few (a pod gone since is skipped), else the namespace's list page by page.
func readLabeledPods(ctx context.Context, k *kubeClient, namespace string, names map[string]bool) (kubeList[labeledObject], error) {
	var list kubeList[labeledObject]

	base := scopedPath("/api/v1", namespace, "pods")
	if len(names) > routePodsByName {
		err := getList(ctx, k, base, &list)

		return list, err
	}

	for _, name := range slices.Sorted(maps.Keys(names)) {
		var pod labeledObject

		switch err := k.get(ctx, base+"/"+url.PathEscape(name), &pod); {
		case isNotFound(err):
			continue
		case err != nil:
			return list, err
		}

		list.Items = append(list.Items, pod)
	}

	return list, nil
}

// selects tells whether a Service selector matches labels; an empty one selects nothing
// (its endpoints are managed by hand).
func selects(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}

	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}

	return true
}

// routeURL is scheme://host[:port][path], the path left out when it is the root.
func routeURL(scheme, host string, port int32, path string) string {
	u := url.URL{Scheme: scheme, Host: host}
	if strings.Contains(host, ":") {
		u.Host = "[" + host + "]" // an IPv6 load balancer address
	}

	if port > 0 && !(scheme == "http" && port == 80) && !(scheme == "https" && port == 443) {
		u.Host = net.JoinHostPort(host, strconv.Itoa(int(port)))
	}

	if strings.Trim(path, "/") != "" {
		u.Path = path
	}

	return u.String()
}

// openableHost keeps a host a browser can open: not a wildcard, not empty.
func openableHost(host string) bool {
	return host != "" && !strings.Contains(host, "*")
}

// hostMatches tells whether host is pattern or under its wildcard ("*.example.com").
func hostMatches(pattern, host string) bool {
	if suffix, ok := strings.CutPrefix(pattern, "*"); ok {
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}

	return strings.EqualFold(pattern, host)
}

// uniqueRoutes drops repeated URLs (the first one wins) and sorts by URL.
func uniqueRoutes(routes []kubeRoute) []kubeRoute {
	seen := map[string]bool{}
	out := []kubeRoute{}

	for _, r := range routes {
		if seen[r.URL] {
			continue
		}

		seen[r.URL] = true
		out = append(out, r)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].URL < out[j].URL })

	return out
}
