package ichorgo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"google.golang.org/grpc"
)

// A talosconfig from Omni (omnictl talosconfig, or the Omni UI) has no certificate: its one
// endpoint is the Omni instance, which proxies the Talos API to the machines of the context's
// cluster, and every call is signed with a PGP key (auth.siderov1). That key is either the
// key of a service account (pasted once, long-lived) or one the user confirms in the
// browser for their Omni account (a few hours). Both live in the AuthStore, never on disk.

// isOmni reports whether ctx is an Omni context.
func isOmni(ctx *clientconfig.Context) bool {
	return ctx != nil && ctx.Auth.SideroV1 != nil
}

// omniIdentity is the identity an Omni context signs as.
func omniIdentity(ctx *clientconfig.Context) string {
	if !isOmni(ctx) {
		return ""
	}

	return strings.TrimSpace(ctx.Auth.SideroV1.Identity)
}

// serviceAccountDomain ends the identity of an Omni service account.
const serviceAccountDomain = "@serviceaccount.omni.sidero.dev"

func isOmniServiceAccount(identity string) bool {
	return strings.HasSuffix(strings.ToLower(identity), serviceAccountDomain)
}

// omniHost is the Omni instance of ctx: the host of its endpoint (an https URL), lower
// case, with its port unless the default one. However the URL is written (with or without
// https://, :443, capitals), the same instance is the same host.
func omniHost(ctx *clientconfig.Context) string {
	if len(ctx.Endpoints) == 0 {
		return ""
	}

	endpoint := strings.TrimSpace(ctx.Endpoints[0])
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}

	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return strings.ToLower(strings.TrimSpace(ctx.Endpoints[0]))
	}

	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && port != "443" {
		return net.JoinHostPort(host, port)
	}

	return host
}

// omniClusterKey identifies an Omni cluster: its instance and its name there. Not its CA:
// a self-hosted Omni's is the instance's, shared by all its clusters.
func omniClusterKey(ctx *clientconfig.Context) string {
	return "omni\x00" + omniHost(ctx) + "\x00" + ctx.Cluster
}

func omniClusterHash(ctx *clientconfig.Context) [sha256.Size]byte {
	return sha256.Sum256([]byte(omniClusterKey(ctx)))
}

// summarizeOmniContext is summarizeContext for an Omni context: no certificate, so no roles
// (Omni's own role for the identity applies) and no expiry.
func summarizeOmniContext(name string, ctx *clientconfig.Context) (contextSummary, error) {
	// Its endpoint is the Omni instance, which no search of the local network finds.
	if len(ctx.Endpoints) == 0 {
		return contextSummary{}, errors.New("no Omni endpoint defined")
	}

	if omniIdentity(ctx) == "" {
		return contextSummary{}, errors.New("no Omni identity (auth.siderov1.identity) defined")
	}

	return contextSummary{
		Name:        name,
		Kind:        kindTalos,
		Fingerprint: contextFingerprint(name, ctx),
		ClusterID:   clusterID(ctx),
		Endpoints:   slices.Clone(ctx.Endpoints),
		Nodes:       slices.Clone(ctx.Nodes),
		Roles:       []string{},
		Omni:        true,
		Identity:    omniIdentity(ctx),
		Cluster:     ctx.Cluster,
		SignIn:      omniMethodName(ctx),
		AuthKey:     omniAuthKey(ctx),
	}, nil
}

var (
	// errOmniIssue: Omni issues the talosconfigs of its clusters, a context of it has no CA.
	errOmniIssue = errors.New("this cluster is managed by Omni: download its talosconfig from Omni")
	// errOmniNoMachines: the key works, the cluster has no machine with an address yet.
	errOmniNoMachines = errors.New("omni lists no machine in this cluster")
	// errOmniNoCluster: Omni's talosconfig of a whole account names no cluster to reach.
	errOmniNoCluster = errors.New("this Omni context names no cluster: add the account's clusters from Omni, or download a cluster's talosconfig")
	// errOmniNotProbed: an Omni context has no endpoint on the local network to look for.
	errOmniNotProbed = errors.New("an Omni cluster is reached through Omni, not probed")
	// errOmniRefusedKey: a pasted service account key Omni does not authenticate. Not a sign-in
	// request: the apps would go on in the browser, which a service account never does.
	errOmniRefusedKey = errors.New("omni refused this service account key")
)

