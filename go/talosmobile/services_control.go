package talosmobile

import (
	"context"
	"errors"
	"strings"
)

// ServiceAction starts, stops or restarts a Talos service on node, like
// `talosctl service SERVICE start|stop|restart` (os:operator or os:admin).
func ServiceAction(configYAML, contextName, node, service, action string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	action = strings.ToLower(strings.TrimSpace(action))
	if err := validateServiceAction(service, action); err != nil {
		return err
	}

	_, err = withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (struct{}, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return struct{}{}, err
		}

		nodeCtx := withNode(ctx, node)

		var err error

		switch action {
		case "start":
			_, err = s.client.ServiceStart(nodeCtx, service)
		case "stop":
			_, err = s.client.ServiceStop(nodeCtx, service)
		case "restart":
			_, err = s.client.ServiceRestart(nodeCtx, service)
		}

		if err != nil {
			return struct{}{}, errors.New(s.friendly(node, err))
		}

		return struct{}{}, nil
	})

	return err
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
