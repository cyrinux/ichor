package ichorgo

import (
	"context"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// Kubeconfig returns an admin kubeconfig for the cluster (like `talosctl kubeconfig`),
// fetched from the first reachable control-plane node. Requires the os:admin role.
// The result is a credential: callers must write it only where the user asked. With a
// kubeServer (see KubePods), the kubeconfig points at it.
func Kubeconfig(configYAML, contextName, kubeServer string) (out string, err error) {
	// The result is a credential the user saves: only the error is masked.
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)

	kubeconfig, err := withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		return fetchKubeconfig(ctx, s)
	})
	if err != nil {
		return "", err
	}

	return withKubeServer(kubeconfig, kubeServer)
}

// fetchKubeconfig asks a control-plane node for an admin kubeconfig. Talos signs a new
// client certificate on every call.
func fetchKubeconfig(ctx context.Context, s *session) (string, error) {
	cps, err := s.controlPlanes(ctx)
	if err != nil {
		return "", err
	}

	data, err := s.client.Kubeconfig(client.WithNode(ctx, cps[0]))
	if err != nil {
		return "", friendlyErr(err)
	}

	return string(data), nil
}
