package ichorgo

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const healthWaitTimeout = time.Minute

// HealthListener receives cluster health check progress (implemented in Kotlin).
type HealthListener interface {
	OnProgress(node, message string)
	// OnDone is called exactly once; errMessage is empty on success.
	OnDone(errMessage string)
}

// HealthRun is a handle on a running health check.
type HealthRun struct {
	cancel context.CancelFunc
}

// Cancel stops the health check; OnDone is still called.
func (h *HealthRun) Cancel() {
	h.cancel()
}

// StartClusterHealth runs the server-side cluster health check (like `talosctl health`)
// on the first control-plane node and streams progress to listener.
func StartClusterHealth(configYAML, contextName string, listener HealthListener) *HealthRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedHealthListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		listener.OnDone(runHealth(ctx, configYAML, contextName, listener))
	}()

	return &HealthRun{cancel: cancel}
}

func runHealth(ctx context.Context, configYAML, contextName string, listener HealthListener) string {
	if isDemoContext(configYAML, contextName) {
		for _, message := range []string{"Demo: all nodes are ready", "Demo: etcd members are healthy", "Demo: Kubernetes control plane is ready"} {
			if ctx.Err() != nil {
				return ""
			}
			listener.OnProgress("demo-cp-1", message)
		}
		return ""
	}
	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		return err.Error()
	}

	defer release()

	info := classifyNodes(ctx, s.client, targetNodes(s.context))
	if len(info.GetControlPlaneNodes()) == 0 {
		return "no reachable control-plane node found in this context"
	}

	runner := info.GetControlPlaneNodes()[0]

	// An empty ClusterInfo makes Talos check the discovered cluster members, like
	// `talosctl health` without flags. Passing the talosconfig's node list instead would make
	// it wait for nodes that are not (or no longer) members, and report the cluster unhealthy.
	stream, err := s.client.ClusterHealthCheck(client.WithNode(ctx, runner), healthWaitTimeout, &clusterapi.ClusterInfo{})
	if err != nil {
		return friendlyError(err)
	}

	var last string

	for {
		msg, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			return ""
		case err != nil:
			return healthFailure(last, err)
		}

		if md := msg.GetMetadata(); md.GetError() != "" {
			return md.GetError()
		}

		last = msg.GetMessage()
		listener.OnProgress(msg.GetMetadata().GetHostname(), last)
	}
}

// healthFailure explains a failed check: the last progress line names the failing
// condition, which beats a bare "deadline exceeded" once the server gives up waiting.
func healthFailure(lastProgress string, err error) string {
	if status.Code(err) == codes.PermissionDenied {
		// The server runs the check with the caller's roles and needs a Kubernetes admin kubeconfig.
		return "the cluster health check needs an os:admin talosconfig: " + friendlyError(err)
	}

	if lastProgress == "" {
		return friendlyError(err)
	}

	return "not healthy after " + healthWaitTimeout.String() + ": " + lastProgress
}

// classifyNodes splits context nodes into control plane and workers (unreadable roles count
// as workers). Used to pick a control-plane node to run checks on and to find etcd members.
func classifyNodes(ctx context.Context, c *client.Client, nodes []string) *clusterapi.ClusterInfo {
	isCP := make([]bool, len(nodes))

	forEachNode(nodes, func(i int, node string) {
		// Bound each lookup so one unresponsive node cannot eat the whole call budget; per node,
		// so that nodes waiting for a fan-out slot keep their full time.
		ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
		defer cancel()

		mt, err := safe.StateGetByID[*config.MachineType](client.WithNode(ctx, node), c.COSI, config.MachineTypeID)
		isCP[i] = err == nil && mt.MachineType().IsControlPlane()
	})

	info := &clusterapi.ClusterInfo{}

	for i, node := range nodes {
		if isCP[i] {
			info.ControlPlaneNodes = append(info.ControlPlaneNodes, node)
		} else {
			info.WorkerNodes = append(info.WorkerNodes, node)
		}
	}

	return info
}
