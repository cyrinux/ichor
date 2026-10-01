package talosmobile

import (
	"errors"
	"fmt"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// MergeConfig returns storedYAML with the contexts of addedYAML added, so several clusters
// live in the one stored talosconfig. A context named like a stored one replaces it when it
// is the same cluster (same CA: a renewed or re-issued config), and is otherwise added as
// name-1, name-2… like talosctl config merge. addedYAML's current context, under its final
// name, becomes the current one.
func MergeConfig(storedYAML, addedYAML string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	stored, err := clientconfig.FromString(storedYAML)
	if err != nil {
		return "", fmt.Errorf("stored talosconfig: %w", err)
	}

	added, err := loadConfig(addedYAML)
	if err != nil {
		return "", err
	}

	contexts := make(map[string]*clientconfig.Context, len(stored.Contexts)+len(added.Contexts))
	for name, c := range stored.Contexts {
		contexts[name] = c
	}

	current := defaultContextName(added)

	// Sorted, so the suffixes do not depend on the map order.
	for _, name := range sortedContextNames(added) {
		ctx := added.Contexts[name]
		final := mergedName(contexts, name, ctx)
		contexts[final] = ctx

		if name == current {
			current = final
		}
	}

	merged := *stored
	merged.Contexts = contexts
	merged.Context = current

	return encodeConfig(&merged)
}

// mergedName is the name ctx gets among contexts: its own when free or held by the same
// cluster, else the first free name-N.
func mergedName(contexts map[string]*clientconfig.Context, name string, ctx *clientconfig.Context) string {
	existing, taken := contexts[name]
	if !taken || sameCluster(existing, ctx) {
		return name
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if _, taken := contexts[candidate]; !taken {
			return candidate
		}
	}
}

// sameCluster: both contexts trust the same CA (a cluster's identity; endpoints may move).
func sameCluster(a, b *clientconfig.Context) bool {
	return a != nil && b != nil && a.CA != "" && a.CA == b.CA
}

// RemoveContext returns storedYAML without contextName. The last context cannot be
// removed: the app deletes the stored config instead.
func RemoveContext(storedYAML, contextName string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	contextName = unmaskContext(storedYAML, contextName)

	stored, err := clientconfig.FromString(storedYAML)
	if err != nil {
		return "", fmt.Errorf("stored talosconfig: %w", err)
	}

	if _, ok := stored.Contexts[contextName]; !ok {
		return "", fmt.Errorf("context %q not found in the stored talosconfig", contextName)
	}

	if len(stored.Contexts) == 1 {
		return "", errors.New("cannot remove the only context of the talosconfig")
	}

	contexts := make(map[string]*clientconfig.Context, len(stored.Contexts)-1)

	for name, c := range stored.Contexts {
		if name != contextName {
			contexts[name] = c
		}
	}

	remaining := *stored
	remaining.Contexts = contexts
	remaining.Context = defaultContextName(&remaining)

	return encodeConfig(&remaining)
}

func encodeConfig(cfg *clientconfig.Config) (string, error) {
	encoded, err := cfg.Bytes()
	if err != nil {
		return "", fmt.Errorf("encode talosconfig: %w", err)
	}

	return string(encoded), nil
}
