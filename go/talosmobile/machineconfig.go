package talosmobile

import (
	"context"
	"errors"
	"fmt"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
)

// redacted replaces every secret when the config is shown without revealSecrets.
const redacted = "******"

// NodeMachineConfig returns node's active machine configuration as YAML, like
// `talosctl -n NODE get machineconfig -o yaml` (os:admin: the resource is sensitive).
// Secrets (CA keys, tokens, encryption keys...) are masked with Talos's own RedactSecrets
// unless revealSecrets is set, so they only reach the app when explicitly requested.
func NodeMachineConfig(configYAML, contextName, node string, revealSecrets bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		out, err := machineConfigYAML(ctx, s, node, revealSecrets)
		if err != nil {
			return "", err
		}

		return out, nil
	})
}

func machineConfigYAML(ctx context.Context, s *session, node string, revealSecrets bool) (string, error) {
	mc, err := safe.StateGetByID[*config.MachineConfig](client.WithNode(ctx, node), s.client.COSI, config.ActiveID)
	if err != nil {
		return "", errors.New(s.friendly(node, err))
	}

	provider := mc.Provider()
	if !revealSecrets {
		provider = provider.RedactSecrets(redacted)
	}

	out, err := provider.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", fmt.Errorf("encode machine config: %w", err)
	}

	return out, nil
}
