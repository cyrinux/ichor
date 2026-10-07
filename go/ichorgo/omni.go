package ichorgo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/go-api-signature/pkg/client/interceptor"
	"github.com/siderolabs/go-api-signature/pkg/message"
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

// omniClusterKey identifies an Omni cluster: its instance and its name there (Omni contexts
// have no CA to tell clusters apart).
func omniClusterKey(ctx *clientconfig.Context) string {
	endpoint := ""
	if len(ctx.Endpoints) > 0 {
		endpoint = strings.ToLower(endpointHost(ctx.Endpoints[0]))
	}

	return "omni\x00" + endpoint + "\x00" + ctx.Cluster
}

func omniClusterHash(ctx *clientconfig.Context) [sha256.Size]byte {
	return sha256.Sum256([]byte(omniClusterKey(ctx)))
}

// summarizeOmniContext is summarizeContext for an Omni context: no certificate, so no roles
// (Omni's own role for the identity applies) and no expiry.
func summarizeOmniContext(name string, ctx *clientconfig.Context) (contextSummary, error) {
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
	}, nil
}

// errOmniIssue: Omni issues the talosconfigs of its clusters, a context of it has no CA.
var errOmniIssue = errors.New("this cluster is managed by Omni: download its talosconfig from Omni")

// omniClientOptions are the client options of the Omni context stored under key, signed
// with its stored sign-in.
func omniClientOptions(key string, cfgCtx *clientconfig.Context) ([]client.OptionFunc, error) {
	signer, err := loadOmniSigner(key, cfgCtx)
	if err != nil {
		return nil, err
	}

	return omniClientOptionsFor(cfgCtx, signer, func() (omniSigner, error) { return loadOmniSigner(key, cfgCtx) })
}

// omniClientOptionsFor are the client options of an Omni context signing with signer: the
// context without its siderov1 auth (the library's interceptor reads keys from disk and
// opens a browser), our own signing interceptor, and the cluster Omni routes the calls to.
// When Omni refuses the key, reload may give a newer one (nil: there is none).
func omniClientOptionsFor(cfgCtx *clientconfig.Context, signer omniSigner, reload func() (omniSigner, error)) ([]client.OptionFunc, error) {
	unsigned := *cfgCtx
	unsigned.Auth.SideroV1 = nil

	refused := talosSignInRequired(omniMethodName(cfgCtx), "Omni refused the key")

	sign := interceptor.New(interceptor.Options{
		InfoWriter: io.Discard,
		Identity:   signer.identity,
		ClientName: "ichor",
		GetUserKeyFunc: func(context.Context, *grpc.ClientConn, *interceptor.Options) (message.Signer, error) {
			return signer.key, nil
		},
		// Omni refused the key (expired, revoked): a newer one may have been stored since,
		// else the user has to sign in again.
		RenewUserKeyFunc: func(context.Context, *grpc.ClientConn, *interceptor.Options) (message.Signer, error) {
			if reload == nil {
				return nil, refused
			}

			fresh, err := reload()
			if err != nil {
				return nil, err
			}

			if fresh.key.Fingerprint() == signer.key.Fingerprint() {
				return nil, refused
			}

			return fresh.key, nil
		},
	})

	opts := []client.OptionFunc{
		client.WithConfigContext(&unsigned),
		client.WithGRPCDialOptions(
			grpc.WithChainUnaryInterceptor(sign.Unary()),
			grpc.WithChainStreamInterceptor(sign.Stream()),
		),
	}

	if cfgCtx.Cluster != "" {
		opts = append(opts, client.WithCluster(cfgCtx.Cluster))
	}

	return opts, nil
}

// learnOmniNodes lists the cluster's members through Omni, without a node: Omni picks a
// control plane. A talosconfig from Omni lists no nodes, so this is how its session learns
// what to target.
func learnOmniNodes(ctx context.Context, c *client.Client) ([]hostEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	list, err := safe.StateListAll[*cluster.Member](ctx, c.COSI)
	if err != nil {
		return nil, fmt.Errorf("list the cluster's machines through Omni: %w", err)
	}

	var nodes []hostEntry

	for m := range list.All() {
		spec := m.TypedSpec()
		if addr := preferredAddress(spec.Addresses); addr != "" {
			nodes = append(nodes, hostEntry{address: addr, hostname: spec.Hostname, role: roleName(spec.MachineType, nil)})
		}
	}

	if len(nodes) == 0 {
		return nil, errors.New("omni lists no machine in this cluster")
	}

	return nodes, nil
}

// ready completes a session before its first use: an Omni context without nodes learns
// them once. Readers of s.context all come through here, so the update is never raced.
func (s *session) ready(ctx context.Context) error {
	s.readyMu.Lock()
	defer s.readyMu.Unlock()

	if s.isReady || !isOmni(s.context) || len(s.context.Nodes) > 0 {
		s.isReady = true

		return nil
	}

	nodes, err := learnOmniNodes(ctx, s.client)
	if err != nil {
		return err
	}

	privacy.learnHosts(nodes)

	learned := *s.context
	for _, n := range nodes {
		learned.Nodes = append(learned.Nodes, n.address)
	}

	s.context = &learned
	s.isReady = true

	return nil
}
