package ichorgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The OpenAPI v3 schemas behind KubeExplain: the part of a group-version document the help
// reads (components.schemas), the kind's schema found by its x-kubernetes-group-version-kind,
// and the walk down a field path, through array items and map values.

// explainField is the answer of KubeExplain: the field at path and the fields it holds.
type explainField struct {
	Path        string         `json:"path"`
	Type        string         `json:"type"`
	Format      string         `json:"format,omitempty"`
	Description string         `json:"description"`
	Enum        []string       `json:"enum,omitempty"`
	Required    bool           `json:"required"`
	Children    []explainChild `json:"children"`
}

// explainChild is one field of the object at the path, its description cut to the first sentence.
type explainChild struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Enum        []string `json:"enum,omitempty"`
}

// oaSchema keeps what the help shows of an OpenAPI v3 schema; the rest of the document
// (defaults, validations, examples) is not decoded.
type oaSchema struct {
	Type        string               `json:"type"`
	Format      string               `json:"format"`
	Description string               `json:"description"`
	Enum        []any                `json:"enum"`
	Required    []string             `json:"required"`
	Properties  map[string]*oaSchema `json:"properties"`
	Items       *oaSchema            `json:"items"`
	Additional  *oaAdditional        `json:"additionalProperties"`
	Ref         string               `json:"$ref"`
	AllOf       []*oaSchema          `json:"allOf"`
	IntOrString bool                 `json:"x-kubernetes-int-or-string"`
	FreeForm    bool                 `json:"x-kubernetes-preserve-unknown-fields"`
	GVK         []oaGVK              `json:"x-kubernetes-group-version-kind"`
}

type oaGVK struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// oaAdditional is additionalProperties: a schema, or a boolean (no schema).
type oaAdditional struct {
	schema *oaSchema
}

func (a *oaAdditional) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] != '{' {
		return nil
	}

	a.schema = &oaSchema{}

	return json.Unmarshal(data, a.schema)
}

// explainDoc is a decoded group-version document: its schemas and the kinds they describe.
type explainDoc struct {
	schemas map[string]*oaSchema
	kinds   map[oaGVK]string
}

// oaDocument is the part of an OpenAPI v3 document decoded: components.schemas.
type oaDocument struct {
	Components struct {
		Schemas map[string]*oaSchema `json:"schemas"`
	} `json:"components"`
}

// decodeExplainDoc decodes an OpenAPI v3 document (the demo's).
func decodeExplainDoc(data []byte) (*explainDoc, error) {
	var raw oaDocument
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode OpenAPI v3 document: %w", err)
	}

	return newExplainDoc(raw), nil
}

// newExplainDoc indexes the schemas of raw by the kinds they describe.
func newExplainDoc(raw oaDocument) *explainDoc {
	doc := &explainDoc{schemas: raw.Components.Schemas, kinds: map[oaGVK]string{}}

	for name, s := range doc.schemas {
		if s == nil {
			continue
		}

		for _, gvk := range s.GVK {
			doc.kinds[gvk] = name
		}
		// Only the kind's own schema needs them: the walk never reads them again.
		s.GVK = nil
	}

	return doc
}

const oaRefPrefix = "#/components/schemas/"

// resolve follows s's $ref and merges its allOf one level deep: what the field says of
// itself (description, enum) wins over the schema it points to. name is the schema pointed
// to, "" for an inline one.
func (d *explainDoc) resolve(s *oaSchema) (merged *oaSchema, name string) {
	if s == nil {
		return &oaSchema{}, ""
	}

	out := *s
	parts := s.AllOf

	if s.Ref != "" {
		parts = append([]*oaSchema{{Ref: s.Ref}}, parts...)
	}

	for _, part := range parts {
		target := part

		if part != nil && part.Ref != "" {
			name = strings.TrimPrefix(part.Ref, oaRefPrefix)
			target = d.schemas[name]
		}

		if target != nil {
			out = mergeSchema(out, *target)
		}
	}

	out.Ref, out.AllOf = "", nil

	return &out, name
}

// mergeSchema fills what own leaves empty from base.
func mergeSchema(own, base oaSchema) oaSchema {
	if own.Type == "" {
		own.Type = base.Type
	}

	if own.Format == "" {
		own.Format = base.Format
	}

	if own.Description == "" {
		own.Description = base.Description
	}

	if own.Enum == nil {
		own.Enum = base.Enum
	}

	if own.Required == nil {
		own.Required = base.Required
	}

	if own.Properties == nil {
		own.Properties = base.Properties
	}

	if own.Items == nil {
		own.Items = base.Items
	}

	if own.Additional == nil {
		own.Additional = base.Additional
	}

	own.IntOrString = own.IntOrString || base.IntOrString
	own.FreeForm = own.FreeForm || base.FreeForm

	return own
}

