package ichorgo

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v4"
)

// The line edits of a write-back: a scalar in place, a block replaced, keys added under a
// parent, a document appended, a new node file.

// writeLeaf writes l into content, whose document holding it is root.
func writeLeaf(content string, root *yaml.Node, l talosLeaf) (string, error) {
	if root.Kind != yaml.MappingNode {
		return "", errors.New("the document is not a mapping: edit it by hand")
	}

	parent := root

	for i, k := range l.path {
		v := mapValue(parent, k)
		if v == nil {
			return insertUnder(content, parent, l.path[i:], l.value)
		}

		if i == len(l.path)-1 {
			return replaceValue(content, parent, k, v, l.value)
		}

		if v.Kind != yaml.MappingNode {
			return "", fmt.Errorf("%s is not a mapping in the file: edit it by hand", strings.Join(l.path[:i+1], "."))
		}

		parent = v
	}

	return content, nil
}

// replaceValue rewrites the value v of key in parent: a scalar in place when the new value is
// a scalar too, else the whole block of the key.
func replaceValue(content string, parent *yaml.Node, key string, v *yaml.Node, value any) (string, error) {
	keyNode := mapKey(parent, key)

	if text, tag, ok := scalarText(value); ok && v.Kind == yaml.ScalarNode && v.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
		starts := lineStarts(content)

		start, end, err := scalarSpan(content, starts, v)
		if err != nil {
			return "", err
		}

		rendered, err := renderScalar(v.Style, text, tag)
		if err != nil {
			return "", err
		}

		return content[:start] + rendered + content[end:], nil
	}

	if v.Style&yaml.FlowStyle != 0 && v.Line != keyNode.Line {
		return "", fmt.Errorf("%s is a flow value over several lines: edit it by hand", key)
	}

	indent := keyNode.Column - 1
	block, err := renderBlock(indent, []string{key}, value)
	if err != nil {
		return "", err
	}

	lines := strings.SplitAfter(content, "\n")
	end := blockEnd(lines, keyNode.Line, indent)

	return strings.Join(lines[:keyNode.Line-1], "") + block + strings.Join(lines[end:], ""), nil
}

// insertUnder adds path: value under the mapping parent, after its last key's block, with the
// indentation of its keys.
func insertUnder(content string, parent *yaml.Node, path []string, value any) (string, error) {
	if parent.Style&yaml.FlowStyle != 0 || len(parent.Content) == 0 {
		return "", fmt.Errorf("%s has nowhere to go in the file (an empty or flow mapping): edit it by hand", strings.Join(path, "."))
	}

	first, last := parent.Content[0], parent.Content[len(parent.Content)-2]
	indent := first.Column - 1

	block, err := renderBlock(indent, path, value)
	if err != nil {
		return "", err
	}

	lines := strings.SplitAfter(content, "\n")
	end := blockEnd(lines, last.Line, last.Column-1)

	head := strings.Join(lines[:end], "")
	if head != "" && !strings.HasSuffix(head, "\n") {
		head += "\n"
	}

	return head + block + strings.Join(lines[end:], ""), nil
}

// blockEnd is the number of lines up to the end of the block of the key on line (1-based) at
// column indent: the lines below it indented deeper (a list may sit at the key's own column),
// blank lines inside included, trailing ones not.
func blockEnd(lines []string, line, indent int) int {
	end := line

	for j := line; j < len(lines); j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}

		if strings.HasPrefix(lines[j], "---") || strings.HasPrefix(lines[j], "...") {
			break
		}

		ind := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
		if ind < indent || ind == indent && !strings.HasPrefix(t, "- ") && t != "-" {
			break
		}

		end = j + 1
	}

	return end
}

// renderBlock is path: value as YAML lines at indent, nested one mapping per key.
func renderBlock(indent int, path []string, value any) (string, error) {
	var v any = value
	for i := len(path) - 1; i >= 0; i-- {
		v = map[string]any{path[i]: v}
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(v); err != nil {
		return "", fmt.Errorf("write %s: %w", strings.Join(path, "."), err)
	}

	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("write %s: %w", strings.Join(path, "."), err)
	}

	pad := strings.Repeat(" ", indent)

	var out strings.Builder

	for _, l := range strings.SplitAfter(buf.String(), "\n") {
		if l != "" {
			out.WriteString(pad + l)
		}
	}

	return out.String(), nil
}

// appendDocument adds l's document at the end of content, holding l alone.
func appendDocument(content string, l talosLeaf) string {
	doc := map[string]any{}
	for k, v := range l.ident {
		doc[k] = v
	}

	block, err := renderBlock(0, l.path, l.value)
	if err != nil {
		return content
	}

	head, _ := renderIdentity(doc)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	return content + "---\n" + head + block
}

func renderIdentity(ident map[string]any) (string, error) {
	var out strings.Builder

	for _, k := range []string{"apiVersion", "kind", "name"} {
		if v, ok := ident[k]; ok {
			b, err := renderBlock(0, []string{k}, v)
			if err != nil {
				return "", err
			}

			out.WriteString(b)
		}
	}

	return out.String(), nil
}

// newNodeFile is a node file holding leaves, the v1alpha1 document naming the node.
func newNodeFile(leaves []talosLeaf, hostname string) (talosWriteBack, error) {
	byDoc := map[string]map[string]any{}

	var order []string

	for _, l := range leaves {
		doc := byDoc[l.doc]
		if doc == nil {
			doc = map[string]any{}
			for k, v := range l.ident {
				doc[k] = v
			}

			byDoc[l.doc] = doc
			order = append(order, l.doc)
		}

		setYAMLPath(doc, l.path, l.value)
	}

	if hostname != "" {
		if byDoc[talosV1Alpha1] == nil {
			byDoc[talosV1Alpha1] = map[string]any{}
			order = append([]string{talosV1Alpha1}, order...)
		}

		setYAMLPath(byDoc[talosV1Alpha1], []string{"machine", "network", "hostname"}, hostname)
	}

	docs := make([]map[string]any, 0, len(order))
	for _, k := range order {
		docs = append(docs, byDoc[k])
	}

	text, err := encodeYAMLDocs(docs)
	if err != nil {
		return talosWriteBack{}, err
	}

	return talosWriteBack{Content: text, Changed: true, Line: 1, Skipped: []talosRepoSkip{}, Created: true}, nil
}

func setYAMLPath(m map[string]any, path []string, value any) {
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}

		m = next
	}

	m[path[len(path)-1]] = value
}

// mapKey is the key node of key in the mapping m.
func mapKey(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i]
		}
	}

	return nil
}

// scalarText is a scalar value's text and YAML tag; false for a list or a mapping.
func scalarText(v any) (string, string, bool) {
	switch x := v.(type) {
	case string:
		return x, "!!str", true
	case bool:
		return fmt.Sprint(x), "!!bool", true
	case int, int64, uint64:
		return fmt.Sprint(x), "!!int", true
	case float64:
		return fmt.Sprint(x), "!!float", true
	}

	return "", "", false
}
