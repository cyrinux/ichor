package ichorgo

import "net"

// Tests never dial the network: a route certificate probe goes to a closed local port (it
// fails at once) unless a test serves certificates with testTLSHosts.
func init() {
	routeCertDialAddr = func(certTarget) string { return net.JoinHostPort("127.0.0.1", "1") }
}
