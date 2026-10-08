package ichorgo

import (
	"context"
	"fmt"

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

	omni := false

	kubeconfig, err := withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		omni = s.signing != nil

		return fetchKubeconfig(ctx, s)
	})
	if err != nil || omni {
		// Omni's kubeconfig names its kube proxy: an address set for the cluster does not apply.
		return kubeconfig, err
	}

	return withKubeServer(kubeconfig, kubeServer)
}

// fetchKubeconfig asks a control-plane node for an admin kubeconfig. Talos signs a new
// client certificate on every call.
func fetchKubeconfig(ctx context.Context, s *session) (string, error) {
	// Through Omni: Omni's kubeconfig of the cluster, which signs in with kubelogin.
	if s.signing != nil {
		cc, err := omniDial(s.context, &omniSigning{method: omniMethodName(s.context), signer: s.signing.current()})
		if err != nil {
			return "", fmt.Errorf("connect to Omni: %w", err)
		}

		defer cc.Close() //nolint:errcheck

		data, err := omniKubeconfig(ctx, cc, s.context.Cluster)
		if err != nil {
			return "", omniAPIError(err)
		}

		return string(data), nil
	}

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
