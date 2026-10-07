package ichorgo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// The machine config as a tree the apps can show field by field: each YAML document, each
// key with its value, and what the schema says about it (type, documentation, allowed
// values, the fields that could be added).

const (
	configTypeObject  = "object"
	configTypeArray   = "array"
	configTypeString  = "string"
	configTypeInteger = "integer"
	configTypeNumber  = "number"
	configTypeBoolean = "boolean"
	configTypeNull    = "null"
	// configTypeAny is a value whose type nothing tells: it is read as YAML.
	configTypeAny = "any"
)

type configTree struct {
	Schema    bool             `json:"schema"` // described with the Talos schema
	Documents []configDocument `json:"documents"`
	Error     *configSyntax    `json:"error,omitempty"`
}

// configSyntax is why the text is not valid YAML (line: 1-based, 0 when unknown).
type configSyntax struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

type configDocument struct {
	Index int         `json:"index"`
	Title string      `json:"title"`
	Node  *configNode `json:"node"`
}

type configNode struct {
	Key         string        `json:"key"`
	Path        []string      `json:"path"` // keys and list indexes from the document root
	Type        string        `json:"type"`
	Value       string        `json:"value,omitempty"` // scalars only
	Title       string        `json:"title,omitempty"`
	Description string        `json:"description,omitempty"`
	Enum        []string      `json:"enum,omitempty"`
	Redacted    bool          `json:"redacted,omitempty"` // a hidden secret: not editable
	Children    []*configNode `json:"children,omitempty"`
	// Addable lists the fields the schema knows that the object does not set yet.
	Addable []configAddable `json:"addable,omitempty"`
	// FreeKeyType is the type of the values an object takes under any key ("" when it only
	// takes its declared fields); ItemType is the type of a list's items.
	FreeKeyType string `json:"freeKeyType,omitempty"`
	ItemType    string `json:"itemType,omitempty"`
}

type configAddable struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// MachineConfigDescribe turns a machine config (the YAML of NodeMachineConfig, or a draft
// being edited) into a tree: {"schema","documents":[{"index","title","node"}],"error"}.
// A node is {"key","path","type","value","title","description","enum","redacted",
// "children","addable","freeKeyType","itemType"}; types are object, array, string, integer,
// number, boolean, null. talosVersion selects the schema made ready by
// MachineConfigSchemaPrepare; without it the tree only has what the YAML tells. It never
// uses the network. Text that is not valid YAML gives "error" ({"line","message"}) and no
// document.
func MachineConfigDescribe(draftYAML, talosVersion string) (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(describeConfig(draftYAML, configSchemas.cached(talosVersion)))
}

func describeConfig(text string, schema *configSchema) configTree {
	tree := configTree{Schema: schema != nil, Documents: []configDocument{}}

	docs, err := parseConfigDocs(text)
	if err != nil {
		tree.Error = configSyntaxOf(err)

		return tree
	}

	for i, root := range docs {
		var def *schemaNode
		if schema != nil {
			def = schema.documentDef(root)
		}

		b := treeBuilder{schema: schema}
		tree.Documents = append(tree.Documents, configDocument{Index: i, Title: configDocTitle(root), Node: b.build(root, def, "", nil)})
	}

	return tree
}

// parseConfigDocs reads the YAML documents of text as mapping nodes; empty documents are
// skipped.
func parseConfigDocs(text string) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(text))

	var docs []*yaml.Node

	for {
		var doc yaml.Node

		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}

		if err != nil {
			return nil, err
		}

		if len(doc.Content) == 0 {
			continue
		}

		root := doc.Content[0]
		if root.Kind == yaml.ScalarNode && root.ShortTag() == "!!null" {
			continue
		}

		if root.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("yaml: line %d: a config document must be a mapping", root.Line)
		}

		docs = append(docs, root)
	}
}

// encodeConfigDocs writes docs back as a multi-document YAML text, indented like Talos does.
func encodeConfigDocs(docs []*yaml.Node) (string, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)

	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return "", fmt.Errorf("encode machine config: %w", err)
		}
	}

	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encode machine config: %w", err)
	}

	return buf.String(), nil
}

// yamlErrorLine finds the line in a YAML error ("line 3", or "L3.C2" since yaml v4).
var yamlErrorLine = regexp.MustCompile(`(?:line |\bL)(\d+)`)

func configSyntaxOf(err error) *configSyntax {
	message := strings.TrimPrefix(strings.TrimPrefix(err.Error(), "yaml: "), "go-yaml ")

	line := 0
	if m := yamlErrorLine.FindStringSubmatch(message); m != nil {
		line, _ = strconv.Atoi(m[1]) //nolint:errcheck // digits
	}

	return &configSyntax{Line: line, Message: message}
}

// configDocKey identifies a config document: its apiVersion, kind and name, or "v1alpha1"
// for the main one, which has no kind.
func configDocKey(root *yaml.Node) string {
	kind := scalarValue(mappingValue(root, "kind"))
	if kind == "" {
		return "v1alpha1"
	}

	return scalarValue(mappingValue(root, "apiVersion")) + "/" + kind + "/" + scalarValue(mappingValue(root, "name"))
}

func configDocTitle(root *yaml.Node) string {
	kind := scalarValue(mappingValue(root, "kind"))
	if kind == "" {
		return "v1alpha1"
	}

	if name := scalarValue(mappingValue(root, "name")); name != "" {
		return kind + " " + name
	}

	return kind
}

