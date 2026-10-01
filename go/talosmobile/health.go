package talosmobile

import (
	"context"
	"errors"
	"io"
	"sync"
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
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)

	go func() {
		defer cancel()

		listener.OnDone(runHealth(ctx, configYAML, contextName, listener))
	}()

	return &HealthRun{cancel: cancel}
}

func runHealth(ctx context.Context, configYAML, contextName string, listener HealthListener) string {
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

	stream, err := s.client.ClusterHealthCheck(client.WithNode(ctx, runner), healthWaitTimeout, info)
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

// classifyNodes splits context nodes into control plane and workers. Nodes whose role
// cannot be read are treated as workers so the health check still waits for them.
func classifyNodes(ctx context.Context, c *client.Client, nodes []string) *clusterapi.ClusterInfo {
	isCP := make([]bool, len(nodes))

	var wg sync.WaitGroup

	for i, node := range nodes {
		wg.Go(func() {
			mt, err := safe.StateGetByID[*config.MachineType](client.WithNode(ctx, node), c.COSI, config.MachineTypeID)
			isCP[i] = err == nil && mt.MachineType().IsControlPlane()
		})
	}

	wg.Wait()

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
