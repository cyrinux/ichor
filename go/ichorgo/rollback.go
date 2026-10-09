package ichorgo

import (
	"context"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// Rollback boots node back into the Talos it ran before its last upgrade, like `talosctl
// rollback` (os:admin): the node reboots at once, without draining. For an upgrade that
// boots but misbehaves; one that does not boot is rolled back by Talos itself.
func Rollback(configYAML, contextName, node string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "talos-rollback", Node: node})

	return nodeAction(configYAML, contextName, node, callTimeout, func(ctx context.Context, c *client.Client) error {
		return c.Rollback(ctx)
	})
}
