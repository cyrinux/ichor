package ichorgo

import (
	"slices"
	"strings"
)

// demoRouteTable maps a demo pod name prefix to the route that serves it.
var demoRouteTable = []struct {
	prefix string
	route  kubeRoute
}{
	{"hello-ichor", kubeRoute{Kind: "Ingress", Namespace: "demo", Name: "hello-ichor", URL: "https://hello.home.example", Service: "hello-ichor"}},
	{"jellyfin-", kubeRoute{Kind: "HTTPRoute", Namespace: "media", Name: "jellyfin", URL: "https://jellyfin.home.example", Service: "jellyfin"}},
	{"immich-server-", kubeRoute{Kind: "HTTPRoute", Namespace: "media", Name: "immich", URL: "https://photos.home.example", Service: "immich-server"}},
	{"vaultwarden-", kubeRoute{Kind: "Ingress", Namespace: "default", Name: "vaultwarden", URL: "https://vault.home.example", Service: "vaultwarden"}},
	{"paperless-", kubeRoute{Kind: "Ingress", Namespace: "default", Name: "paperless", URL: "https://paperless.home.example", Service: "paperless"}},
	{"uptime-kuma-", kubeRoute{Kind: "HTTPRoute", Namespace: "default", Name: "uptime-kuma", URL: "http://status.home.example:8080/dashboard", Service: "uptime-kuma"}},
}

// demoRoutes are the routes of the built-in demo cluster serving one of pods.
func demoRoutes(pods []routePod) []kubeRoute {
	var routes []kubeRoute

	for _, d := range demoRouteTable {
		if slices.ContainsFunc(pods, func(p routePod) bool {
			return p.Namespace == d.route.Namespace && strings.HasPrefix(p.Pod, d.prefix)
		}) {
			routes = append(routes, d.route)
		}
	}

	return uniqueRoutes(routes)
}
