package ichorgo

import (
	"context"
	"errors"
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

// kubeNodeReadySince tells whether the Kubernetes node is Ready with a kubelet heartbeat after
// since (kubeNodeObject.readySince); an unreadable node is not.
func kubeNodeReadySince(ctx context.Context, k *kubeClient, kubeNode string, since time.Time) bool {
	obj, err := readKubeNodeObject(ctx, k, kubeNode)

	return err == nil && obj.readySince(since)
}

// waitBack polls until the node went down and came back running with its Kubernetes node
// Ready. Stopping pods and services takes longer than a poll, so the reboot is seen.
func waitBack(ctx context.Context, observe func(context.Context) backObservation, emit func(string), interval time.Duration) error {
	sawDown, last := false, ""

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