// omniClientOptions are the client options of the Omni context stored under key, signed
// with its stored sign-in.
func omniClientOptions(key string, cfgCtx *clientconfig.Context) ([]client.OptionFunc, *omniSigning, error) {
	signer, err := loadOmniSigner(key, cfgCtx)
	if err != nil {
		return nil, nil, err
	}

	opts, signing := omniClientOptionsFor(cfgCtx, signer, func() (omniSigner, error) { return loadOmniSigner(key, cfgCtx) })

	return opts, signing, nil
}

// omniClientOptionsFor are the client options of an Omni context signing with signer: the
// context without its siderov1 auth (the library would install its own interceptor), ours
// (see omniSigning), and the cluster Omni routes the calls to. When Omni refuses the key,
// reload may give a newer one (nil: there is none).
func omniClientOptionsFor(cfgCtx *clientconfig.Context, signer omniSigner, reload func() (omniSigner, error)) ([]client.OptionFunc, *omniSigning) {
	unsigned := *cfgCtx
	unsigned.Auth.SideroV1 = nil

	signing := &omniSigning{method: omniMethodName(cfgCtx), reload: reload, signer: signer}

	opts := []client.OptionFunc{
		client.WithConfigContext(&unsigned),
		client.WithGRPCDialOptions(
			grpc.WithChainUnaryInterceptor(signing.unary()),
			grpc.WithChainStreamInterceptor(signing.stream()),
		),
	}

	if cfgCtx.Cluster != "" {
		opts = append(opts, client.WithCluster(cfgCtx.Cluster))
	}

	return opts, signing
}

// learnOmniNodes lists the cluster's members through Omni, without a node: Omni picks a
// control plane. A talosconfig from Omni lists no nodes, so this is how its session learns
// what to target.
func learnOmniNodes(ctx context.Context, c *client.Client) ([]hostEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	list, err := safe.StateListAll[*cluster.Member](ctx, c.COSI)
	if err != nil {
		// A sign-in request keeps its code first, for the apps.
		if needSignIn := (*errSignInRequired)(nil); errors.As(err, &needSignIn) {
			return nil, needSignIn
		}

		return nil, fmt.Errorf("list the cluster's machines through Omni: %s", friendlyError(err))
	}

	var nodes []hostEntry

	for m := range list.All() {
		spec := m.TypedSpec()
		if addr := preferredAddress(spec.Addresses); addr != "" {
			nodes = append(nodes, hostEntry{address: addr, hostname: spec.Hostname, role: roleName(spec.MachineType, nil)})
		}
	}

	if len(nodes) == 0 {
		return nil, errOmniNoMachines
	}

	return nodes, nil
}

// readyRetry is how long a failed node lookup answers every caller, rather than each one
// waiting for its own.
const readyRetry = 5 * time.Second

// ready completes a session before its first use: an Omni context without nodes learns
// them once. Readers of s.context all come through here, so the update is never raced.
func (s *session) ready(ctx context.Context) error {
	s.readyMu.Lock()
	defer s.readyMu.Unlock()

	if s.isReady || !isOmni(s.context) || len(s.context.Nodes) > 0 {
		s.isReady = true

		return nil
	}

	if s.readyErr != nil && time.Since(s.readyErrAt) < readyRetry {
		return s.readyErr
	}

	// Omni routes a call by its cluster: without one (and without nodes) it reaches none.
	if s.context.Cluster == "" {
		return errOmniNoCluster
	}

	nodes, err := learnOmniNodes(ctx, s.client)
	if err != nil {
		s.readyErr, s.readyErrAt = err, time.Now()

		return err
	}

	privacy.learnHosts(nodes)

	learned := *s.context
	for _, n := range nodes {
		learned.Nodes = append(learned.Nodes, n.address)
	}

	s.context = &learned
	s.isReady, s.readyErr = true, nil

	return nil
}
