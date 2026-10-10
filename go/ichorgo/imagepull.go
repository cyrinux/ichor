package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

// Image pre-pull: pull one image on several nodes before a maintenance window, so the
// upgrade's reboot (or a rollout) does not wait for the download, like
// `talosctl image pull -n NODES IMAGE`.

const (
	// imagePullParallel nodes pull at a time: a pull is heavy on the uplink.
	imagePullParallel = 3
	// imagePullNodeTimeout bounds one node's pull; slow links take minutes.
	imagePullNodeTimeout = 10 * time.Minute
)

// Image pull namespaces: "system" holds the Talos installer and system images, "cri" the
// Kubernetes (k8s.io) images. An image pulled into the wrong one is of no use.
const (
	imagePullSystem = "system"
	imagePullCRI    = "cri"
)

// Node states of an image pull.
const (
	pullPending = "pending"
	pullPulling = "pulling"
	pullDone    = "done"
	pullFailed  = "failed"
)

// ImagePullListener follows an image pull (implemented in Kotlin/Swift).
type ImagePullListener interface {
	// OnProgress gets {"nodes":[{"node","hostname","state","error"}],"done","total","at"}
	// (state: pending, pulling, done or failed; at: unix ms) on every change.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty when every node pulled the image,
	// else the first failure.
	OnDone(errMessage string)
}

// ImagePullRun is a handle on a running image pull.
type ImagePullRun struct {
	cancel context.CancelFunc
}

// Cancel stops the pulls in flight; the nodes not started stay pending.
func (r *ImagePullRun) Cancel() { r.cancel() }

type imagePullNode struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
}

type imagePullProgress struct {
	Nodes []imagePullNode `json:"nodes"`
	Done  int             `json:"done"`
	Total int             `json:"total"`
	At    int64           `json:"at"`
}

// StartImagePull pulls image on nodesCSV (comma-separated; empty: every node of the
// context), at most three nodes at a time (os:admin). namespace is "system" (the Talos
// installer and system images) or "cri" (Kubernetes images). A node that fails does not stop
// the others.
func StartImagePull(configYAML, contextName, nodesCSV, image, namespace string, listener ImagePullListener) *ImagePullRun {
	contextName, nodesCSV = unmaskTargets(configYAML, contextName, nodesCSV)
	image = strings.TrimSpace(image)

	listener = maskedImagePullListener{listener}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		pulled := 0

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "image-pull", Params: fmt.Sprintf("image=%s namespace=%s nodes=%d", image, namespace, pulled)}
		}, func() error {
			return runImagePull(ctx, configYAML, contextName, splitCSV(nodesCSV), image, namespace, func(p imagePullProgress) {
				pulled = p.Done
				emitJSON(p, listener.OnProgress)
			})
		})

		listener.OnDone(errText(err))
	}()

	return &ImagePullRun{cancel: cancel}
}

func imagePullNamespace(namespace string) (common.ContainerdNamespace, error) {
	switch namespace {
	case imagePullSystem:
		return common.ContainerdNamespace_NS_SYSTEM, nil
	case imagePullCRI:
		return common.ContainerdNamespace_NS_CRI, nil
	}

	return 0, fmt.Errorf("unknown image namespace %q (system or cri)", namespace)
}

func runImagePull(ctx context.Context, configYAML, contextName string, nodes []string, image, namespace string, emit func(imagePullProgress)) error {
	if isDemoContext(configYAML, contextName) {
		return errDemoUnavailable
	}

	if err := validateDebugImage(image); err != nil {
		return err
	}

	ns, err := imagePullNamespace(namespace)
	if err != nil {
		return err
	}

	s, release, err := acquireSession(configYAML, contextName)
	if err != nil {
		return err
	}

	defer release()

	if len(nodes) == 0 {
		nodes = targetNodes(s.context)
	}

	if len(nodes) == 0 {
		return errors.New("no node to pull the image on")
	}

	t := newPullTracker(nodes, targetHostnames(ctx, s.client, nodes), emit)
	t.emit()

	forEachLimit(nodes, imagePullParallel, func(i int, node string) {
		if ctx.Err() != nil {
			return
		}

		t.set(i, pullPulling, nil)

		nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), imagePullNodeTimeout)
		defer cancel()

		err := pullOnNode(nodeCtx, s.client, ns, image)
		if err != nil {
			err = s.friendlyErr(node, err)
		}

		t.set(i, pullDone, err)
	})

	if err := ctx.Err(); err != nil {
		return errors.New("image pull cancelled")
	}

	return t.firstErr()
}

// pullOnNode pulls image through MachineService.ImagePull, or the ImageService once that
// deprecated API is gone (Talos 1.13+).
func pullOnNode(ctx context.Context, c *client.Client, ns common.ContainerdNamespace, image string) error {
	//lint:ignore SA1019 the ImageService replacement is not in every supported Talos version
	err := c.ImagePull(ctx, ns, image)
	if isUnavailableAPI(err) {
		err = pullWithImageService(ctx, c, ns, image)
	}

	return err
}

func pullWithImageService(ctx context.Context, c *client.Client, ns common.ContainerdNamespace, image string) error {
	driver := common.ContainerDriver_CONTAINERD
	if ns == common.ContainerdNamespace_NS_CRI {
		driver = common.ContainerDriver_CRI
	}

	stream, err := c.ImageClient.Pull(ctx, &machineapi.ImageServicePullRequest{
		Containerd: &common.ContainerdInstance{Driver: driver, Namespace: ns},
		ImageRef:   image,
	})
	if err != nil {
		return err
	}

	for {
		if _, err := stream.Recv(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}
	}
}

// pullTracker holds the nodes' states, updated from the pulling goroutines.
type pullTracker struct {
	mu    sync.Mutex
	nodes []imagePullNode
	errs  []error
	send  func(imagePullProgress)
}

func newPullTracker(nodes, hostnames []string, send func(imagePullProgress)) *pullTracker {
	t := &pullTracker{nodes: make([]imagePullNode, len(nodes)), errs: make([]error, len(nodes)), send: send}

	for i, n := range nodes {
		t.nodes[i] = imagePullNode{Node: n, Hostname: hostnames[i], State: pullPending}
	}

	return t
}

// set moves node i to state (pullDone with an error is pullFailed) and reports it.
func (t *pullTracker) set(i int, state string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err != nil {
		state = pullFailed
		t.nodes[i].Error = err.Error()
		t.errs[i] = err
	}

	t.nodes[i].State = state
	t.sendLocked()
}

func (t *pullTracker) emit() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sendLocked()
}

// sendLocked reports the states; under t.mu, so the listener gets them in order.
func (t *pullTracker) sendLocked() {
	p := imagePullProgress{Nodes: slices.Clone(t.nodes), Total: len(t.nodes), At: time.Now().UnixMilli()}

	for _, n := range t.nodes {
		if n.State == pullDone {
			p.Done++
		}
	}

	t.send(p)
}

// firstErr is the first failed node's error, in node order, naming the node.
func (t *pullTracker) firstErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	failed := 0

	var first error

	for i, err := range t.errs {
		if err == nil {
			continue
		}

		failed++

		if first == nil {
			name := t.nodes[i].Hostname
			if name == "" {
				name = t.nodes[i].Node
			}

			first = fmt.Errorf("%s: %w", name, err)
		}
	}

	if failed > 1 {
		return fmt.Errorf("%w (and %d more nodes failed)", first, failed-1)
	}

	return first
}
