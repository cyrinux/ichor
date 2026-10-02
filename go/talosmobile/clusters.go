package talosmobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// ImportConflicts lists the contexts of addedYAML named like a stored one, as JSON
// [{index, suggested, sameAs}]: index is the context's position among addedYAML's sorted
// contexts (the order ParseConfig lists them in), suggested the free name it gets unless
// the user picks another, and sameAs the stored context of the same cluster (same CA) it
// may replace instead, if any.
func ImportConflicts(storedYAML, addedYAML string) (out string, err error) {
	defer maskResult(&out, &err)

	stored, added, err := loadMergeInputs(storedYAML, addedYAML)
	if err != nil {
		return "", err
	}

	conflicts := []importConflict{}
	taken := takenNames(stored)
	reserved := takenNames(added)

	for i, name := range sortedContextNames(added) {
		if !taken[name] {
			taken[name] = true

			continue
		}

		suggested := freeName(taken, reserved, name)
		taken[suggested] = true

		conflicts = append(conflicts, importConflict{
			Index:     i,
			Suggested: suggested,
			SameAs:    sameClusterName(stored.Contexts, name, added.Contexts[name]),
		})
	}

	return toJSON(conflicts)
}

type importConflict struct {
	Index     int    `json:"index"`
	Suggested string `json:"suggested"`
	SameAs    string `json:"sameAs,omitempty"`
}

// importChoice is what the user picked for a clashing context (see ImportConflicts): a name
// of their own, or replacing the stored context of the same cluster. Neither: the suggested name.
type importChoice struct {
	Index   int    `json:"index"`
	Name    string `json:"name,omitempty"`
	Replace bool   `json:"replace,omitempty"`
}

// MergeConfig returns storedYAML with the contexts of addedYAML added, so several clusters
// live in the one stored talosconfig. A stored context is never overwritten unless
// choicesJSON (a JSON array of importChoice, "" for none) asks to replace it with the same
// cluster: a context named like a stored one gets the name the user chose, else name-1,
// name-2… like talosctl config merge. addedYAML's current context, under its final name,
// becomes the current one.
func MergeConfig(storedYAML, addedYAML, choicesJSON string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	stored, added, err := loadMergeInputs(storedYAML, addedYAML)
	if err != nil {
		return "", err
	}

	choices, err := parseImportChoices(choicesJSON)
	if err != nil {
		return "", err
	}

	contexts := make(map[string]*clientconfig.Context, len(stored.Contexts)+len(added.Contexts))
	for name, c := range stored.Contexts {
		contexts[name] = c
	}

	taken := takenNames(stored)
	reserved := takenNames(added)
	current := defaultContextName(added)

	// Sorted, so the indexes and suffixes do not depend on the map order.
	for i, name := range sortedContextNames(added) {
		ctx := added.Contexts[name]

		final, err := mergedName(stored, taken, reserved, name, ctx, choices[i])
		if err != nil {
			return "", err
		}

		taken[final] = true
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

func loadMergeInputs(storedYAML, addedYAML string) (stored, added *clientconfig.Config, err error) {
	stored, err = clientconfig.FromString(storedYAML)
	if err != nil {
		return nil, nil, fmt.Errorf("stored talosconfig: %w", err)
	}

	added, err = loadConfig(addedYAML)
	if err != nil {
		return nil, nil, err
	}

	return stored, added, nil
}

func parseImportChoices(choicesJSON string) (map[int]importChoice, error) {
	byIndex := map[int]importChoice{}
	if strings.TrimSpace(choicesJSON) == "" {
		return byIndex, nil
	}

	var choices []importChoice
	if err := json.Unmarshal([]byte(choicesJSON), &choices); err != nil {
		return nil, fmt.Errorf("import choices: %w", err)
	}

	for _, c := range choices {
		c.Name = strings.TrimSpace(c.Name)
		byIndex[c.Index] = c
	}

	return byIndex, nil
}

func takenNames(cfg *clientconfig.Config) map[string]bool {
	taken := make(map[string]bool, len(cfg.Contexts))
	for name := range cfg.Contexts {
		taken[name] = true
	}

	return taken
}

// mergedName is the name ctx (name in the imported config) is stored under: its own when
// free, else the stored context of the same cluster when the user chose to replace it, the
// name the user chose, or the first free name-N. A taken name (stored, or given to another
// imported context) is refused rather than overwritten; reserved holds the imported names.
func mergedName(stored *clientconfig.Config, taken, reserved map[string]bool, name string, ctx *clientconfig.Context, choice importChoice) (string, error) {
	if !taken[name] {
		return name, nil
	}

	switch {
	case choice.Replace:
		same := sameClusterName(stored.Contexts, name, ctx)
		if same == "" {
			return "", fmt.Errorf("context %q is another cluster than the stored one: it cannot replace it", name)
		}

		return same, nil
	case choice.Name != "":
		if taken[choice.Name] || (reserved[choice.Name] && choice.Name != name) {
			return "", fmt.Errorf("a cluster named %q is already imported", choice.Name)
		}

		return choice.Name, nil
	default:
		return freeName(taken, reserved, name), nil
	}
}

// freeName is the first of name-1, name-2… in neither taken nor reserved.
func freeName(taken, reserved map[string]bool, name string) string {
	for i := 1; ; i++ {
		if candidate := fmt.Sprintf("%s-%d", name, i); !taken[candidate] && !reserved[candidate] {
			return candidate
		}
	}
}

// sameClusterName is the stored context named name, or name-N (renamed by an earlier
// merge), that is the same cluster as ctx; "" when there is none.
func sameClusterName(contexts map[string]*clientconfig.Context, name string, ctx *clientconfig.Context) string {
	if sameCluster(contexts[name], ctx) {
		return name
	}

	for _, candidate := range sortedContextNames(&clientconfig.Config{Contexts: contexts}) {
		suffix, ok := strings.CutPrefix(candidate, name+"-")
		if ok && isDigits(suffix) && sameCluster(contexts[candidate], ctx) {
			return candidate
		}
	}

	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}

	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}

	return true
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
