package ichorgo

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
)

// A draft is edited from the redacted config, so it holds the mask instead of each secret.
// Before it is sent to the node the secrets are put back from the node's real config. This
// is the only place where the real secrets and the user's text meet, and it refuses what it
// cannot place for sure: a mask is never sent to a node, and a secret never moves to
// another field.

// restoreSecrets returns draft with every mask replaced by the secret it hides. realYAML and
// baseYAML are the node's config with and without its secrets (same structure); draftYAML
// is what the user made of baseYAML. Refused: a new value typed over a secret, a field
// holding a secret removed, and a mask that cannot be traced back to its secret.
func restoreSecrets(realYAML, baseYAML, draftYAML string) (string, error) {
	realDocs, err := parseConfigDocs(realYAML)
	if err != nil {
		return "", fmt.Errorf("read the node's config: %w", err)
	}

	baseDocs, err := parseConfigDocs(baseYAML)
	if err != nil {
		return "", fmt.Errorf("read the node's config: %w", err)
	}

	if len(realDocs) != len(baseDocs) {
		return "", errors.New("read the node's config: its documents do not line up")
	}

	draftDocs, err := parseConfigDocs(draftYAML)
	if err != nil {
		return "", fmt.Errorf("the edited config is not valid YAML: %w", err)
	}

	if len(draftDocs) == 0 {
		return "", errors.New("the edited config is empty")
	}

	// An alias would copy what its anchor holds, a restored secret included, to wherever it
	// is used. Talos writes neither, so a draft has no use for them.
	for _, draft := range draftDocs {
		if usesAnchors(draft) {
			return "", errors.New("YAML anchors and aliases (&name, *name) are not supported in a machine config edited here")
		}
	}

	// A document is found by what it is, not where it is: they may be reordered or dropped.
	used := make([]bool, len(baseDocs))

	for _, draft := range draftDocs {
		key := configDocKey(draft)
		match := -1

		for i, base := range baseDocs {
			if !used[i] && configDocKey(base) == key {
				match = i

				break
			}
		}

		if match < 0 {
			if err := restoreNode(nil, nil, draft, key); err != nil {
				return "", err
			}

			continue
		}

		used[match] = true

		if err := restoreNode(realDocs[match], baseDocs[match], draft, ""); err != nil {
			return "", err
		}
	}

	return encodeConfigDocs(draftDocs)
}

func errLeftoverMask(path string) error {
	return fmt.Errorf("%s holds the mask of a hidden secret that cannot be traced back to it; edit a list item or move it, not both at once", pathLabel(path))
}

func errSecretChanged(path string) error {
	return fmt.Errorf("%s is a secret: changing it from the app is not supported", pathLabel(path))
}

func pathLabel(path string) string {
	if path == "" {
		return "the config"
	}

	return path
}

// restoreNode puts the secrets of real (base: the same, redacted) back in draft. base is
// nil for what the user added.
func restoreNode(real, base, draft *yaml.Node, path string) error {
	if base == nil || real == nil || base.Kind != draft.Kind || real.Kind != base.Kind {
		if containsMask(draft) {
			return errLeftoverMask(path)
		}

		// Something of another shape written over a secret is a changed secret too.
		if containsMask(base) {
			return errSecretChanged(path)
		}

		return nil
	}

	switch draft.Kind {
	case yaml.MappingNode:
		return restoreMapping(real, base, draft, path)
	case yaml.SequenceNode:
		return restoreSequence(real, base, draft, path)
	case yaml.ScalarNode:
		switch {
		case base.Value != redacted && containsMask(draft):
			return errLeftoverMask(path)
		case base.Value != redacted:
			return nil
		case draft.Value != redacted:
			return errSecretChanged(path)
		}

		draft.Value, draft.Tag, draft.Style = real.Value, real.Tag, real.Style
	}

	return nil
}

