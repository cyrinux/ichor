package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
)

// The findings of the certificates section about the certificates routes serve, outside
// cert-manager (its own are alerted by the Data services). Read from a TLS handshake with
// each host, never from the Secret (its private key would come along). Namespace, Name: the
// TLS Secret the route names; Extra: the first route using it ("<Kind> <ns>/<name>");
// Reason: the host it was read at.
const (
	findRouteCertExpired      = "routeCertExpired"
	findRouteCertExpiring     = "routeCertExpiring"     // Count: whole days left; Since: its expiry (unix ms)
	findRouteCertHostMismatch = "routeCertHostMismatch" // Message: the names it covers, comma-separated
)

// routeTLSRef is one route (or Gateway listener) serving hosts with a TLS Secret, at port.
type routeTLSRef struct {
	secret string // "namespace/name"
	hosts  []string
	port   int
	route  string // "<Kind> <ns>/<name>"
}

var routeCertFindingKind = map[string]string{
	routeCertExpired:      findRouteCertExpired,
	routeCertExpiring:     findRouteCertExpiring,
	routeCertHostMismatch: findRouteCertHostMismatch,
}

// checkupRouteCerts reads the certificate each host an Ingress, a Gateway listener or a
// Traefik IngressRoute terminates TLS for serves (once per host and port, the unreachable
// skipped), and finds those expired, expiring within certWarningWithin or not covering the
// host. A Secret cert-manager writes is skipped. Ingress and Traefik hosts are read on 443,
// a Gateway listener on its own port; a wildcard listener hostname is not probed.
func checkupRouteCerts(ctx context.Context, k *kubeClient, in checkupInput) checkupPart {
	ingresses, ingErr := listObjects[ingressObject](ctx, k, "/apis/networking.k8s.io/v1/ingresses")
	ingErr = ignoreNotFound(ingErr)

	var (
		gateways      []gatewayObject
		gwErr, irErr  error
		ingressRoutes []ingressRouteObject
	)

	if in.groups[groupGatewayAPI] != "" {
		gateways, gwErr = listObjects[gatewayObject](ctx, k, "/apis/"+groupGatewayAPI+"/v1/gateways")
		gwErr = ignoreNotFound(gwErr)
	}

	if in.groups[groupTraefik] != "" {
		var set traefikRouteSet

		set, irErr = readTraefikRoutes(ctx, k, "", false)
		ingressRoutes = set.http
	}

	managed := map[string]bool{}
	if in.groups[groupCertManager] != "" {
		managed = certManagerSecrets(ctx, k)
	}

	refs := slices.DeleteFunc(routeTLSRefs(ingresses, gateways, ingressRoutes), func(r routeTLSRef) bool { return managed[r.secret] })

	var targets []certTarget

	for _, r := range refs {
		for _, h := range probeableHosts(r.hosts) {
			targets = append(targets, certTarget{host: h, port: r.port})
		}
	}

	certs := probeRouteCerts(ctx, targets, in.now)

	return checkupPart{
		served: true, checked: len(certs), findings: routeCertFindings(refs, certs, in.now),
		errs: slices.DeleteFunc([]error{ingErr, gwErr, irErr}, func(err error) bool { return err == nil }),
	}
}

// probeableHosts are the hosts a handshake can name (probeableHost).
func probeableHosts(hosts []string) []string {
	return slices.DeleteFunc(slices.Clone(hosts), func(h string) bool { return !probeableHost(h) })
}

// ingressTLSHosts are the hosts a tls entry serves that a handshake can name: its own, and
// the rule hosts its wildcards (or an entry without hosts) cover.
func ingressTLSHosts(ing ingressObject, tlsHosts []string) []string {
	hosts := probeableHosts(tlsHosts)

	for _, r := range ing.Spec.Rules {
		if !probeableHost(r.Host) || slices.Contains(hosts, r.Host) {
			continue
		}

		if len(tlsHosts) == 0 || slices.ContainsFunc(tlsHosts, func(h string) bool { return strings.Contains(h, "*") && hostMatches(h, r.Host) }) {
			hosts = append(hosts, r.Host)
		}
	}

	return hosts
}

// routeTLSRefs lists the TLS Secrets of Ingress tls entries, Gateway listeners and Traefik
// IngressRoutes, with the hosts each serves.
func routeTLSRefs(ingresses []ingressObject, gateways []gatewayObject, ingressRoutes []ingressRouteObject) []routeTLSRef {
	var refs []routeTLSRef

	for _, ing := range ingresses {
		for _, t := range ing.Spec.TLS {
			if t.SecretName != "" {
				refs = append(refs, routeTLSRef{
					secret: ing.Metadata.Namespace + "/" + t.SecretName, hosts: ingressTLSHosts(ing, t.Hosts), port: 443,
					route: "Ingress " + ing.Metadata.Namespace + "/" + ing.Metadata.Name,
				})
			}
		}
	}

	for _, g := range gateways {
		for _, l := range g.Spec.Listeners {
			l.namespace = g.Metadata.Namespace
			if s := l.secretRef(); s != "" {
				refs = append(refs, routeTLSRef{
					secret: s, hosts: []string{l.Hostname}, port: int(cmp.Or(l.Port, 443)),
					route: "Gateway " + g.Metadata.Namespace + "/" + g.Metadata.Name,
				})
			}
		}
	}

	for _, ir := range ingressRoutes {
		s := ingressRouteTLSSecret(ir)
		if s == "" {
			continue
		}

		var hosts []string
		for _, r := range ir.Spec.Routes {
			h, _ := traefikMatch(r.Match)
			hosts = append(hosts, h...)
		}

		refs = append(refs, routeTLSRef{secret: s, hosts: hosts, port: 443, route: "IngressRoute " + ir.Metadata.Namespace + "/" + ir.Metadata.Name})
	}

	slices.SortStableFunc(refs, func(a, b routeTLSRef) int { return strings.Compare(a.secret, b.secret) })

	return refs
}

// routeCertFindings is one finding per Secret and kind of trouble (many routes and hosts may
// share a wildcard Secret); a host that could not be read says nothing.
func routeCertFindings(refs []routeTLSRef, certs map[certTarget]routeCert, now time.Time) []checkupFinding {
	findings := []checkupFinding{}
	seen := map[string]bool{}

	for _, ref := range refs {
		for _, host := range probeableHosts(ref.hosts) {
			c, ok := certs[certTarget{host: host, port: ref.port}]
			if !ok {
				continue
			}

			kind := routeCertFindingKind[routeCertIssue(c, host, now)]
			if kind == "" || seen[ref.secret+"|"+kind] {
				continue
			}

			seen[ref.secret+"|"+kind] = true

			ns, name, _ := strings.Cut(ref.secret, "/")
			f := checkupFinding{Kind: kind, Severity: sevWarning, Namespace: ns, Name: name, Extra: ref.route, Reason: host}

			switch kind {
			case findRouteCertExpired:
				f.Severity, f.Since = sevCritical, c.NotAfter*1000
			case findRouteCertExpiring:
				f.Count, f.Since = int(time.Unix(c.NotAfter, 0).Sub(now)/(24*time.Hour)), c.NotAfter*1000
			case findRouteCertHostMismatch:
				f.Message = strings.Join(c.DNSNames, ", ")
			}

			findings = append(findings, f)
		}
	}

	return findings
}
