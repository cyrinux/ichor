package ichorgo

import (
	"errors"
	"fmt"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// ReplaceContextCredentials returns storedYAML with contextName's CA, certificate and key
// replaced by those of generatedYAML's single context (as GenerateTalosconfig returns).
// Other contexts, and contextName's endpoints and nodes, are kept. Used to renew the
// stored config's certificate in place.
func ReplaceContextCredentials(storedYAML, generatedYAML, contextName string) (out string, err error) {
	// The result is a talosconfig the user saves: only the error is masked.
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	stored, err := clientconfig.FromString(storedYAML)
	if err != nil {
		return "", fmt.Errorf("stored talosconfig: %w", err)
	}

	generated, err := clientconfig.FromString(generatedYAML)
	if err != nil {
		return "", fmt.Errorf("new talosconfig: %w", err)
	}

	if len(generated.Contexts) != 1 {
		return "", fmt.Errorf("new talosconfig must have exactly one context, got %d", len(generated.Contexts))
	}

	var source *clientconfig.Context
	for _, c := range generated.Contexts {
		source = c
	}

	if source.CA == "" || source.Crt == "" || source.Key == "" {
		return "", errors.New("new talosconfig has no client credentials")
	}

	target, ok := stored.Contexts[contextName]
	if !ok || target == nil {
		return "", fmt.Errorf("context %q not found in the stored talosconfig", contextName)
	}

	renewed := *target
	renewed.CA = source.CA
	renewed.Crt = source.Crt
	renewed.Key = source.Key

	contexts := make(map[string]*clientconfig.Context, len(stored.Contexts))
	for name, c := range stored.Contexts {
		contexts[name] = c
	}

	contexts[contextName] = &renewed

	merged := *stored
	merged.Contexts = contexts

	encoded, err := merged.Bytes()
	if err != nil {
		return "", fmt.Errorf("encode talosconfig: %w", err)
	}

	return string(encoded), nil
}