func restoreMapping(real, base, draft *yaml.Node, path string) error {
	for i := 0; i+1 < len(base.Content); i += 2 {
		key := base.Content[i].Value
		if mappingValue(draft, key) == nil && containsMask(base.Content[i+1]) {
			return fmt.Errorf("%s holds a secret: removing it from the app is not supported", joinPath(path, key))
		}
	}

	for i := 0; i+1 < len(draft.Content); i += 2 {
		key := draft.Content[i].Value
		if err := restoreNode(mappingValue(real, key), mappingValue(base, key), draft.Content[i+1], joinPath(path, key)); err != nil {
			return err
		}
	}

	return nil
}

// restoreSequence pairs each item of draft with the item of base it comes from. An untouched
// item is found wherever it moved. Nothing identifies an edited item, so it is paired with
// the one at its place only when it is the single edited item of a list that kept its shape;
// otherwise it counts as new, and a mask in it is refused. When items holding secrets look
// the same once redacted, they cannot be told apart at all: such a list must keep its shape.
func restoreSequence(real, base, draft *yaml.Node, path string) error {
	if len(real.Content) != len(base.Content) {
		return errors.New("read the node's config: its lists do not line up")
	}

	from := make([]int, len(draft.Content))
	used := make([]bool, len(base.Content))

	for i, item := range draft.Content {
		from[i] = -1

		// The same place first: equal items (two workers' identical patches) stay put.
		if i < len(base.Content) && !used[i] && nodesEqual(item, base.Content[i]) {
			from[i], used[i] = i, true

			continue
		}

		for j, b := range base.Content {
			if !used[j] && nodesEqual(item, b) {
				from[i], used[j] = j, true

				break
			}
		}
	}

	inPlace, edited := len(draft.Content) == len(base.Content), 0

	for i, j := range from {
		inPlace = inPlace && (j < 0 || j == i)

		if j < 0 {
			edited++
		}
	}

	if !inPlace && hasTwinSecrets(base) {
		return fmt.Errorf("%s holds secrets that cannot be told apart: edit its items without adding, removing or moving any", pathLabel(path))
	}

	if inPlace && edited == 1 {
		for i := range from {
			from[i] = i
		}
	}

	for i, j := range from {
		itemPath := fmt.Sprintf("%s[%d]", path, i)

		if j < 0 {
			if err := restoreNode(nil, nil, draft.Content[i], itemPath); err != nil {
				return err
			}

			continue
		}

		if err := restoreNode(real.Content[j], base.Content[j], draft.Content[i], itemPath); err != nil {
			return err
		}
	}

	return nil
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}

	return path + "." + key
}

// hasTwinSecrets tells whether two items of list hold a secret and look the same redacted.
func hasTwinSecrets(list *yaml.Node) bool {
	for i, a := range list.Content {
		if !containsMask(a) {
			continue
		}

		for _, b := range list.Content[i+1:] {
			if nodesEqual(a, b) {
				return true
			}
		}
	}

	return false
}

// usesAnchors tells whether n or anything under it is an alias or carries an anchor.
func usesAnchors(n *yaml.Node) bool {
	if n == nil {
		return false
	}

	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return true
	}

	for _, c := range n.Content {
		if usesAnchors(c) {
			return true
		}
	}

	return false
}

// containsMask tells whether n or anything under it is the mask of a secret.
func containsMask(n *yaml.Node) bool {
	if n == nil {
		return false
	}

	if n.Kind == yaml.ScalarNode {
		return strings.TrimSpace(n.Value) == redacted
	}

	for _, c := range n.Content {
		if containsMask(c) {
			return true
		}
	}

	return false
}

// nodesEqual compares two YAML trees by content (not by style or position in the text).
func nodesEqual(a, b *yaml.Node) bool {
	if a.Kind != b.Kind || len(a.Content) != len(b.Content) {
		return false
	}

	if a.Kind == yaml.ScalarNode {
		return a.Value == b.Value && a.ShortTag() == b.ShortTag()
	}

	for i := range a.Content {
		if !nodesEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}

	return true
}
