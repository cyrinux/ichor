package talosmobile

import (
	"context"
	"errors"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// Kubeconfig returns an admin kubeconfig for the cluster (like `talosctl kubeconfig`),
// fetched from the first reachable control-plane node. Requires the os:admin role.
// The result is a credential: callers must write it only where the user asked.
func Kubeconfig(configYAML, contextName string) (out string, err error) {
	// The result is a credential the user saves: only the error is masked.
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		cps := classifyNodes(ctx, s.client, targetNodes(s.context)).GetControlPlaneNodes()
		if len(cps) == 0 {
			return "", errors.New("no reachable control-plane node found in this context")
		}

		data, err := s.client.Kubeconfig(client.WithNode(ctx, cps[0]))
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		return string(data), nil
	})
}
