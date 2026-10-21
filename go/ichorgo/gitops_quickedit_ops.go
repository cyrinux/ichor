package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

const quickEditMaxReplicas = 10000

// quickEditTarget is one value to write: node is the scalar to rewrite, or nil to add the
// line "key: value" after the line of after, indented like its siblings (indent).
type quickEditTarget struct {
	node   *yaml.Node
	after  *yaml.Node
	indent int
	key    string
	value  string
	tag    string
}

func gitOpsQuickEdit(content string, e quickEdit) (quickEditResult, error) {
	if e.Op == "talos" {
		return talosQuickEdit(content, e)
	}

	docs, err := parseQuickEditDocs(content)
	if err != nil {
		return quickEditResult{}, err
	}

	if e.Op == "keep" {
		return keepLiveEdit(content, docs, e)
	}

	targets, err := quickEditLocate(docs, e)
	if err != nil {
		return quickEditResult{}, err
	}

	out, line, err := spliceTargets(content, targets)
	if err != nil {
		return quickEditResult{}, err
	}

	if out == content {
		return quickEditResult{Content: content, Line: line}, nil
	}

	if err := verifyQuickEdit(out, len(docs), e); err != nil {
		return quickEditResult{}, err
	}

	return quickEditResult{Content: out, Changed: true, Line: line}, nil
}

// keepLiveEdit writes the live values of e.Changes the document holds (see keepTargets).
func keepLiveEdit(content string, docs []*yaml.Node, e quickEdit) (quickEditResult, error) {
	index, err := quickEditDocIndex(docs, e)
	if err != nil {
		return quickEditResult{}, err
	}

	root := docs[index]
	if root == nil || root.Kind != yaml.MappingNode {
		return quickEditResult{}, fmt.Errorf("document %d is not a manifest", index)
	}

	targets, applied, skipped, err := keepTargets(root, e)
	if err != nil {
		return quickEditResult{}, err
	}

	out, line, err := spliceTargets(content, targets)
	if err != nil {
		return quickEditResult{}, err
	}

	if out == content {
		return quickEditResult{Content: content, Line: line, Skipped: skipped}, nil
	}

	if err := verifyKeep(out, len(docs), e, applied); err != nil {
		return quickEditResult{}, err
	}

	return quickEditResult{Content: out, Changed: true, Line: line, Skipped: skipped}, nil
}

// verifyQuickEdit parses the rewritten file again: same documents, and every target now
// holds its new value.
func verifyQuickEdit(out string, count int, e quickEdit) error {
	docs, err := parseQuickEditDocs(out)
	if err != nil || len(docs) != count {
		return errors.New("the rewritten file does not parse the same: edit it in the editor")
	}

	targets, err := quickEditLocate(docs, e)
	if err != nil {
		return fmt.Errorf("the rewritten file does not parse the same: %w", err)
	}

	for _, t := range targets {
		if t.node == nil || t.node.Value != t.value {
			return errors.New("the rewritten file does not hold the new value: edit it in the editor")
		}
	}

	return nil
}

func quickEditLocate(docs []*yaml.Node, e quickEdit) ([]quickEditTarget, error) {
	index, err := quickEditDocIndex(docs, e)
	if err != nil {
		return nil, err
	}

	root := docs[index]
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("document %d is not a manifest", index)
	}

	switch e.Op {
	case "image":
		if e.KustomizeImage != "" {
			return kustImageTargets(root, e)
		}

		return containerImageTargets(root, e)
	case "replicas":
		return replicasTargets(root, e)
	case "path":
		return pathTargets(root, e)
	}

	return nil, fmt.Errorf("unknown quick edit %q: image, replicas, path or keep", e.Op)
}

// quickEditDocIndex is the document the edit applies to: documentIndex, unless the edit names
// its object (kind, name) and that document is another one; then the one document of the
// file with that kind and name.
func quickEditDocIndex(docs []*yaml.Node, e quickEdit) (int, error) {
	if e.Kind == "" && e.Name == "" {
		if e.DocumentIndex >= len(docs) {
			return 0, fmt.Errorf("document %d is not in the file (it has %d)", e.DocumentIndex, len(docs))
		}

		return e.DocumentIndex, nil
	}

	if e.DocumentIndex < len(docs) && isObject(docs[e.DocumentIndex], e.Kind, e.Name) {
		return e.DocumentIndex, nil
	}

	label := strings.TrimSpace(e.Kind + " " + e.Name)
	found := -1

	for i, root := range docs {
		if !isObject(root, e.Kind, e.Name) {
			continue
		}

		if found >= 0 {
			return 0, fmt.Errorf("%s is in this file more than once: reload, or edit it in the editor", label)
		}

		found = i
	}

	if found < 0 {
		return 0, fmt.Errorf("%s is not in this file any more: reload", label)
	}

	return found, nil
}

