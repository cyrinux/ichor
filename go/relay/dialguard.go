package relay

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// errAddressRefused is a push delivery the dial check stopped: the endpoint resolved to an
// address the relay must not reach (see addrPolicy).
var errAddressRefused = errors.New("push endpoint address refused")

// cgnat is the shared address space (RFC 6598), private like RFC 1918 for this check.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// addrPolicy decides, at dial time, which addresses a push may reach. Whoever holds the pairing
// token chooses the endpoint, so the relay must not become a client of what only the cluster
// can reach: unspecified, link-local (the cloud metadata service at 169.254.169.254) and
// multicast addresses are always refused; loopback and private ranges only with allowPrivate
// (RELAY_ALLOW_PRIVATE, or RELAY_ALLOW_HTTP for an in-cluster ntfy over a ClusterIP). Checked
// on the address dialled, after DNS, so a name pointing inside is refused too.
type addrPolicy struct {
	allowPrivate bool
}

func (p addrPolicy) check(ip netip.Addr) error {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid(), ip.IsUnspecified(), ip.IsLinkLocalUnicast(), ip.IsMulticast():
		return errAddressRefused
	case ip.IsLoopback(), ip.IsPrivate(), cgnat.Contains(ip):
		if !p.allowPrivate {
			return errAddressRefused
		}
	}

	return nil
}

// control is a net.Dialer Control: it runs on each address the dialer is about to connect to.
func (p addrPolicy) control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errAddressRefused
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return errAddressRefused
	}

	return p.check(ip)
}

// newPushClient is the client the pushes go out with: newHTTPClient's, its dials checked by policy.
func newPushClient(policy addrPolicy) *http.Client {
	client := newHTTPClient(nil, http.ProxyFromEnvironment, 0)
	transport := client.Transport.(*http.Transport)
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: policy.control}).DialContext

	return client
}
