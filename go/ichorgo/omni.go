package ichorgo

import (
	"strings"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// An Omni talosconfig context has no client certificate: its endpoint is the Omni instance
// (https://…), auth.siderov1 says requests are signed with the user's key, and cluster names
// the cluster Omni proxies them to.

// authOmni is contextSummary.Auth for an Omni context.
const authOmni = "omni"

func isOmni(ctx *clientconfig.Context) bool {
	return ctx != nil && ctx.Auth.SideroV1 != nil
}

// omniClusterKey identifies the cluster an Omni context reaches, as its CA does for a
// direct one: the Omni instance and the cluster name.
func omniClusterKey(ctx *clientconfig.Context) string {
	instance := ""
	if len(ctx.Endpoints) > 0 {
		instance = strings.TrimSuffix(strings.TrimSpace(ctx.Endpoints[0]), "/")
	}

	return instance + "\x00" + ctx.Cluster
}

// clusterKey is what tells clusters apart: the CA, or the Omni instance and cluster.
func clusterKey(ctx *clientconfig.Context) string {
	if isOmni(ctx) {
		return omniClusterKey(ctx)
	}

	return ctx.CA
}
