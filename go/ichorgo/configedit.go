package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

const (
	configOpSet    = "set"
	configOpAdd    = "add"
	configOpRemove = "remove"
)

// configEdit is one change to a draft: set replaces the scalar at Path, add puts a new value
// in the object (under Key) or at the end of the list at Path, remove deletes what is at Path.
type configEdit struct {
	Doc   int      `json:"doc"`
	Path  []string `json:"path"`
	Op    string   `json:"op"`
	Key   string   `json:"key"`
	Type  string   `json:"type"`
	Value string   `json:"value"`
}

// MachineConfigEdit applies one field edit to a machine config draft and returns the new
// draft YAML. editJSON is {"doc","path","op","key","type","value"}: doc and path as given by
// MachineConfigDescribe; op is "set" (replace the scalar at path), "add" (a new "key" in the
// object at path, or a new item at the end of the list at path) or "remove"; type is the
// type of the new value (object and array start empty; "any" reads value as YAML). Hidden
// secrets cannot be set. Nothing is sent to the node.
func MachineConfigEdit(draftYAML, editJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var edit configEdit
	if err := json.Unmarshal([]byte(editJSON), &edit); err != nil {
		return "", fmt.Errorf("invalid edit: %w", err)
	}

	docs, err := parseConfigDocs(draftYAML)
	if err != nil {
		return "", fmt.Errorf("the draft is not valid YAML: %w", err)
	}

	if edit.Doc < 0 || edit.Doc >= len(docs) {
		return "", fmt.Errorf("no document %d in this config", edit.Doc)
	}

	if err := applyConfigEdit(docs[edit.Doc], edit); err != nil {
		return "", err
	}

	return encodeConfigDocs(docs)
}

func applyConfigEdit(root *yaml.Node, edit configEdit) error {
	parent, target, index, err := findConfigNode(root, edit.Path)
	if err != nil {
		return err
	}

	switch edit.Op {
	case configOpSet:
		return setConfigValue(target, edit)
	case configOpAdd:
		return addConfigValue(target, edit)
	case configOpRemove:
		return removeConfigValue(parent, index)
	default:
		return fmt.Errorf("unknown edit %q (set, add, remove)", edit.Op)
	}
}

// findConfigNode walks path from root: the node found, its parent (nil for the root) and
// the node's index in parent.Content.
func findConfigNode(root *yaml.Node, path []string) (parent, node *yaml.Node, index int, err error) {
	node = root

	for i, seg := range path {
		next := -1

		switch node.Kind {
		case yaml.MappingNode:
			for j := 0; j+1 < len(node.Content); j += 2 {
				if node.Content[j].Value == seg {
					next = j + 1

					break
				}
			}
		case yaml.SequenceNode:
			if n, convErr := strconv.Atoi(seg); convErr == nil && n >= 0 && n < len(node.Content) {
				next = n
			}
		}

		if next < 0 {
			return nil, nil, 0, fmt.Errorf("no field %s in this config", strings.Join(path[:i+1], "."))
		}

		parent, node, index = node, node.Content[next], next
	}

	return parent, node, index, nil
}

func setConfigValue(target *yaml.Node, edit configEdit) error {
	if target.Kind != yaml.ScalarNode {
		return errors.New("only a single value can be set; add or remove fields instead")
	}

	if target.Value == redacted {
		return errors.New("a hidden secret cannot be changed here")
	}

	value, err := newConfigValue(edit.Type, edit.Value)
	if err != nil {
		return err
	}

	*target = *value

	return nil
}

func addConfigValue(target *yaml.Node, edit configEdit) error {
	value, err := newConfigValue(edit.Type, edit.Value)
	if err != nil {
		return err
	}

	// "key:" with nothing after it is null: it becomes the container the addition needs.
	if target.Kind == yaml.ScalarNode && target.ShortTag() == "!!null" {
		if edit.Key != "" {
			*target = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		} else {
			*target = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		}
	}

	switch target.Kind {
	case yaml.MappingNode:
		key := strings.TrimSpace(edit.Key)
		if key == "" {
			return errors.New("a name is needed for the new field")
		}

		if mappingValue(target, key) != nil {
			return fmt.Errorf("the field %s already exists", key)
		}

		target.Content = append(target.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	case yaml.SequenceNode:
		target.Content = append(target.Content, value)
	default:
		return errors.New("fields can only be added to an object or a list")
	}

	return nil
}

func removeConfigValue(parent *yaml.Node, index int) error {
	if parent == nil {
		return errors.New("a whole document cannot be removed here; use the YAML editor")
	}

	switch parent.Kind {
	case yaml.MappingNode:
		// index is the value's: its key sits just before.
		parent.Content = append(parent.Content[:index-1:index-1], parent.Content[index+1:]...)
	case yaml.SequenceNode:
		parent.Content = append(parent.Content[:index:index], parent.Content[index+1:]...)
	}

	return nil
}

// newConfigValue builds the YAML node of a value typed in the app.
func newConfigValue(typ, value string) (*yaml.Node, error) {
	if value == redacted {
		return nil, errors.New("this value is the mask of a hidden secret")
	}

	switch typ {
	case configTypeObject:
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	case configTypeArray:
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}, nil
	case configTypeString:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}, nil
	case configTypeBoolean:
		if value != "true" && value != "false" {
			return nil, fmt.Errorf("%q is not true or false", value)
		}

		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: value}, nil
	case configTypeInteger:
		// Written back in base 10: YAML would read "010" as octal.
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", value)
		}

		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(n, 10)}, nil
	case configTypeNumber:
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%q is not a number", value)
		}

		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(f, 'g', -1, 64)}, nil
	}

	// Any type: what YAML reads, so true, 5 and text all work.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(value), &doc); err != nil || len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}, nil
	}

	if doc.Content[0].Kind != yaml.ScalarNode {
		return nil, errors.New("only a single value can be typed here; use the YAML editor for more")
	}

	// YAML dropped or folded part of the text (a "#" comment, a line break): it is text.
	if doc.Content[0].Value != strings.TrimSpace(value) || doc.Content[0].Anchor != "" {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}, nil
	}

	return &yaml.Node{Kind: yaml.ScalarNode, Tag: doc.Content[0].Tag, Value: doc.Content[0].Value}, nil
}
