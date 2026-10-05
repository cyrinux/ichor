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

	generated, err := parseTalosconfig(generatedYAML)
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

	return editContext(storedYAML, contextName, func(c *clientconfig.Context) error {
		c.CA, c.Crt, c.Key = source.CA, source.Crt, source.Key

		return nil
	})
}
