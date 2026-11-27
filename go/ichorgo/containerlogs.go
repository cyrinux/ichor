package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// ContainerLogs returns the last tailLines lines of a container's log as JSON logTail (same
// shape as ServiceLogs), like `talosctl logs [-k] ID --tail N` (os:reader). namespace and
// containerID are the "namespace" and "id" NodeContainers returns: "k8s.io" (or "", the
// default) and "namespace/pod:container:hash" for a Kubernetes container, "taloscontainers"
// and the ContainerConfig name for a Talos container (its log spans every restart).
func ContainerLogs(configYAML, contextName, node, namespace, containerID string, tailLines int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ContainerLogs", configYAML, contextName, node, containerID, fmt.Sprint(tailLines))
	}
	containerID = privacy.unmaskText(containerID)

	namespace, driver, err := logNamespace(namespace)
	if err != nil {
		return "", err
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if strings.TrimSpace(containerID) == "" {
			return "", errors.New("no container given")
		}

		n := clampTail(tailLines)

		stream, err := s.client.Logs(ctx, namespace, driver, containerID, false, int32(n))
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		return logTailJSON(stream.Recv, n)
	})
}

// StartContainerLogFollow streams a container's log from node, like
// `talosctl logs [-k] -f ID --tail N` (os:reader). See ContainerLogs for namespace and
// containerID.
func StartContainerLogFollow(configYAML, contextName, node, namespace, containerID string, tailLines int, listener LogListener) *LogRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)
	containerID = privacy.unmaskText(containerID)
	namespace, driver, nsErr := logNamespace(namespace)

	listener = maskedLogListener{listener}

	ctx, cancel := context.WithCancel(context.Background())
	tail := clampTail(tailLines)

	open := func(ctx context.Context, s *session) (func() (*common.Data, error), error) {
		if nsErr != nil {
			return nil, nsErr
		}

		if strings.TrimSpace(containerID) == "" {
			return nil, errors.New("no container given")
		}

		stream, err := s.client.Logs(ctx, namespace, driver, containerID, true, int32(tail))
		if err != nil {
			return nil, err
		}

		return stream.Recv, nil
	}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		listener.OnDone(followLog(ctx, configYAML, contextName, node, open, listener, containerID, tailLines))
	}()

	return &LogRun{cancel: cancel}
}

// logNamespace is the namespace and driver a container log is asked with: "" is a Kubernetes
// container (the apps before Talos containers sent no namespace). A Talos container's log is
// asked in its namespace through containerd, like `talosctl logs --namespace taloscontainers`.
func logNamespace(namespace string) (string, common.ContainerDriver, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = constants.K8sContainerdNamespace
	}

	driver, err := containerDriver(namespace)

	return namespace, driver, err
}