// isObject: root has this kind and metadata.name (an empty one matches any).
func isObject(root *yaml.Node, kind, name string) bool {
	if root == nil || root.Kind != yaml.MappingNode {
		return false
	}

	return (kind == "" || scalarAt(root, "kind") == kind) && (name == "" || scalarAt(mapValue(root, "metadata"), "name") == name)
}

func objectLabel(root *yaml.Node) string {
	kind, name := scalarAt(root, "kind"), scalarAt(mapValue(root, "metadata"), "name")
	if kind == "" {
		kind = "document"
	}

	return strings.TrimSpace(kind + " " + name)
}

func validImageText(s string) error {
	if s == "" || len(s) > 512 || strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return fmt.Errorf("%q is not an image reference", s)
	}

	return nil
}

func containerImageTargets(root *yaml.Node, e quickEdit) ([]quickEditTarget, error) {
	if err := validImageText(e.Image); err != nil {
		return nil, err
	}

	cs := containersOf(root)
	if len(cs) == 0 {
		return nil, fmt.Errorf("%s has no containers", objectLabel(root))
	}

	c, err := pickContainer(cs, e.Container, objectLabel(root))
	if err != nil {
		return nil, err
	}

	if c.image == nil || c.image.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("container %s in %s has no image", c.name, objectLabel(root))
	}

	image := e.Image
	if isBareTag(image) {
		image = imageRepo(c.image.Value) + ":" + image
	}

	return []quickEditTarget{{node: c.image, value: image, tag: "!!str"}}, nil
}

func pickContainer(cs []containerRef, name, label string) (containerRef, error) {
	if name == "" {
		if len(cs) == 1 {
			return cs[0], nil
		}

		names := make([]string, 0, len(cs))
		for _, c := range cs {
			names = append(names, c.name)
		}

		return containerRef{}, fmt.Errorf("say which container of %s: %s", label, strings.Join(names, ", "))
	}

	for _, c := range cs {
		if c.name == name {
			return c, nil
		}
	}

	return containerRef{}, fmt.Errorf("container %s is not in %s", name, label)
}

// kustImageTargets edits the kustomize images entry named e.KustomizeImage: a bare tag sets
// newTag; a "name:tag" reference sets newTag, and newName when name differs from the
// image the entry produces today. Missing keys are added under the entry's name.
func kustImageTargets(root *yaml.Node, e quickEdit) ([]quickEditTarget, error) {
	if err := validImageText(e.Image); err != nil {
		return nil, err
	}

	entry := kustEntry(kustomizeList(root, "images"), e.KustomizeImage)
	if entry == nil {
		return nil, fmt.Errorf("image %s is not in the kustomization's images", e.KustomizeImage)
	}

	if scalarAt(entry, "digest") != "" {
		return nil, fmt.Errorf("image %s is pinned by digest: edit it in the editor", e.KustomizeImage)
	}

	if strings.Contains(e.Image, "@") {
		return nil, errors.New("a digest cannot be set from a quick edit: edit it in the editor")
	}

	name, tag := "", e.Image
	if !isBareTag(e.Image) {
		name = imageRepo(e.Image)
		tag = strings.TrimPrefix(strings.TrimPrefix(e.Image, name), ":")

		if tag == "" {
			return nil, fmt.Errorf("say the tag: %s:<tag>", name)
		}
	}

	var out []quickEditTarget

	current := scalarAt(entry, "newName")
	if current == "" {
		current = e.KustomizeImage
	}

	if name != "" && name != current {
		t, err := entryTarget(entry, "newName", name, "!!str")
		if err != nil {
			return nil, err
		}

		out = append(out, t)
	}

	t, err := entryTarget(entry, "newTag", tag, "!!str")
	if err != nil {
		return nil, err
	}

	return append(out, t), nil
}

func kustEntry(list *yaml.Node, name string) *yaml.Node {
	for _, entry := range seqItems(list) {
		if scalarAt(entry, "name") == name {
			return entry
		}
	}

	return nil
}

