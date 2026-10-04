package ichorgo

import (
	"context"
	"sync"
	"time"
)

// agentFollower is the stream of one node's agent pod.
type agentFollower struct {
	pod    string
	cancel context.CancelFunc
	done   chan struct{}
}

func (f *agentFollower) finished() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

// superviseAgents keeps one stream per node's agent: it starts the missing ones, restarts
// one whose pod was replaced or whose stream ended, and stops those of nodes gone, listing
// the agents again every hubbleAgentRefresh. It returns once ctx ends and every stream stopped.
func superviseAgents(ctx context.Context, k *kubeClient, status ciliumStatus, filter hubbleFilter, agg *hubbleAgg) {
	running := map[string]*agentFollower{}

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		current := map[string]bool{}

		for _, agent := range status.Agents {
			current[agent.Node] = true

			if f, ok := running[agent.Node]; ok {
				if f.pod == agent.Pod && !f.finished() {
					continue
				}

				f.cancel()
			}

			child, cancel := context.WithCancel(ctx)
			f := &agentFollower{pod: agent.Pod, cancel: cancel, done: make(chan struct{})}
			running[agent.Node] = f

			wg.Go(func() {
				defer close(f.done)
				defer cancel()

				followAgent(child, k, status.Namespace, agent, filter, agg)
			})
		}

		for node, f := range running {
			if !current[node] {
				f.cancel()
				delete(running, node)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(hubbleAgentRefresh):
		}

		readCtx, cancel := context.WithTimeout(ctx, callTimeout)
		fresh, err := readCiliumStatus(readCtx, k)
		cancel()

		if err == nil && fresh.Installed {
			status = fresh
			agg.syncAgents(fresh.Agents)
		}
	}
}

// followAgent follows one agent until its stream ends or ctx does. A stream started again
// picks up after the last flow the node sent, not with the backfill a second time.
func followAgent(ctx context.Context, k *kubeClient, namespace string, agent ciliumAgent, filter hubbleFilter, agg *hubbleAgg) {
	agg.setNode(agent.Node, hubbleNodeConnecting, "")

	argv := hubbleCommand(filter, agg.lastFlow(agent.Node))

	// Live once the command runs: a filter may let no flow through for a long time.
	onStart := func() { agg.setNode(agent.Node, hubbleNodeLive, "") }
	onLine := func(line []byte) {
		if parsed, ok := parseHubbleLine(line); ok {
			agg.add(agent.Node, parsed)
		}
	}

	err := k.execLines(ctx, namespace, agent.Pod, ciliumAgentContainer, argv, onStart, onLine)

	if ctx.Err() != nil {
		return
	}

	msg := "the flow stream ended"
	if err != nil {
		msg = kubeError(err).Error()
	}

	agg.setNode(agent.Node, hubbleNodeError, msg)
}
