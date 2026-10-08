package ichorgo

import (
	"bytes"

	"go.yaml.in/yaml/v4"
)

// dedupeContexts returns configYAML with each context named once: a talosconfig that names a
// context twice (a file appended to itself, or to another export of the cluster) is refused
// by the YAML decoder outright. Identical copies collapse into one; copies that differ are
// kept apart as name-1, name-2… the way MergeConfig keeps the contexts of two files, so the
// user sees both and picks. Text that is not such a mapping, or has no duplicate, is
// returned as is: its errors keep their line numbers.
func dedupeContexts(configYAML string) string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(configYAML), &doc); err != nil || len(doc.Content) == 0 {
		return configYAML
	}

	root := doc.Content[0]

	contexts := mappingValue(root, "contexts")
	if contexts == nil || contexts.Kind != yaml.MappingNode {
		return configYAML
	}

	deduped, changed := dedupeMapping(contexts)
	if !changed {
		return configYAML
	}

	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{withMappingValue(root, "contexts", deduped)}})
	if err != nil {
		return configYAML
	}

	return string(out)
}

// dedupeMapping is mapping with its repeated keys collapsed (same value) or renamed to the
// first free key-N (another value); changed says whether any was.
func dedupeMapping(mapping *yaml.Node) (*yaml.Node, bool) {
	reserved := make(map[string]bool, len(mapping.Content)/2)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		reserved[mapping.Content[i].Value] = true
	}

	taken := map[string]bool{}
	seen := map[string]*yaml.Node{}
	content := make([]*yaml.Node, 0, len(mapping.Content))
	changed := false

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]

		first, dup := seen[key.Value]
		if !dup {
			seen[key.Value] = value
			taken[key.Value] = true
			content = append(content, key, value)

			continue
		}

		changed = true

		if sameNode(first, value) {
			continue
		}

		renamed := *key
		renamed.Value = freeName(taken, reserved, key.Value)
		taken[renamed.Value] = true
		content = append(content, &renamed, value)
	}

	if !changed {
		return mapping, false
	}

	out := *mapping
	out.Content = content

	return &out, true
}

// sameNode compares two YAML nodes by their encoding: comments and style aside, the same data.
func sameNode(a, b *yaml.Node) bool {
	ea, errA := yaml.Marshal(a)
	eb, errB := yaml.Marshal(b)

	return errA == nil && errB == nil && bytes.Equal(ea, eb)
}

// withMappingValue is a copy of mapping with key's value replaced.
func withMappingValue(mapping *yaml.Node, key string, value *yaml.Node) *yaml.Node {
	out := *mapping
	out.Content = make([]*yaml.Node, len(mapping.Content))
	copy(out.Content, mapping.Content)

	for i := 0; i+1 < len(out.Content); i += 2 {
		if out.Content[i].Value == key {
			out.Content[i+1] = value
		}
	}

	return &out
}
