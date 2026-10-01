package talosmobile

import (
	"context"
	"errors"
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

	_, err = withSession(configYAML, contextName, defragTimeout, func(ctx context.Context, s *session) (struct{}, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return struct{}{}, err
		}

		if _, err := s.client.EtcdDefragment(client.WithNode(ctx, node)); err != nil {
			return struct{}{}, errors.New(s.friendly(node, err))
		}

		return struct{}{}, nil
	})

	return err
}
