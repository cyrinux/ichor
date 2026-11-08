package ichorgo

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// What is wrong with the certificate a route serves (kubeRoute.CertIssue), worst first.
const (
	routeCertExpired      = "expired"
	routeCertHostMismatch = "hostMismatch" // includes the proxy's default certificate (a Secret it cannot find)
	routeCertExpiring     = "expiring"     // within certWarningWithin
)

// The certificate a route serves is read from a TLS handshake with its host, as a browser
// sees it: never from the Secret, which would bring its private key to the phone.
const (
	routeCertProbeTimeout = 3 * time.Second
	routeCertProbeWorkers = 8
	// routeCertProbeMax bounds the hosts one read probes: the checkup runs at every
	// background check.
	routeCertProbeMax = 40
	// routeCertCacheTTL keeps a host's answer (or its silence) between reads: a certificate
	// changes rarely and the background checks run often.
	routeCertCacheTTL = 30 * time.Minute
)

// routeCert is the leaf certificate a host serves.
type routeCert struct {
	NotAfter int64 // unix seconds
	Issuer   string
	DNSNames []string
}

// certTarget is where a certificate is read: a host and its TLS port.
type certTarget struct {
	host string
	port int
}

// routeCertDialAddr is where a target is dialled; tests point it at a local server.
var routeCertDialAddr = func(t certTarget) string { return net.JoinHostPort(t.host, strconv.Itoa(t.port)) }

type cachedRouteCert struct {
	cert routeCert
	ok   bool
	at   time.Time
}

var routeCertCache = struct {
	sync.Mutex
	m map[certTarget]cachedRouteCert
}{m: map[certTarget]cachedRouteCert{}}

// probeRouteCert reads the leaf certificate t's host serves for its name (SNI); false when it
// cannot be reached. Nothing is sent but the handshake, and the chain is only inspected, so
// it is not verified (an expired or mismatched certificate is what is looked for).
func probeRouteCert(ctx context.Context, t certTarget, now time.Time) (routeCert, bool) {
	routeCertCache.Lock()
	cached, hit := routeCertCache.m[t]
	routeCertCache.Unlock()

	if hit && now.Sub(cached.at) < routeCertCacheTTL {
		return cached.cert, cached.ok
	}

	c, ok := dialRouteCert(ctx, t)
	if ctx.Err() != nil {
		// The caller gave up (a screen closed, the call's deadline): the host was not judged.
		return c, ok
	}

	routeCertCache.Lock()
	for key, e := range routeCertCache.m {
		if now.Sub(e.at) >= routeCertCacheTTL {
			delete(routeCertCache.m, key)
		}
	}
	routeCertCache.m[t] = cachedRouteCert{cert: c, ok: ok, at: now}
	routeCertCache.Unlock()

	return c, ok
}

func dialRouteCert(ctx context.Context, t certTarget) (routeCert, bool) {
	ctx, cancel := context.WithTimeout(ctx, routeCertProbeTimeout)
	defer cancel()

	// Not verified: only the served certificate is read, nothing is sent.
	cfg := httpx.BaseTLS(nil, true)
	cfg.ServerName = t.host

	d := tls.Dialer{Config: cfg}

	conn, err := d.DialContext(ctx, "tcp", routeCertDialAddr(t))
	if err != nil {
		return routeCert{}, false
	}
	defer conn.Close()

	tc, ok := conn.(*tls.Conn)
	if !ok || len(tc.ConnectionState().PeerCertificates) == 0 {
		return routeCert{}, false
	}

	leaf := tc.ConnectionState().PeerCertificates[0]
	learnCertNames(&kubeCertInfo{DNSNames: leaf.DNSNames})

	return routeCert{NotAfter: leaf.NotAfter.Unix(), Issuer: leaf.Issuer.String(), DNSNames: leaf.DNSNames}, true
}

// probeRouteCerts reads each target once, a few at a time, at most routeCertProbeMax of them
// (the first, in sorted order); the unreachable are left out.
func probeRouteCerts(ctx context.Context, targets []certTarget, now time.Time) map[certTarget]routeCert {
	slices.SortFunc(targets, func(a, b certTarget) int {
		if c := strings.Compare(a.host, b.host); c != 0 {
			return c
		}

		return a.port - b.port
	})
	targets = slices.Compact(targets)
	targets = targets[:min(len(targets), routeCertProbeMax)]

	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		certs = map[certTarget]routeCert{}
		slots = make(chan struct{}, routeCertProbeWorkers)
	)

	for _, t := range targets {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			if c, ok := probeRouteCert(ctx, t, now); ok {
				mu.Lock()
				certs[t] = c
				mu.Unlock()
			}
		})
	}

	wg.Wait()

	return certs
}

// routeCertIssue is what is wrong with c for a route reached at host: expired, a host it
// does not cover, expiring soon, or "".
func routeCertIssue(c routeCert, host string, now time.Time) string {
	switch {
	case c.NotAfter <= now.Unix():
		return routeCertExpired
	case host != "" && !certCovers(c.DNSNames, host):
		return routeCertHostMismatch
	case time.Unix(c.NotAfter, 0).Sub(now) < certWarningWithin:
		return routeCertExpiring
	}

	return ""
}

