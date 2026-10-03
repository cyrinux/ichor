package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// Reboot reboots node like `talosctl reboot -m MODE` (os:operator role or higher). Modes:
//   - "default": stop pods and services gracefully, then reboot (kexec when available);
//   - "powercycle": same, but a full firmware reboot instead of kexec;
//   - "force": skip all teardown and reboot immediately.
func Reboot(configYAML, contextName, node, mode string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	m, err := parseRebootMode(mode)
	if err != nil {
		return err
	}

	return powerAction(configYAML, contextName, node, func(ctx context.Context, c *client.Client) error {
		return c.Reboot(ctx, client.WithRebootMode(m))
	})
}

// Shutdown powers node off like `talosctl shutdown [--force]` (os:operator role or higher).
// force skips the Kubernetes cordon/drain; pods and services are still stopped.
func Shutdown(configYAML, contextName, node string, force bool) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return powerAction(configYAML, contextName, node, func(ctx context.Context, c *client.Client) error {
		return c.Shutdown(ctx, client.WithShutdownForce(force))
	})
}

func parseRebootMode(mode string) (machineapi.RebootRequest_Mode, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "default":
		return machineapi.RebootRequest_DEFAULT, nil
	case "powercycle":
		return machineapi.RebootRequest_POWERCYCLE, nil
	case "force":
		return machineapi.RebootRequest_FORCE, nil
	default:
		return 0, fmt.Errorf("unknown reboot mode %q (default, powercycle, force)", mode)
	}
}

func powerAction(configYAML, contextName, node string, action func(context.Context, *client.Client) error) error {
	_, err := withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (struct{}, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return struct{}{}, err
		}

		if err := action(client.WithNode(ctx, node), s.client); err != nil {
			return struct{}{}, errors.New(s.friendly(node, err))
		}

		return struct{}{}, nil
	})

	return err
}

// validatePowerTarget only allows nodes listed in the context. Without node metadata the
// request would go to the endpoint itself, so an empty or unknown node must never pass.
func validatePowerTarget(ctx *clientconfig.Context, node string) error {
	if strings.TrimSpace(node) == "" {
		return errors.New("no target node given")
	}

	if !slices.Contains(targetNodes(ctx), node) {
		return fmt.Errorf("node %q is not part of this context", node)
	}

	return nil
}