// isMap tells a map[string]T (additionalProperties, no properties) from an object.
func (s *oaSchema) isMap() bool {
	return len(s.Properties) == 0 && s.Additional != nil && s.Additional.schema != nil
}

// typeName is how kubectl explain names a type: "string", "[]Container",
// "map[string]string", "PodSpec", "int-or-string".
func (d *explainDoc) typeName(s *oaSchema, name string) string {
	switch {
	case s.IntOrString || s.Format == "int-or-string":
		return "int-or-string"
	case s.Type == "array":
		items, itemName := d.resolve(s.Items)

		return "[]" + d.typeName(items, itemName)
	case s.isMap():
		value, valueName := d.resolve(s.Additional.schema)

		return "map[string]" + d.typeName(value, valueName)
	case name != "":
		return name[strings.LastIndex(name, ".")+1:]
	case s.Type != "":
		return s.Type
	default:
		return "Object"
	}
}

// container is what holds s's fields: an array's items, a map's values, else s itself.
func (d *explainDoc) container(s *oaSchema) *oaSchema {
	for range 8 {
		switch {
		case s.Type == "array" && s.Items != nil:
			s, _ = d.resolve(s.Items)
		case s.isMap():
			s, _ = d.resolve(s.Additional.schema)
		default:
			return s
		}
	}

	return s
}

// explain walks fieldPath (dot-separated) down the kind's schema and describes where it
// lands. A segment below a map is a key of the map; arrays are walked through their items.
func (d *explainDoc) explain(group, version, kind string, segments []string) (explainField, error) {
	name, ok := d.kinds[oaGVK{group, version, kind}]
	if !ok {
		return explainField{}, fmt.Errorf("%s: no schema for %s in %s", explainUnavailablePrefix, kind, groupVersion(group, version))
	}

	node, _ := d.resolve(d.schemas[name])
	nodeName := name
	required := false

	for i, seg := range segments {
		holder := node
		for range 8 {
			if holder.Type == "array" && holder.Items != nil {
				holder, _ = d.resolve(holder.Items)

				continue
			}

			break
		}

		if holder.isMap() {
			node, nodeName = d.resolve(holder.Additional.schema)
			required = false

			// A key with dots ("app.kubernetes.io/name") was split: its value is a scalar.
			if len(node.Properties) == 0 && node.Type != "array" && !node.isMap() {
				break
			}

			continue
		}

		prop, ok := holder.Properties[seg]
		if !ok {
			path := strings.Join(segments[:i+1], ".")
			if holder.FreeForm {
				return explainField{}, fmt.Errorf("%s accepts any field: its schema does not describe %s", kind, path)
			}

			return explainField{}, fmt.Errorf("%s has no field %s", kind, path)
		}

		required = slices.Contains(holder.Required, seg)
		node, nodeName = d.resolve(prop)
	}

	return explainField{
		Path:        strings.Join(segments, "."),
		Type:        d.typeName(node, nodeName),
		Format:      node.Format,
		Description: strings.TrimSpace(node.Description),
		Enum:        enumStrings(node.Enum),
		Required:    required,
		Children:    d.children(d.container(node)),
	}, nil
}

func (d *explainDoc) children(s *oaSchema) []explainChild {
	out := make([]explainChild, 0, len(s.Properties))

	for name, prop := range s.Properties {
		child, childName := d.resolve(prop)
		out = append(out, explainChild{
			Name:        name,
			Type:        d.typeName(child, childName),
			Description: firstSentence(child.Description),
			Required:    slices.Contains(s.Required, name),
			Enum:        enumStrings(child.Enum),
		})
	}

	slices.SortFunc(out, func(a, b explainChild) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// firstSentence cuts a description to its first sentence, or paragraph.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "\n\n"); i >= 0 {
		text = text[:i]
	}

	if i := strings.Index(text, ". "); i >= 0 {
		text = text[:i+1]
	}

	return strings.Join(strings.Fields(text), " ")
}

// splitFieldPath splits "spec.template.spec.containers[0].image" into its field names,
// dropping list indexes.
func splitFieldPath(path string) []string {
	var out []string

	for seg := range strings.SplitSeq(strings.TrimSpace(path), ".") {
		if i := strings.IndexByte(seg, '['); i >= 0 {
			seg = seg[:i]
		}

		seg = strings.TrimSpace(seg)
		if seg != "" {
			out = append(out, seg)
		}
	}

	return out
}

func groupVersion(group, version string) string {
	if group == "" {
		return version
	}

	return group + "/" + version
}
