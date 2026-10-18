package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"

	"go.yaml.in/yaml/v4"
)

// CRDManifest is the part of a CustomResourceDefinition (apiextensions.k8s.io/v1) that Lint
// and a CRD-to-plugin converter read: the group, the names, the scope and, per version, its
// schema, printer columns and subresources. Anything else in the manifest is ignored.
type CRDManifest struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   CRDMetadata `yaml:"metadata" json:"metadata"`
	Spec       CRDSpec     `yaml:"spec" json:"spec"`
}

// CRDMetadata is the CRD's name (<plural>.<group>).
type CRDMetadata struct {
	Name string `yaml:"name" json:"name"`
}

// CRDSpec is a CRD's spec.
type CRDSpec struct {
	Group    string       `yaml:"group" json:"group"`
	Names    CRDNames     `yaml:"names" json:"names"`
	Scope    string       `yaml:"scope" json:"scope"`
	Versions []CRDVersion `yaml:"versions" json:"versions"`
}

// CRDNames are the resource's names.
type CRDNames struct {
	Plural     string   `yaml:"plural" json:"plural"`
	Singular   string   `yaml:"singular" json:"singular"`
	Kind       string   `yaml:"kind" json:"kind"`
	ListKind   string   `yaml:"listKind" json:"listKind"`
	ShortNames []string `yaml:"shortNames" json:"shortNames"`
	Categories []string `yaml:"categories" json:"categories"`
}

// CRDVersion is one version the CRD declares.
type CRDVersion struct {
	Name                     string             `yaml:"name" json:"name"`
	Served                   bool               `yaml:"served" json:"served"`
	Storage                  bool               `yaml:"storage" json:"storage"`
	Deprecated               bool               `yaml:"deprecated" json:"deprecated"`
	Schema                   *CRDSchema         `yaml:"schema" json:"schema"`
	AdditionalPrinterColumns []CRDPrinterColumn `yaml:"additionalPrinterColumns" json:"additionalPrinterColumns"`
	Subresources             *CRDSubresources   `yaml:"subresources" json:"subresources"`
}

// CRDSchema holds the version's OpenAPI v3 schema, kept as decoded.
type CRDSchema struct {
	OpenAPIV3Schema map[string]any `yaml:"openAPIV3Schema" json:"openAPIV3Schema"`
}

// CRDPrinterColumn is a column kubectl get shows (and the server-side Table returns).
type CRDPrinterColumn struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"`
	Format      string `yaml:"format" json:"format"`
	Description string `yaml:"description" json:"description"`
	Priority    int    `yaml:"priority" json:"priority"`
	JSONPath    string `yaml:"jsonPath" json:"jsonPath"`
}

// CRDSubresources are the status and scale subresources; nil when absent.
type CRDSubresources struct {
	Status map[string]any `yaml:"status" json:"status"`
	Scale  *CRDScale      `yaml:"scale" json:"scale"`
}

// CRDScale is the scale subresource's paths.
type CRDScale struct {
	SpecReplicasPath   string `yaml:"specReplicasPath" json:"specReplicasPath"`
	StatusReplicasPath string `yaml:"statusReplicasPath" json:"statusReplicasPath"`
	LabelSelectorPath  string `yaml:"labelSelectorPath" json:"labelSelectorPath"`
}

// LoadCRD reads a CRD manifest in YAML or JSON (kubectl get crd NAME -o yaml, or an upstream
// config/crd/bases file). A file of several documents, or a List (kubectl get crd -o yaml),
// gives its first CustomResourceDefinition; other documents are skipped whatever their shape.
func LoadCRD(src []byte) (*CRDManifest, error) {
	if len(src) > 4*MaxSize {
		return nil, errors.New("the CRD manifest is larger than 4 MiB")
	}

	dec, err := yaml.NewLoader(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}

	for {
		var doc yaml.Node

		err := dec.Load(&doc)
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no CustomResourceDefinition in the manifest")
		}

		if err != nil {
			return nil, fmt.Errorf("the CRD manifest does not parse: %w", err)
		}

		crd, err := crdOf(&doc, true)
		if err != nil || crd != nil {
			return crd, err
		}
	}
}

// crdHead is what LoadCRD reads of every document before decoding it.
type crdHead struct {
	Kind  string      `yaml:"kind"`
	Items []yaml.Node `yaml:"items"`
}

// crdOf decodes n when it is a CustomResourceDefinition, or the first one of a List (when
// list); nil for anything else.
func crdOf(n *yaml.Node, list bool) (*CRDManifest, error) {
	var head crdHead
	if err := n.Load(&head); err != nil {
		return nil, nil //nolint:nilerr // not a CRD: kind or items of another shape
	}

	switch {
	case head.Kind == "CustomResourceDefinition":
		var crd CRDManifest
		if err := n.Load(&crd); err != nil {
			return nil, fmt.Errorf("the CustomResourceDefinition does not decode: %w", err)
		}

		if err := crd.check(); err != nil {
			return nil, err
		}

		return &crd, nil
	case list && (head.Kind == "List" || head.Kind == "CustomResourceDefinitionList"):
		for i := range head.Items {
			if crd, err := crdOf(&head.Items[i], false); err != nil || crd != nil {
				return crd, err
			}
		}
	}

	return nil, nil
}

func (c *CRDManifest) check() error {
	switch {
	case c.Spec.Group == "":
		return errors.New("the CRD has no spec.group")
	case c.Spec.Names.Plural == "" || c.Spec.Names.Kind == "":
		return errors.New("the CRD has no spec.names.plural or spec.names.kind")
	case len(c.Spec.Versions) == 0:
		return errors.New("the CRD has no spec.versions")
	}

	return nil
}

// Version is the version named name, nil when the CRD has none.
func (c *CRDManifest) Version(name string) *CRDVersion {
	for i := range c.Spec.Versions {
		if c.Spec.Versions[i].Name == name {
			return &c.Spec.Versions[i]
		}
	}

	return nil
}

// StorageVersion is the version with storage: true, else the last served one, else the last.
func (c *CRDManifest) StorageVersion() *CRDVersion {
	vs := c.Spec.Versions
	if i := slices.IndexFunc(vs, func(v CRDVersion) bool { return v.Storage }); i >= 0 {
		return &vs[i]
	}

	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].Served {
			return &vs[i]
		}
	}

	return &vs[len(vs)-1]
}

// Property is the schema node at path under openAPIV3Schema (each step a key of
// properties; "[]" steps into items), nil when absent.
func (v *CRDVersion) Property(path ...string) map[string]any {
	if v.Schema == nil {
		return nil
	}

	node := v.Schema.OpenAPIV3Schema
	for _, step := range path {
		if node == nil {
			return nil
		}

		if step == "[]" {
			node, _ = node["items"].(map[string]any) //nolint:errcheck // nil when absent
		} else {
			props, _ := node["properties"].(map[string]any) //nolint:errcheck // nil when absent
			node, _ = props[step].(map[string]any)          //nolint:errcheck // nil when absent
		}
	}

	return node
}

// ConditionTypes are the condition types the schema names: the enum of
// status.conditions[].type, when the CRD declares one.
func (v *CRDVersion) ConditionTypes() []string {
	var out []string

	enum, _ := v.Property("status", "conditions", "[]", "type")["enum"].([]any) //nolint:errcheck // nil when absent
	for _, e := range enum {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}

	return out
}
