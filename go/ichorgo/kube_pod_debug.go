package ichorgo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// `kubectl debug -it POD --image=IMAGE --target=CONTAINER`: an ephemeral container added to a
// running pod, sharing the target container's process namespace, then attached to. The answer
// for distroless images, which have no shell for StartPodShell to run. Kubernetes keeps an
// ephemeral container in the pod spec until the pod is deleted: it cannot be removed.

const (
	podDebugDefaultImage = "busybox:1.37"
	podDebugStartTimeout = 3 * time.Minute
	podDebugNameLength   = 5
	podDebugNameAlphabet = "bcdfghjklmnpqrstvwxz2456789"
)

// podDebugPoll is how often the pod is read while the debug container starts; tests shorten it.
var podDebugPoll = time.Second

// podDebugFatalWaits are the waiting reasons a container does not recover from by itself.
var podDebugFatalWaits = []string{
	"ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ErrImageNeverPull",
	"CreateContainerError", "CreateContainerConfigError", "RunContainerError",
}

var errEphemeralUnsupported = errors.New("this cluster cannot add debug containers (ephemeral containers need Kubernetes 1.25 or later)")

// StartPodDebug adds a debug container running image ("" for busybox) to the pod, sharing
// targetContainer's processes ("" for none), then opens a terminal on it, like `kubectl debug
// -it`. The addition is recorded in the audit log; the container stays in the pod until the
// pod is deleted. Output and exit go to listener, as for StartPodShell.
func StartPodDebug(configYAML, contextName, kubeServer, namespace, pod, targetContainer, image string, cols, rows int, listener DebugListener) *DebugSession {
	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	targetContainer = privacy.reveal(strings.TrimSpace(targetContainer))
	image = cmpOr(strings.TrimSpace(image), podDebugDefaultImage)

	d, ctx := newDebugSession(listener)
	target := kubeTarget{configYAML, contextName, kubeServer}

	// A refused input is not an action: nothing to record.
	if err := validateDebugTarget(namespace, pod, targetContainer, image); err != nil {
		d.exit(-1, err.Error())
		d.cancel()

		return d
	}

	go func() {
		defer d.cancel()
		defer onPanic(func(msg string) { d.exit(-1, msg) })

		var (
			k    *kubeClient
			name string
		)

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{
				Server: kubeServer, Action: "debug-pod", Namespace: namespace, Object: "Pod/" + pod,
				Params: fmt.Sprintf("image=%s,target=%s,container=%s", image, targetContainer, name),
			}
		}, func() (err error) {
			if isDemoContext(configYAML, contextName) {
				return errDemoUnavailable
			}

			if k, _, err = kubeClients.get(target); err != nil {
				return kubeError(err)
			}

			name, err = addDebugContainer(ctx, k, namespace, pod, targetContainer, image, d.listener.OnStatus)

			return err
		})
		if err != nil {
			d.exitDialFailed(ctx, err)

			return
		}

		d.listener.OnStatus("Attached to " + name + ". If no prompt shows, press Enter.")

		ws, err := k.dialAttach(ctx, namespace, pod, name)
		if err != nil {
			d.exitDialFailed(ctx, err)

			return
		}

		d.serveTTY(ctx, ws, cols, rows, true)
	}()

	return d
}

func validateDebugTarget(namespace, pod, targetContainer, image string) error {
	if err := validateKubeName("pod", namespace, pod); err != nil {
		return err
	}

	if targetContainer != "" && !kubeNamePattern.MatchString(targetContainer) {
		return fmt.Errorf("invalid container name %q", targetContainer)
	}

	return validateDebugImage(image)
}

// debugPodObject is what the debug run reads of the pod: its containers and the ephemeral
// containers' states.
type debugPodObject struct {
	Spec struct {
		Containers []struct {
			Name string `json:"name"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		EphemeralContainerStatuses []struct {
			Name  string `json:"name"`
			State struct {
				Running *struct{} `json:"running"`
				Waiting *struct {
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"waiting"`
				Terminated *struct {
					Reason   string `json:"reason"`
					Message  string `json:"message"`
					ExitCode int    `json:"exitCode"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"ephemeralContainerStatuses"`
	} `json:"status"`
}

func (p debugPodObject) hasContainer(name string) bool {
	for _, c := range p.Spec.Containers {
		if c.Name == name {
			return true
		}
	}

	return false
}

// addDebugContainer adds the ephemeral container and waits until it runs; its name.
func addDebugContainer(ctx context.Context, k *kubeClient, namespace, pod, targetContainer, image string, status func(string)) (string, error) {
	var current debugPodObject
	if err := k.get(ctx, podPath(namespace, pod), &current); err != nil {
		return "", kubeError(err)
	}

	if targetContainer != "" && !current.hasContainer(targetContainer) {
		return "", fmt.Errorf("pod %s/%s has no container %q", namespace, pod, targetContainer)
	}

	name := debuggerName()
	container := map[string]any{
		"name": name, "image": image, "stdin": true, "tty": true,
		"imagePullPolicy": "IfNotPresent", "terminationMessagePolicy": "File",
	}

	if targetContainer != "" {
		container["targetContainerName"] = targetContainer
	}

	status("Adding debug container " + name + " (" + image + ")…")

	body := map[string]any{"spec": map[string]any{"ephemeralContainers": []any{container}}}
	if err := k.patch(ctx, podPath(namespace, pod)+"/ephemeralcontainers", "application/strategic-merge-patch+json", body, nil); err != nil {
		if isNotFound(err) {
			return "", errEphemeralUnsupported
		}

		return "", kubeMutationError(err)
	}

	return name, waitDebugContainer(ctx, k, namespace, pod, name, status)
}

// waitDebugContainer reads the pod until the container runs, fails, or podDebugStartTimeout.
func waitDebugContainer(ctx context.Context, k *kubeClient, namespace, pod, name string, status func(string)) error {
	ctx, cancel := context.WithTimeout(ctx, podDebugStartTimeout)
	defer cancel()

	lastReason := ""

	for {
		var p debugPodObject
		if err := k.get(ctx, podPath(namespace, pod), &p); err != nil {
			return kubeError(err)
		}

		for _, s := range p.Status.EphemeralContainerStatuses {
			if s.Name != name {
				continue
			}

			switch st := s.State; {
			case st.Running != nil:
				return nil
			case st.Terminated != nil:
				return fmt.Errorf("the debug container exited (%s, code %d): %s", st.Terminated.Reason, st.Terminated.ExitCode, st.Terminated.Message)
			case st.Waiting != nil && slices.Contains(podDebugFatalWaits, st.Waiting.Reason):
				return fmt.Errorf("the debug container cannot start (%s): %s", st.Waiting.Reason, st.Waiting.Message)
			case st.Waiting != nil && st.Waiting.Reason != lastReason:
				lastReason = st.Waiting.Reason
				status("Debug container " + name + ": " + lastReason + "…")
			}
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("the debug container %s did not start within %s", name, podDebugStartTimeout)
			}

			return ctx.Err()
		case <-time.After(podDebugPoll):
		}
	}
}

// debuggerName is a fresh ephemeral container name, as kubectl picks them ("debugger-x7k2p").
func debuggerName() string {
	suffix := make([]byte, podDebugNameLength)
	_, _ = rand.Read(suffix)

	for i, b := range suffix {
		suffix[i] = podDebugNameAlphabet[int(b)%len(podDebugNameAlphabet)]
	}

	return "debugger-" + string(suffix)
}
