package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// ContainerLogs returns the last tailLines lines of a Kubernetes container's log as JSON
// logTail (same shape as ServiceLogs), like `talosctl logs -k ID --tail N` (os:reader).
// containerID is the "id" NodeContainers returns ("namespace/pod:container:hash").
func ContainerLogs(configYAML, contextName, node, containerID string, tailLines int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ContainerLogs", configYAML, contextName, node, containerID, fmt.Sprint(tailLines))
	}
	containerID = privacy.unmaskText(containerID)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		if strings.TrimSpace(containerID) == "" {
			return "", errors.New("no container given")
		}

		n := clampTail(tailLines)

		stream, err := s.client.Logs(withNode(ctx, node), constants.K8sContainerdNamespace,
			common.ContainerDriver_CRI, containerID, false, int32(n))
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		tail, err := drainStream(stream.Recv, n)
		if err != nil {
			return "", err
		}

		return toJSON(tail)
	})
}

// StartContainerLogFollow streams a Kubernetes container's log from node, like
// `talosctl logs -k -f ID --tail N` (os:reader). See ContainerLogs for containerID.
func StartContainerLogFollow(configYAML, contextName, node, containerID string, tailLines int, listener LogListener) *LogRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)
	containerID = privacy.unmaskText(containerID)

	listener = maskedLogListener{listener}

	ctx, cancel := context.WithCancel(context.Background())
	tail := clampTail(tailLines)

	open := func(ctx context.Context, s *session) (func() (*common.Data, error), error) {
		if strings.TrimSpace(containerID) == "" {
			return nil, errors.New("no container given")
		}

		stream, err := s.client.Logs(ctx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI, containerID, true, int32(tail))
		if err != nil {
			return nil, err
		}

		return stream.Recv, nil
	}

	go func() {
		defer cancel()

		listener.OnDone(followLog(ctx, configYAML, contextName, node, open, listener, containerID, tailLines))
	}()

	return &LogRun{cancel: cancel}
}
