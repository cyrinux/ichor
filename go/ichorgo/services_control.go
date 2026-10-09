package ichorgo

import (
	"context"
	"errors"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// ServiceAction starts, stops or restarts a Talos service on node, like
// `talosctl service SERVICE start|stop|restart` (os:operator or os:admin).
func ServiceAction(configYAML, contextName, node, service, action string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "service-" + strings.ToLower(strings.TrimSpace(action)), Node: node, Object: "Service/" + service})

	action = strings.ToLower(strings.TrimSpace(action))
	if err := validateServiceAction(service, action); err != nil {
		return err
	}

	return nodeAction(configYAML, contextName, node, callTimeout, func(ctx context.Context, c *client.Client) error {
		var err error

		switch action {
		case "start":
			_, err = c.ServiceStart(ctx, service)
		case "stop":
			_, err = c.ServiceStop(ctx, service)
		case "restart":
			_, err = c.ServiceRestart(ctx, service)
		}

		return err
	})
}

func validateServiceAction(service, action string) error {
	if strings.TrimSpace(service) == "" {
		return errors.New("no service given")
	}

	switch action {
	case "start", "stop", "restart":
		return nil
	default:
		return errUnsupportedAction
	}
}