// entryTarget rewrites entry.key, or adds it after the entry's name line.
func entryTarget(entry *yaml.Node, key, value, tag string) (quickEditTarget, error) {
	if v := mapValue(entry, key); v != nil {
		if v.Kind != yaml.ScalarNode {
			return quickEditTarget{}, fmt.Errorf("%s is not a plain value: edit it in the editor", key)
		}

		return quickEditTarget{node: v, value: value, tag: tag}, nil
	}

	nameKey, nameValue := mapEntry(entry, "name")
	if entry.Style&yaml.FlowStyle != 0 || nameKey == nil || nameValue.Line != nameKey.Line {
		return quickEditTarget{}, fmt.Errorf("%s cannot be added here: edit it in the editor", key)
	}

	return quickEditTarget{after: nameValue, indent: nameKey.Column - 1, key: key, value: value, tag: tag}, nil
}

func replicasTargets(root *yaml.Node, e quickEdit) ([]quickEditTarget, error) {
	if e.Replicas == nil || *e.Replicas < 0 || *e.Replicas > quickEditMaxReplicas {
		return nil, fmt.Errorf("replicas must be between 0 and %d", quickEditMaxReplicas)
	}

	value := strconv.Itoa(*e.Replicas)

	if e.KustomizeReplica != "" {
		entry := kustEntry(kustomizeList(root, "replicas"), e.KustomizeReplica)
		if entry == nil {
			return nil, fmt.Errorf("%s is not in the kustomization's replicas", e.KustomizeReplica)
		}

		t, err := entryTarget(entry, "count", value, "!!int")
		if err != nil {
			return nil, err
		}

		return []quickEditTarget{t}, nil
	}

	if !isScalable(root) {
		return nil, fmt.Errorf("%s has no replicas", objectLabel(root))
	}

	specKey, spec := mapEntry(root, "spec")
	if spec == nil || spec.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s has no spec", objectLabel(root))
	}

	if v := mapValue(spec, "replicas"); v != nil {
		if v.Kind != yaml.ScalarNode {
			return nil, errors.New("replicas is not a plain value: edit it in the editor")
		}

		return []quickEditTarget{{node: v, value: value, tag: "!!int"}}, nil
	}

	if spec.Style&yaml.FlowStyle != 0 || len(spec.Content) == 0 {
		return nil, errors.New("replicas cannot be added to this spec: edit it in the editor")
	}

	return []quickEditTarget{{after: specKey, indent: spec.Content[0].Column - 1, key: "replicas", value: value, tag: "!!int"}}, nil
}

// pathTargets rewrites the scalar at a dotted path (spec.values.image.tag); a number
// segment indexes a list (spec.values.sidecars.0.tag).
func pathTargets(root *yaml.Node, e quickEdit) ([]quickEditTarget, error) {
	value, tag, err := pathValue(e.Value)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(e.Path) == "" {
		return nil, errors.New("say the path to change, like spec.values.image.tag")
	}

	n := root

	for _, seg := range strings.Split(e.Path, ".") {
		next := pathStep(n, seg)
		if next == nil {
			return nil, fmt.Errorf("%s is not in %s: %s is missing", e.Path, objectLabel(root), seg)
		}

		n = next
	}

	if n.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("%s is not a plain value: edit it in the editor", e.Path)
	}

	return []quickEditTarget{{node: n, value: value, tag: tag}}, nil
}

func pathStep(n *yaml.Node, seg string) *yaml.Node {
	switch n.Kind {
	case yaml.MappingNode:
		return mapValue(n, seg)
	case yaml.SequenceNode:
		if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(n.Content) {
			return n.Content[i]
		}
	}

	return nil
}

// pathValue is the text and YAML tag of the JSON value of a path edit: a string, a number
// or a boolean on one line.
func pathValue(raw json.RawMessage) (string, string, error) {
	s := strings.TrimSpace(string(raw))

	switch {
	case s == "" || s == "null":
		return "", "", errors.New("say the new value")
	case s[0] == '"':
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", "", fmt.Errorf("the value is not valid: %w", err)
		}

		if strings.ContainsAny(v, "\n\r") || len(v) > 1024 {
			return "", "", errors.New("a quick edit writes one line: edit it in the editor")
		}

		return v, "!!str", nil
	case s == "true" || s == "false":
		return s, "!!bool", nil
	}

	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return s, "!!int", nil
	}

	if _, err := strconv.ParseFloat(s, 64); err == nil && !strings.ContainsAny(s, "{[") {
		return s, "!!float", nil
	}

	return "", "", errors.New("the value must be a string, a number or true/false")
}

// mapEntry is the key and value nodes of key in the mapping m.
func mapEntry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}

	return nil, nil
}
