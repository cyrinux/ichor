package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"go.yaml.in/yaml/v4"
)

// The cluster's secrets bundle (talosctl gen secrets --from-controlplane-config): the CAs,
// tokens and encryption keys every config of the cluster is made from. A cluster made from
// the phone keeps them only in its control planes' configs, so the export is a read of a
// control plane's config, rebuilt into the bundle; nothing new is stored on the phone.

var (
	errSecretsPrivacy    = errors.New("turn privacy mode off to export the secrets bundle: it is the cluster's root of trust")
	errSecretsOmni       = errors.New("this cluster's secrets stay with Omni: its configs on the nodes carry none to export")
	errSecretsWorker     = errors.New("a worker's config does not hold the cluster's secrets: pick a control plane")
	errSecretsNoNode     = errors.New("pick the control plane whose config holds the secrets")
	errSecretsIncomplete = errors.New("this node's config does not hold the cluster's CAs: pick a control plane")
)

// ClusterSecretsBundle returns the cluster's secrets bundle as secrets.yaml, read from node's
// machine config (os:admin, a control plane). talosctl gen config --with-secrets accepts the
// result. Refused on a worker, on Omni, on the demo and in privacy mode. Each export is logged
// in the activity log (the node only); the YAML is never logged and only the errors are masked.
func ClusterSecretsBundle(configYAML, contextName, node string) (out string, err error) {
	defer maskErr(&err)

	if privacy.isEnabled() {
		return "", errSecretsPrivacy
	}

	if strings.TrimSpace(node) == "" {
		return "", errSecretsNoNode
	}

	contextName, node = unmaskTarget(configYAML, contextName, strings.TrimSpace(node))

	if isTalosDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "secrets-bundle-exported", Node: node})

	return withNodeSessionFrom(context.Background(), configYAML, contextName, node, configPreviewTimeout, func(ctx context.Context, s *session) (string, error) {
		if isOmni(s.context) {
			return "", errSecretsOmni
		}

		text, err := machineConfigYAML(ctx, s, node, true)
		if err != nil {
			return "", err
		}

		return secretsBundleYAML(text)
	})
}

// secretsBundleYAML rebuilds the secrets bundle from a control plane's config text.
func secretsBundleYAML(configText string) (string, error) {
	provider, err := configloader.NewFromBytes([]byte(configText))
	if err != nil {
		return "", fmt.Errorf("the node's config cannot be read: %s", friendlyError(err))
	}

	if provider.Machine() == nil || provider.Cluster() == nil {
		return "", errSecretsIncomplete
	}

	if provider.Machine().Type() != machine.TypeControlPlane && provider.Machine().Type() != machine.TypeInit {
		return "", errSecretsWorker
	}

	bundle, err := secrets.NewBundleFromConfig(secrets.NewClock(), provider)
	if err != nil {
		return "", fmt.Errorf("rebuild the secrets bundle: %s", friendlyError(err))
	}

	if err := checkBundleComplete(bundle); err != nil {
		return "", err
	}

	data, err := yaml.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("encode the secrets bundle: %s", friendlyError(err))
	}

	return string(data), nil
}

// checkBundleComplete refuses a bundle without the CAs a control plane always holds (a config
// whose secrets live in documents the node did not return).
func checkBundleComplete(b *secrets.Bundle) error {
	if b.Certs == nil || b.Certs.OS == nil || b.Certs.K8s == nil || b.Certs.Etcd == nil ||
		len(b.Certs.OS.Key) == 0 || len(b.Certs.K8s.Key) == 0 || len(b.Certs.Etcd.Key) == 0 {
		return errSecretsIncomplete
	}

	return nil
}
