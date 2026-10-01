package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
)

// DebugListener receives a debug shell's output (implemented in Kotlin/Swift).
type DebugListener interface {
	// OnStatus reports progress before the shell starts (image pull, container start).
	OnStatus(message string)
	// OnOutput delivers terminal bytes (TTY: stdout and stderr merged).
	OnOutput(data []byte)
	// OnExit is called exactly once: the exit code, or -1 with a message on failure.
	OnExit(code int, errMessage string)
}

// DebugSession is a running debug container; methods are safe from any thread.
type DebugSession struct {
	listener DebugListener
	send     chan *machineapi.DebugContainerRunRequest
	cancel   context.CancelFunc
	exitOnce sync.Once
}

const defaultDebugShell = "/bin/sh"

// The in-memory containerd instance, talosctl debug's default namespace ("inmem").
var debugContainerd = &common.ContainerdInstance{
	Driver:    common.ContainerDriver_CONTAINERD,
	Namespace: common.ContainerdNamespace_NS_SYSTEM,
}

// StartDebugShell runs image on node with a TTY, like
// `talosctl debug -n NODE IMAGE --args ARGS` (os:admin). The container gets the
// privileged profile, as talosctl does. Pull progress and output go to listener.
func StartDebugShell(configYAML, contextName, node, image, args string, cols, rows int, listener DebugListener) *DebugSession {
	// Only the target is unmasked: the terminal byte stream is not masked (escape
	// sequences may split an address anywhere), so screenshot mode does not cover the shell.
	contextName, node = unmaskTarget(configYAML, contextName, node)

	ctx, cancel := context.WithCancel(context.Background())
	d := &DebugSession{
		listener: maskedDebugListener{listener},
		send:     make(chan *machineapi.DebugContainerRunRequest, 256),
		cancel:   cancel,
	}

	go d.run(ctx, configYAML, contextName, node, strings.TrimSpace(image), debugArgs(args), cols, rows)

	return d
}

// Write sends keyboard input to the container.
func (d *DebugSession) Write(data []byte) {
	d.enqueue(&machineapi.DebugContainerRunRequest{
		Request: &machineapi.DebugContainerRunRequest_StdinData{StdinData: append([]byte(nil), data...)},
	})
}

// Resize tells the container's TTY about the terminal size.
func (d *DebugSession) Resize(cols, rows int) {
	d.enqueue(&machineapi.DebugContainerRunRequest{
		Request: &machineapi.DebugContainerRunRequest_TermResize{
			TermResize: &machineapi.DebugContainerTerminalResize{Width: int32(cols), Height: int32(rows)},
		},
	})
}

// Close ends the session; OnExit is still called (with -1 if it had not exited yet).
func (d *DebugSession) Close() {
	d.cancel()
}

func (d *DebugSession) enqueue(req *machineapi.DebugContainerRunRequest) {
	select {
	case d.send <- req:
	default: // the shell is not consuming input; dropping beats blocking the UI thread
	}
}

func (d *DebugSession) exit(code int, message string) {
	d.exitOnce.Do(func() { d.listener.OnExit(code, message) })
}

func (d *DebugSession) run(ctx context.Context, configYAML, contextName, node, image string, args []string, cols, rows int) {
	defer d.cancel()

	if err := validateDebugImage(image); err != nil {
		d.exit(-1, err.Error())

		return
	}

	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		d.exit(-1, err.Error())

		return
	}

	defer release()

	nodeCtx := client.WithNode(ctx, node)

	// Masked here: the scanner leaves an address glued to the ellipsis alone.
	d.listener.OnStatus("Pulling " + image + " on " + privacy.maskPlain(node) + "…")

	imageName, err := pullImage(nodeCtx, s.client, image)
	if err != nil {
		d.exit(-1, "pull failed: "+s.friendly(node, err))

		return
	}

	d.listener.OnStatus("Starting " + strings.Join(args, " ") + "…")

	stream, err := s.client.DebugClient.ContainerRun(nodeCtx,
		grpc.MaxCallRecvMsgSize(4*1024*1024), grpc.MaxCallSendMsgSize(4*1024*1024))
	if err != nil {
		d.exit(-1, s.friendly(node, err))

		return
	}

	err = stream.Send(&machineapi.DebugContainerRunRequest{
		Request: &machineapi.DebugContainerRunRequest_Spec{Spec: &machineapi.DebugContainerRunRequestSpec{
			Containerd: debugContainerd,
			ImageName:  imageName,
			Args:       args,
			Profile:    machineapi.DebugContainerRunRequestSpec_PROFILE_PRIVILEGED,
			Tty:        true,
		}},
	})
	if err != nil {
		d.exit(-1, s.friendly(node, err))

		return
	}

	if cols > 0 && rows > 0 {
		d.Resize(cols, rows)
	}

	go d.forwardInput(ctx, stream)

	d.receive(ctx, stream)
}

func (d *DebugSession) forwardInput(ctx context.Context, stream machineapi.DebugService_ContainerRunClient) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-d.send:
			if err := stream.Send(req); err != nil {
				return
			}
		}
	}
}

func (d *DebugSession) receive(ctx context.Context, stream machineapi.DebugService_ContainerRunClient) {
	for {
		msg, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			d.exit(-1, "session ended")

			return
		case err != nil:
			if ctx.Err() != nil {
				d.exit(-1, "closed")
			} else {
				d.exit(-1, friendlyError(err))
			}

			return
		}

		switch resp := msg.GetResp().(type) {
		case *machineapi.DebugContainerRunResponse_StdoutData:
			if len(resp.StdoutData) > 0 {
				d.listener.OnOutput(resp.StdoutData)
			}
		case *machineapi.DebugContainerRunResponse_ExitCode:
			d.exit(int(resp.ExitCode), "")

			return
		}
	}
}

// pullImage pulls ref on the node (Talos skips layers it already has) and returns the
// resolved image name to run.
func pullImage(ctx context.Context, c *client.Client, ref string) (string, error) {
	stream, err := c.ImageClient.Pull(ctx, &machineapi.ImageServicePullRequest{Containerd: debugContainerd, ImageRef: ref})
	if err != nil {
		return "", err
	}

	name := ""

	for {
		msg, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			if name == "" {
				return "", fmt.Errorf("no image name returned for %s", ref)
			}

			return name, nil
		case err != nil:
			return "", err
		}

		if n := msg.GetName(); n != "" {
			name = n
		}
	}
}

// maskedDebugListener masks the status and exit messages, which may name the real node.
// The terminal bytes pass through unmasked (see StartDebugShell).
type maskedDebugListener struct{ DebugListener }

func (l maskedDebugListener) OnStatus(message string) {
	l.DebugListener.OnStatus(privacy.maskPlain(message))
}

func (l maskedDebugListener) OnExit(code int, errMessage string) {
	l.DebugListener.OnExit(code, privacy.maskPlain(errMessage))
}

func debugArgs(args string) []string {
	if fields := strings.Fields(args); len(fields) > 0 {
		return fields
	}

	return []string{defaultDebugShell}
}

func validateDebugImage(image string) error {
	image = strings.TrimSpace(image)

	switch {
	case image == "":
		return errors.New("no image given")
	case strings.ContainsAny(image, " \t\n"):
		return fmt.Errorf("invalid image reference %q", image)
	case strings.HasPrefix(image, "-"):
		return fmt.Errorf("invalid image reference %q", image)
	}

	return nil
}
