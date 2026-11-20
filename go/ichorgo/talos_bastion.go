package ichorgo

import (
	"context"
	"errors"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/internal/httpx"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"google.golang.org/grpc"
)

// A cluster added from a talosconfig uses the same SSH bastion setting as a kubeconfig one
// (kube_bastion.go), kept under its context's fingerprint, with one more choice, its scope:
//
//   - "all" (the default): the Talos API (gRPC to each endpoint, port 50000) and the
//     Kubernetes API both go through the bastion.
//   - "kube": the Talos endpoints are reachable directly, only the Kubernetes API server is
//     not; Talos calls stay direct.
//
// The session's gRPC connection owns one SSH connection (closed with the session); the
// Kubernetes client opens its own (kube_facade.go): two SSH connections to the bastion at most
// per cluster, so neither depends on the other's lifetime. A changed host key stops both.
//
// The one-off Talos clients (endpoint scan, join inspection, CA rotation, new cluster) stay
// direct.

// The scopes of a Talos context's bastion.
const (
	bastionScopeAll  = "all"
	bastionScopeKube = "kube"
)

// bastionScopeField is the auth store secret (and KubeSetBastion field) holding the scope.
const bastionScopeField = "scope"

// talosBastionKey is the bastion key a (non-Omni) context's summary carries: "" on the demo,
// which cannot have one. An Omni context has none either (its Kubernetes goes through Omni).
func talosBastionKey(fingerprint string, ctx *clientconfig.Context) string {
	if isOmni(ctx) || slices.Contains(ctx.Endpoints, demoEndpoint) {
		return ""
	}

	return kubeBastionKey(fingerprint)
}

// talosBastionContext is bastionContext for a talosconfig context: its key is the context's
// fingerprint's, and server the first endpoint's Talos API address (what scope all checks).
func talosBastionContext(storedYAML, name string) (bastionTarget, error) {
	name, ctx, err := resolveContext(storedYAML, name)
	if err != nil {
		return bastionTarget{}, err
	}

	switch {
	case slices.Contains(ctx.Endpoints, demoEndpoint):
		return bastionTarget{unavailable: bastionUnavailableDemo}, nil
	case isOmni(ctx):
		return bastionTarget{unavailable: bastionUnavailableOmni}, nil
	}

	out := bastionTarget{key: kubeBastionKey(contextFingerprint(name, ctx)), talos: true}
	if len(ctx.Endpoints) > 0 {
		out.server = talosEndpointAddress(ctx.Endpoints[0])
	}

	return out, nil
}

// talosEndpointAddress is endpoint (host, host:port or a URL) as the address the Talos client
// dials: port 50000 unless it names one.
func talosEndpointAddress(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)

	if u, err := url.Parse(endpoint); err == nil && u.Scheme != "" && u.Host != "" {
		endpoint = u.Host
	}

	if _, _, err := net.SplitHostPort(endpoint); err == nil {
		return endpoint
	}

	return net.JoinHostPort(strings.Trim(endpoint, "[]"), strconv.Itoa(talosAPIPort))
}

// parseBastionScope reads the scope a user chose ("" keeps stored, or all for a new one).
func parseBastionScope(scope, stored string) (string, error) {
	switch strings.TrimSpace(scope) {
	case "":
		if stored != "" {
			return stored, nil
		}

		return bastionScopeAll, nil
	case bastionScopeAll:
		return bastionScopeAll, nil
	case bastionScopeKube:
		return bastionScopeKube, nil
	default:
		return "", errors.New(`the SSH bastion scope is "all" or "kube"`)
	}
}

// loadBastionScope is the scope stored under key: all when none was ever chosen.
func loadBastionScope(key string) string {
	if scope := kubeAuth.load(key).secret(bastionScopeField); scope != "" {
		return scope
	}

	return bastionScopeAll
}

// checkTalosBastion is checkBastion for a talosconfig context: with scope all a channel to the
// first endpoint's Talos API, with scope kube only the sign-in (the API server is not known
// before the kubeconfig is fetched).
func checkTalosBastion(c httpx.BastionConfig, scope, endpoint string) error {
	if scope == bastionScopeAll {
		if endpoint == "" {
			return errors.New("the talosconfig names no endpoint")
		}

		return checkBastion(c, endpoint)
	}

	d, err := httpx.NewBastionDialer(c, kubeProbeTimeout)
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck

	return d.Connect(context.Background())
}

// talosSessionBastion is the hop of a session to the context whose bastion key this is, when
// its scope is all, with the client option that sends every gRPC dial through it; nil without.
func talosSessionBastion(key string) (*httpx.BastionDialer, client.OptionFunc, error) {
	c, ok := loadBastion(key)
	if !ok || loadBastionScope(key) != bastionScopeAll {
		return nil, nil, nil
	}

	d, err := httpx.NewBastionDialer(c, kubeProbeTimeout)
	if err != nil {
		return nil, nil, err
	}

	privacy.learnTarget(c.Host)

	// The client balances over every endpoint and redials on its own: each dial is a new
	// channel on the session's SSH connection, which the dialer reopens once if it died.
	dial := grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		return d.DialContext(ctx, "tcp", addr)
	})

	return d, client.WithGRPCDialOptions(dial), nil
}
