package talosmobile

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"google.golang.org/protobuf/types/known/durationpb"
)

const maxTalosconfigTTLHours = 87600 // 10 years

// allowedRoles are the Talos API roles a generated talosconfig may carry.
var allowedRoles = []string{"os:admin", "os:operator", "os:reader", "os:etcd:backup"}

// GenerateTalosconfig issues a new client certificate with the given comma-separated roles
// (e.g. "os:reader") valid for ttlHours, like `talosctl config new --roles ... --crt-ttl ...`
// (os:admin). The returned talosconfig YAML has a single context with the same name,
// endpoints and nodes as contextName, but with the newly issued CA/crt/key. It serves both
// "renew my certificate" (same roles) and "create a restricted config for another device".
func GenerateTalosconfig(configYAML, contextName, roles string, ttlHours int) (string, error) {
	roleList, err := parseRoles(roles)
	if err != nil {
		return "", err
	}

	if ttlHours < 1 || ttlHours > maxTalosconfigTTLHours {
		return "", fmt.Errorf("certificate lifetime must be between 1 and %d hours", maxTalosconfigTTLHours)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		info := classifyNodes(ctx, s.client, targetNodes(s.context))

		if len(info.GetControlPlaneNodes()) == 0 {
			return "", errors.New("no reachable control-plane node found in this context")
		}

		resp, err := s.client.GenerateClientConfiguration(client.WithNode(ctx, info.GetControlPlaneNodes()[0]),
			&machineapi.GenerateClientConfigurationRequest{
				Roles:  roleList,
				CrtTtl: durationpb.New(time.Duration(ttlHours) * time.Hour),
			})
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		gen := first(resp.GetMessages())
		if gen == nil {
			return "", errors.New("the node returned no client configuration")
		}

		return buildTalosconfig(name, s.context, gen.GetCa(), gen.GetCrt(), gen.GetKey())
	})
}

// parseRoles splits, trims and de-duplicates a comma-separated role list, rejecting
// unknown roles so a typo cannot silently produce a config without any rights.
func parseRoles(roles string) ([]string, error) {
	var out []string

	for r := range strings.SplitSeq(roles, ",") {
		r = strings.TrimSpace(r)
		if r == "" || slices.Contains(out, r) {
			continue
		}

		if !slices.Contains(allowedRoles, r) {
			return nil, fmt.Errorf("unknown role %q (%s)", r, strings.Join(allowedRoles, ", "))
		}

		out = append(out, r)
	}

	if len(out) == 0 {
		return nil, errors.New("no role given")
	}

	return out, nil
}

// buildTalosconfig writes a single-context talosconfig reusing src's endpoints and nodes
// with the given PEM CA/certificate/key, base64-encoded like talosctl writes them.
func buildTalosconfig(contextName string, src *clientconfig.Context, caPEM, crtPEM, keyPEM []byte) (string, error) {
	if len(caPEM) == 0 || len(crtPEM) == 0 || len(keyPEM) == 0 {
		return "", errors.New("the generated client configuration is incomplete")
	}

	enc := base64.StdEncoding.EncodeToString
	cfg := &clientconfig.Config{
		Context: contextName,
		Contexts: map[string]*clientconfig.Context{
			contextName: {
				Endpoints: slices.Clone(src.Endpoints),
				Nodes:     slices.Clone(src.Nodes),
				CA:        enc(caPEM),
				Crt:       enc(crtPEM),
				Key:       enc(keyPEM),
			},
		},
	}

	out, err := cfg.Bytes()
	if err != nil {
		return "", fmt.Errorf("encode talosconfig: %w", err)
	}

	return string(out), nil
}