// certCovers tells whether one of sans names host: the same name, or a wildcard for exactly
// one more label (*.example.net covers a.example.net, not a.b.example.net nor example.net).
func certCovers(sans []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))

	for _, san := range sans {
		san = strings.ToLower(san)
		if san == host {
			return true
		}

		if suffix, ok := strings.CutPrefix(san, "*."); ok {
			if label, rest, found := strings.Cut(host, "."); found && label != "" && rest == suffix {
				return true
			}
		}
	}

	return false
}

// routeCertTarget is where the certificate of an https route is read; false for http, a
// wildcard, an IP address (no SNI: the proxy would answer with its default certificate) or a
// host-less URL.
func routeCertTarget(r kubeRoute) (certTarget, bool) {
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme != "https" || !probeableHost(u.Hostname()) {
		return certTarget{}, false
	}

	port := 443
	if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
		port = p
	}

	return certTarget{host: u.Hostname(), port: port}, true
}

// probeableHost is a name a handshake can carry by SNI: not empty, no wildcard, not an IP.
func probeableHost(h string) bool {
	return h != "" && !strings.Contains(h, "*") && net.ParseIP(h) == nil
}

// fillRouteCerts gives each https route the expiry and issue of the certificate its host
// serves, its TLS Secret and whether cert-manager writes it; an unreachable host keeps none.
func fillRouteCerts(routes []kubeRoute, certs map[certTarget]routeCert, managed map[string]bool, now time.Time) {
	for i := range routes {
		r := &routes[i]

		t, ok := routeCertTarget(*r)
		if !ok {
			continue
		}

		c, ok := certs[t]
		if !ok {
			continue
		}

		r.CertNotAfter, r.CertIssue = c.NotAfter, routeCertIssue(c, t.host, now)
		r.CertSecret, r.CertManaged = r.tlsSecret, managed[r.tlsSecret]
	}
}

// withRouteCerts badges routes with the certificates their hosts serve; nothing here fails
// the routes (an unreachable host keeps no badge).
func withRouteCerts(ctx context.Context, k *kubeClient, routes []kubeRoute, now time.Time) []kubeRoute {
	var targets []certTarget

	for _, r := range routes {
		if t, ok := routeCertTarget(r); ok {
			targets = append(targets, t)
		}
	}

	if len(targets) == 0 {
		return routes
	}

	fillRouteCerts(routes, probeRouteCerts(ctx, targets, now), certManagerSecrets(ctx, k), now)

	return routes
}

// certManagerSecrets are the Secrets cert-manager Certificates write ("namespace/name"):
// read from the Certificates, which hold no key. Empty without cert-manager.
func certManagerSecrets(ctx context.Context, k *kubeClient) map[string]bool {
	out := map[string]bool{}

	certs, err := listObjects[certificateObject](ctx, k, "/apis/"+groupCertManager+"/v1/certificates")
	if err != nil {
		return out
	}

	for _, c := range certs {
		if c.Spec.SecretName != "" {
			out[c.Metadata.Namespace+"/"+c.Spec.SecretName] = true
		}
	}

	return out
}

// ingressTLSSecret is the Secret of the ing's tls entry naming host ("namespace/name"), or of
// an entry without hosts; "" when none serves it.
func ingressTLSSecret(ing ingressObject, host string) string {
	for _, t := range ing.Spec.TLS {
		if t.SecretName == "" {
			continue
		}

		if len(t.Hosts) == 0 || slices.ContainsFunc(t.Hosts, func(h string) bool { return hostMatches(h, host) }) {
			return ing.Metadata.Namespace + "/" + t.SecretName
		}
	}

	return ""
}

// listenerTLSSecret is the first Secret of the HTTPS or TLS listener serving host; "" when
// none terminates TLS for it with a Secret.
func listenerTLSSecret(listeners []gatewayListener, host string) string {
	for _, l := range listeners {
		if l.Hostname != "" && !hostMatches(l.Hostname, host) || l.TLS == nil {
			continue
		}

		if s := l.secretRef(); s != "" {
			return s
		}
	}

	return ""
}

// secretRef is the listener's first certificate Secret, "namespace/name" (the Gateway's
// namespace when the ref names none); "" when it has none.
func (l gatewayListener) secretRef() string {
	if l.TLS == nil {
		return ""
	}

	for _, ref := range l.TLS.CertificateRefs {
		if (ref.Kind == "" || ref.Kind == "Secret") && (ref.Group == "" || ref.Group == "core") && ref.Name != "" {
			return orDefault(ref.Namespace, l.namespace) + "/" + ref.Name
		}
	}

	return ""
}

// ingressRouteTLSSecret is the IngressRoute's tls.secretName, "" without one.
func ingressRouteTLSSecret(ir ingressRouteObject) string {
	if ir.Spec.TLS == nil || ir.Spec.TLS.SecretName == "" {
		return ""
	}

	return ir.Metadata.Namespace + "/" + ir.Spec.TLS.SecretName
}
