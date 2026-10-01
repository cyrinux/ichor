package talosmobile

import (
	"context"
	"errors"
	"sync"

	"github.com/siderolabs/talos/pkg/machinery/api/time"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

type nodeTime struct {
	Node       string `json:"node"`
	Server     string `json:"server"`     // NTP server the node compared against
	LocalTime  int64  `json:"localTime"`  // unix ms, node clock
	RemoteTime int64  `json:"remoteTime"` // unix ms, NTP server clock
	OffsetMs   int64  `json:"offsetMs"`   // remoteTime - localTime
	Error      string `json:"error,omitempty"`
}

type clusterTime struct {
	Context string     `json:"context"`
	Nodes   []nodeTime `json:"nodes"`
}

// NodeTime compares node's clock with its NTP server, like `talosctl time` (os:reader).
func NodeTime(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		resp, err := s.client.Time(withNode(ctx, node))
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		return toJSON(mapNodeTime(node, first(resp.GetMessages())))
	})
}

// ClusterTime runs NodeTime on every node of the context in parallel (os:reader), so the
// UI can flag clock drift. Failing nodes are reported per node.
func ClusterTime(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		result := clusterTime{Context: name, Nodes: make([]nodeTime, len(nodes))}

		var wg sync.WaitGroup

		for i, node := range nodes {
			wg.Go(func() {
				result.Nodes[i] = probeTime(ctx, s.client, node)
			})
		}

		wg.Wait()

		return toJSON(result)
	})
}

func probeTime(ctx context.Context, c *client.Client, node string) nodeTime {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	resp, err := c.Time(withNode(ctx, node))
	if err != nil {
		return nodeTime{Node: node, Error: friendlyError(err)}
	}

	return mapNodeTime(node, first(resp.GetMessages()))
}

func mapNodeTime(node string, t *time.Time) nodeTime {
	if t == nil || t.GetLocaltime() == nil || t.GetRemotetime() == nil {
		return nodeTime{Node: node, Server: t.GetServer(), Error: "the node returned no time"}
	}

	local := t.GetLocaltime().AsTime().UnixMilli()
	remote := t.GetRemotetime().AsTime().UnixMilli()

	return nodeTime{
		Node:       node,
		Server:     t.GetServer(),
		LocalTime:  local,
		RemoteTime: remote,
		OffsetMs:   remote - local,
	}
}
