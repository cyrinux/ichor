package ichorgo

import (
	"context"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// Defragmenting a large database can take a while; the member is busy meanwhile.
const defragTimeout = 5 * time.Minute

// EtcdDefragment defragments node's etcd member, like `talosctl -n NODE etcd defrag`
// (os:operator or os:admin). Talos advises one member at a time: callers sequence them.
func EtcdDefragment(configYAML, contextName, node string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return nodeAction(configYAML, contextName, node, defragTimeout, func(ctx context.Context, c *client.Client) error {
		_, err := c.EtcdDefragment(ctx)

		return err
	})
}