func scalarValue(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}

	return n.Value
}

// documentDef is the definition of the document root is: the one whose "kind" allows root's,
// or the main v1alpha1 config, which has a "version" and no kind.
func (s *configSchema) documentDef(root *yaml.Node) *schemaNode {
	kind := scalarValue(mappingValue(root, "kind"))

	for _, alt := range s.OneOf {
		def := s.resolve(alt)
		if def == nil {
			continue
		}

		kinds, hasKind := def.Properties["kind"]

		switch {
		case kind == "" && !hasKind && def.Properties["version"] != nil:
			return def
		case kind != "" && hasKind && slices.Contains(enumStrings(kinds.Enum), kind):
			return def
		}
	}

	return nil
}

func enumStrings(values []any) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprint(v)
	}

	return out
}

type treeBuilder struct {
	schema *configSchema
}

// pick resolves sch for node: a oneOf becomes the alternative of node's kind.
func (b treeBuilder) pick(sch *schemaNode, node *yaml.Node) *schemaNode {
	if b.schema == nil || sch == nil {
		return nil
	}

	sch = b.schema.resolve(sch)
	if sch == nil || len(sch.OneOf) == 0 || sch.Type != "" || len(sch.Properties) > 0 {
		return sch
	}

	want := yamlType(node)

	for _, alt := range sch.OneOf {
		if r := b.schema.resolve(alt); r != nil && (r.Type == want || r.Type == configTypeNumber && want == configTypeInteger) {
			return r
		}
	}

	return sch
}

func (b treeBuilder) build(node *yaml.Node, sch *schemaNode, key string, path []string) *configNode {
	// An alias is shown as written, never followed: a draft is typed by hand, and aliases of
	// aliases would make the tree as large as one likes. Such a draft cannot be applied anyway.
	if node.Kind == yaml.AliasNode {
		return &configNode{Key: key, Path: slices.Clone(path), Type: configTypeString, Value: "*" + node.Value}
	}

	sch = b.pick(sch, node)
	out := &configNode{Key: key, Path: slices.Clone(path), Type: yamlType(node)}

	if out.Path == nil {
		out.Path = []string{}
	}

	if sch != nil {
		out.Description = strings.TrimSpace(sch.Description)
		out.Enum = enumStrings(sch.Enum)

		if sch.Title != key {
			out.Title = sch.Title
		}
	}

	switch node.Kind {
	case yaml.MappingNode:
		b.buildObject(out, node, sch, path)
	case yaml.SequenceNode:
		b.buildArray(out, node, sch, path)
	default:
		out.Value = node.Value
		out.Redacted = node.Value == redacted

		if sch != nil && isScalarType(sch.Type) && out.Type != configTypeNull {
			out.Type = sch.Type
		}
	}

	return out
}

func (b treeBuilder) buildObject(out *configNode, node *yaml.Node, sch *schemaNode, path []string) {
	set := map[string]bool{}

	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i].Value
		set[k] = true

		var child *schemaNode
		if b.schema != nil {
			child = b.schema.property(sch, k)
		}

		out.Children = append(out.Children, b.build(node.Content[i+1], child, k, append(slices.Clone(path), k)))
	}

	if b.schema == nil || sch == nil {
		out.FreeKeyType = configTypeAny

		return
	}

	if b.schema.allowsFreeKeys(sch) {
		out.FreeKeyType = schemaType(b.schema.freeKey(sch))
	}

	for k, p := range sch.Properties {
		if set[k] {
			continue
		}

		p = b.schema.resolve(p)
		if p == nil {
			continue
		}

		out.Addable = append(out.Addable, configAddable{Key: k, Type: schemaType(p), Description: strings.TrimSpace(p.Description), Enum: enumStrings(p.Enum)})
	}

	slices.SortFunc(out.Addable, func(x, y configAddable) int { return strings.Compare(x.Key, y.Key) })
}

func (b treeBuilder) buildArray(out *configNode, node *yaml.Node, sch *schemaNode, path []string) {
	var item *schemaNode
	if sch != nil {
		item = sch.Items
	}

	out.ItemType = configTypeAny
	if b.schema != nil && item != nil {
		out.ItemType = schemaType(b.schema.resolve(item))
	}

	for i, c := range node.Content {
		k := strconv.Itoa(i)
		out.Children = append(out.Children, b.build(c, item, k, append(slices.Clone(path), k)))
	}
}

func isScalarType(t string) bool {
	return t == configTypeString || t == configTypeInteger || t == configTypeNumber || t == configTypeBoolean
}

// schemaType is the type a new value described by sch gets.
func schemaType(sch *schemaNode) string {
	switch {
	case sch == nil:
		return configTypeAny
	case sch.Type == configTypeObject || sch.Type == configTypeArray || isScalarType(sch.Type):
		return sch.Type
	case len(sch.Properties) > 0:
		return configTypeObject
	case len(sch.Enum) > 0:
		return configTypeString
	default:
		return configTypeAny
	}
}

func yamlType(node *yaml.Node) string {
	switch node.Kind {
	case yaml.MappingNode:
		return configTypeObject
	case yaml.SequenceNode:
		return configTypeArray
	}

	switch node.ShortTag() {
	case "!!bool":
		return configTypeBoolean
	case "!!int":
		return configTypeInteger
	case "!!float":
		return configTypeNumber
	case "!!null":
		return configTypeNull
	default:
		return configTypeString
	}
}
