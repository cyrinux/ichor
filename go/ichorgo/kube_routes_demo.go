package ichorgo

import (
	"slices"
	"strings"
	"time"
)

// demoRouteTable maps a demo pod name prefix to the route that serves it.
var demoRouteTable = []struct {
	prefix string
	route  kubeRoute
}{
	{"hello-ichor", routeRow("Ingress", "demo", "hello-ichor", "https://hello.home.example", "hello-ichor")},
	{"jellyfin-", routeRow("HTTPRoute", "media", "jellyfin", "https://jellyfin.home.example", "jellyfin")},
	{"immich-server-", routeRow("HTTPRoute", "media", "immich", "https://photos.home.example", "immich-server")},
	{"vaultwarden-", routeRow("Ingress", "default", "vaultwarden", "https://vault.home.example", "vaultwarden")},
	{"paperless-", routeRow("Ingress", "default", "paperless", "https://paperless.home.example", "paperless")},
	{"grafana-", routeRow("IngressRoute", "monitoring", "grafana", "https://grafana.home.example", "grafana")},
	{"uptime-kuma-", routeRow("HTTPRoute", "default", "uptime-kuma", "http://status.home.example:8080/dashboard", "uptime-kuma")},
	{"mosquitto-0", routeRow("TLSRoute", "home", "mosquitto", "https://mqtt.home.example", "mosquitto")},
}

// demoRoutes are the routes of the built-in demo cluster serving one of pods.
func demoRoutes(pods []routePod) []kubeRoute {
	var routes []kubeRoute

	for _, d := range demoRouteTable {
		if slices.ContainsFunc(pods, func(p routePod) bool {
			return p.Namespace == d.route.Namespace && strings.HasPrefix(p.Pod, d.prefix)
		}) {
			routes = append(routes, demoRouteCert(d.route, time.Now()))
		}
	}

	return uniqueRoutes(routes)
}

// demoRouteCert badges the demo's two certificates in trouble: Vaultwarden's expires in nine
// days, Paperless's names other hosts.
func demoRouteCert(r kubeRoute, now time.Time) kubeRoute {
	switch r.Name {
	case "vaultwarden":
		r.CertSecret, r.CertIssue, r.CertNotAfter = "default/vaultwarden-tls", routeCertExpiring, now.Add(9*24*time.Hour+time.Hour).Unix()
	case "paperless":
		r.CertSecret, r.CertIssue, r.CertNotAfter = "default/paperless-tls", routeCertHostMismatch, now.Add(200*24*time.Hour).Unix()
	}

	return r
}
