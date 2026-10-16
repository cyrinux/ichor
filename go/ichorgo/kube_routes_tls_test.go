package ichorgo

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

var routeCertNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

// testTLSHosts serves, for each host, a self-signed certificate valid until its notAfter and
// naming its names, and points routeCertDialAddr at the local listener. The cache is emptied.
func testTLSHosts(t *testing.T, hosts map[string]testTLSCert) {
	t.Helper()

	certs := map[string]tls.Certificate{}
	for host, c := range hosts {
		certs[host] = testTLSCertificate(t, c.notAfter, c.names...)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			c, ok := certs[hello.ServerName]
			if !ok {
				return nil, net.ErrClosed
			}

			return &c, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go func() {
				_ = conn.(*tls.Conn).Handshake()
				_ = conn.Close()
			}()
		}
	}()

	addr, dial := ln.Addr().String(), routeCertDialAddr
	routeCertDialAddr = func(certTarget) string { return addr }

	t.Cleanup(func() {
		routeCertDialAddr = dial
		_ = ln.Close()
		resetRouteCertCache()
	})
	resetRouteCertCache()
}

type testTLSCert struct {
	notAfter time.Time
	names    []string
}

func resetRouteCertCache() {
	routeCertCache.Lock()
	routeCertCache.m = map[certTarget]cachedRouteCert{}
	routeCertCache.Unlock()
}

func testTLSCertificate(t *testing.T, notAfter time.Time, names ...string) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names,
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestRouteCertIssue(t *testing.T) {
	future := routeCertNow.Add(90 * 24 * time.Hour).Unix()

	for _, c := range []struct {
		name string
		cert routeCert
		host string
		want string
	}{
		{"fine", routeCert{NotAfter: future, DNSNames: []string{"app.home.example"}}, "app.home.example", ""},
		{"expired", routeCert{NotAfter: routeCertNow.Add(-time.Hour).Unix(), DNSNames: []string{"x.example"}}, "other.example", routeCertExpired},
		{"expiring", routeCert{NotAfter: routeCertNow.Add(5 * 24 * time.Hour).Unix(), DNSNames: []string{"app.home.example"}}, "app.home.example", routeCertExpiring},
		{"wildcard", routeCert{NotAfter: future, DNSNames: []string{"*.home.example"}}, "App.Home.Example", ""},
		{"wildcard one label only", routeCert{NotAfter: future, DNSNames: []string{"*.home.example"}}, "a.b.home.example", routeCertHostMismatch},
		{"mismatch before expiring", routeCert{NotAfter: routeCertNow.Add(5 * 24 * time.Hour).Unix(), DNSNames: []string{"lan"}}, "app.home.example", routeCertHostMismatch},
		{"no host never mismatches", routeCert{NotAfter: future, DNSNames: []string{"lan"}}, "", ""},
	} {
		if got := routeCertIssue(c.cert, c.host, routeCertNow); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The certificate comes from the handshake (by SNI), never from a Secret; an unreachable
// host is left out, and a host is dialled once while its answer is fresh.
func TestProbeRouteCerts(t *testing.T) {
	testTLSHosts(t, map[string]testTLSCert{"app.home.example": {time.Now().Add(30 * 24 * time.Hour), []string{"app.home.example"}}})

	now := time.Now()
	targets := []certTarget{{"app.home.example", 443}, {"app.home.example", 443}, {"gone.home.example", 443}}

	certs := probeRouteCerts(context.Background(), targets, now)
	if c, ok := certs[certTarget{"app.home.example", 443}]; !ok || c.NotAfter == 0 || len(c.DNSNames) != 1 {
		t.Fatalf("certs %+v", certs)
	}

	if _, ok := certs[certTarget{"gone.home.example", 443}]; ok {
		t.Error("an unreachable host answered")
	}

	routeCertDialAddr = func(certTarget) string { return "127.0.0.1:1" }
	if _, ok := probeRouteCert(context.Background(), certTarget{"app.home.example", 443}, now.Add(time.Minute)); !ok {
		t.Error("a fresh answer was not kept")
	}
}

func TestIngressTLSSecret(t *testing.T) {
	var ing ingressObject
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"app","namespace":"web"},"spec":{"tls":[
		{"hosts":["a.home.example"],"secretName":"a-tls"},{"hosts":["*.lan"],"secretName":"lan-tls"},{"secretName":""}]}}`), &ing); err != nil {
		t.Fatal(err)
	}

	for host, want := range map[string]string{"a.home.example": "web/a-tls", "x.lan": "web/lan-tls", "b.home.example": ""} {
		if got := ingressTLSSecret(ing, host); got != want {
			t.Errorf("%s: %q, want %q", host, got, want)
		}
	}
}

func TestListenerSecretRef(t *testing.T) {
	var g gatewayObject
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"public","namespace":"gw"},"spec":{"listeners":[
		{"name":"https","hostname":"*.home.example","protocol":"HTTPS","tls":{"certificateRefs":[{"kind":"Secret","name":"wild-tls"}]}},
		{"name":"other","hostname":"x.lan","protocol":"HTTPS","tls":{"certificateRefs":[{"name":"lan","namespace":"certs"}]}},
		{"name":"http","protocol":"HTTP"}]}}`), &g); err != nil {
		t.Fatal(err)
	}

	listeners := g.Spec.Listeners
	for i := range listeners {
		listeners[i].namespace = g.Metadata.Namespace
	}

	for host, want := range map[string]string{"app.home.example": "gw/wild-tls", "x.lan": "certs/lan", "y.other": ""} {
		if got := listenerTLSSecret(listeners, host); got != want {
			t.Errorf("%s: %q, want %q", host, got, want)
		}
	}
}

// fillRouteCerts badges only the https routes whose host answered; the others keep nothing.
func TestFillRouteCerts(t *testing.T) {
	routes := []kubeRoute{
		{URL: "https://a.home.example", tlsSecret: "web/a"},
		{URL: "https://b.home.example:8443/app", tlsSecret: "web/b"},
		{URL: "http://c.home.example"},
	}
	certs := map[certTarget]routeCert{
		{"a.home.example", 443}: {NotAfter: routeCertNow.Add(3 * 24 * time.Hour).Unix(), DNSNames: []string{"a.home.example"}},
	}

	fillRouteCerts(routes, certs, map[string]bool{"web/a": true}, routeCertNow)

	if r := routes[0]; r.CertIssue != routeCertExpiring || r.CertSecret != "web/a" || !r.CertManaged || r.CertNotAfter == 0 {
		t.Errorf("a %+v", r)
	}

	if routes[1].CertSecret != "" || routes[1].CertIssue != "" || routes[2].CertSecret != "" {
		t.Errorf("unread or plain routes badged: %+v", routes[1:])
	}

	if tgt, ok := routeCertTarget(routes[1]); !ok || tgt.port != 8443 {
		t.Errorf("target %+v %v", tgt, ok)
	}

	out, err := json.Marshal(routes[2])
	if err != nil || strings.Contains(string(out), "cert") || strings.Contains(string(out), "tls") {
		t.Errorf("a plain route's JSON %s %v", out, err)
	}
}

// An IP address carries no SNI: its certificate is the proxy's default, not the route's.
func TestRouteCertTargetSkipsAddresses(t *testing.T) {
	for _, u := range []string{"https://10.0.0.5", "https://[fd00::5]:8443", "http://a.home.example", "https://*.home.example"} {
		if _, ok := routeCertTarget(kubeRoute{URL: u}); ok {
			t.Errorf("%s is probed", u)
		}
	}
}
