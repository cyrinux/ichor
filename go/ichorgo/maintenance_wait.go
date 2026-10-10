package ichorgo

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// Waiting for a node to come back after a reboot: Talos answering, then its kubelet Ready.

// backObservation is one poll of a rebooting node.
type backObservation struct {
	reachable, running, kubeReady bool
}

func observeBack(ctx context.Context, c *client.Client, k *kubeClient, node, kubeNode string, since time.Time) backObservation {
	o := observeNode(ctx, c, node)
	b := backObservation{reachable: o.reachable, running: o.stage == runtime.MachineStageRunning.String() && o.ready}

	if b.running {
		ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
		defer cancel()

		b.kubeReady = kubeNodeReadySince(ctx, k, kubeNode, since)
	}

	return b
}

// kubeNodeReadySince tells whether the Kubernetes node is Ready with a heartbeat after since:
// right after a fast reboot the Node object still says Ready from before it (Kubernetes
// marks a node NotReady only after a grace period), while the restarted kubelet posts its
// status, with a fresh heartbeat, as soon as it registers.
func kubeNodeReadySince(ctx context.Context, k *kubeClient, kubeNode string, since time.Time) bool {
	var obj struct {
		Status struct {
			Conditions []struct {
				Type              string    `json:"type"`
				Status            string    `json:"status"`
				LastHeartbeatTime time.Time `json:"lastHeartbeatTime"`
			} `json:"conditions"`
		} `json:"status"`
	}

	if err := k.get(ctx, "/api/v1/nodes/"+url.PathEscape(kubeNode), &obj); err != nil {
		return false
	}

	for _, c := range obj.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True" && c.LastHeartbeatTime.After(since)
		}
	}

	return false
}

// waitBack polls until the node went down and came back running with its Kubernetes node
// Ready. Stopping pods and services takes longer than a poll, so the reboot is seen.
func waitBack(ctx context.Context, observe func(context.Context) backObservation, emit func(string), interval time.Duration) error {
	return waitBackFrom(ctx, false, observe, emit, interval)
}

// waitBackFrom is waitBack for a reboot already seen (sawDown): after an upgrade the node was
// followed through it, only its Kubernetes node remains to be Ready.
func waitBackFrom(ctx context.Context, sawDown bool, observe func(context.Context) backObservation, emit func(string), interval time.Duration) error {
	last := ""

	for {
		select {
		case <-ctx.Done():
			return errors.New("stopped while waiting for the node")
		case <-time.After(interval):
		}

		o := observe(ctx)
		if ctx.Err() != nil {
			continue
		}

		var msg string

		switch {
		case !o.reachable:
			sawDown, msg = true, "the node is rebooting"
		case !o.running:
			sawDown, msg = true, "the node is booting"
		case !sawDown:
			msg = "waiting for the node to reboot"
		case !o.kubeReady:
			msg = "the node is up, waiting for Kubernetes to report it Ready"
		default:
			emit("the node is back and Ready")

			return nil
		}

		if msg != last {
			emit(msg)
			last = msg
		}
	}
}
