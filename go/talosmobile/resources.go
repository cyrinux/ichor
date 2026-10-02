package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/state"
	"go.yaml.in/yaml/v4"
)

const maxResourceItems = 1000

type resourceTypeInfo struct {
	Type        string   `json:"type"`
	Aliases     []string `json:"aliases"`
	Namespace   string   `json:"namespace"`   // default namespace
	Sensitivity string   `json:"sensitivity"` // "" | "sensitive"
}

type resourceItems struct {
	Type      string         `json:"type"`      // canonical type
	Namespace string         `json:"namespace"` // namespace listed
	Items     []resourceItem `json:"items"`
	Truncated bool           `json:"truncated"`
}

type resourceItem struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Version   string `json:"version"`
	Phase     string `json:"phase"`
	Updated   int64  `json:"updated"` // unix ms, 0 when unknown
}

type resourceYAML struct {
	YAML string `json:"yaml"`
}

// ResourceTypes lists the resource types node knows, from its resource definitions, like
// `talosctl get rd` (os:reader). Sensitive types can only be read with os:admin.
func ResourceTypes(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ResourceTypes", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		defs, err := s.resourceDefinitions(ctx, node)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		return toJSON(mapResourceTypes(defs))
	})
}

func mapResourceTypes(defs []*meta.ResourceDefinition) []resourceTypeInfo {
	out := make([]resourceTypeInfo, 0, len(defs))

	for _, rd := range defs {
		spec := rd.TypedSpec()

		out = append(out, resourceTypeInfo{
			Type:        spec.Type,
			Aliases:     uniqueAliases(spec.Aliases),
			Namespace:   spec.DefaultNamespace,
			Sensitivity: sensitivityName(spec),
		})
	}

	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Type) < strings.ToLower(out[j].Type) })

	return out
}

func uniqueAliases(aliases []string) []string {
	out := make([]string, 0, len(aliases))

	for _, a := range aliases {
		out = appendUnique(out, a)
	}

	return out
}

func sensitivityName(spec *meta.ResourceDefinitionSpec) string {
	if spec.Sensitivity == meta.Sensitive {
		return "sensitive"
	}

	return ""
}

// ResourceList lists the resources of a type on node, like `talosctl get TYPE` (os:reader;
// os:admin for sensitive types, enforced by the node). resourceType is a canonical type or
// an alias; an empty namespace means the type's default one. At most 1000 items.
func ResourceList(configYAML, contextName, node, namespace, resourceType string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ResourceList", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		rd, err := s.resolveResourceType(ctx, node, resourceType)
		if err != nil {
			return "", err
		}

		if namespace == "" {
			namespace = rd.DefaultNamespace
		}

		list, err := s.client.COSI.List(withNode(ctx, node), resource.NewMetadata(namespace, rd.Type, "", resource.VersionUndefined))
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		return toJSON(mapResourceItems(rd.Type, namespace, list.Items))
	})
}

func mapResourceItems(resourceType, namespace string, items []resource.Resource) resourceItems {
	out := resourceItems{Type: resourceType, Namespace: namespace, Items: make([]resourceItem, 0, len(items))}

	for _, r := range items {
		md := r.Metadata()
		item := resourceItem{ID: md.ID(), Namespace: md.Namespace(), Version: md.Version().String(), Phase: md.Phase().String()}

		if !md.Updated().IsZero() {
			item.Updated = md.Updated().UnixMilli()
		}

		out.Items = append(out.Items, item)
	}

	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].ID < out.Items[j].ID })

	if len(out.Items) > maxResourceItems {
		out.Items, out.Truncated = out.Items[:maxResourceItems], true
	}

	return out
}

// ResourceGet returns one resource of node as YAML (metadata and spec), like
// `talosctl get TYPE ID -o yaml` (os:reader; os:admin for sensitive types, enforced by the
// node). Sensitive resources hold secrets (keys, tokens, the machine config): the app must
// warn before showing them.
func ResourceGet(configYAML, contextName, node, namespace, resourceType, id string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ResourceGet", configYAML, contextName, node)
	}
	id = privacy.unmaskText(id)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		rd, err := s.resolveResourceType(ctx, node, resourceType)
		if err != nil {
			return "", err
		}

		if namespace == "" {
			namespace = rd.DefaultNamespace
		}

		r, err := s.client.COSI.Get(withNode(ctx, node), resource.NewMetadata(namespace, rd.Type, id, resource.VersionUndefined))
		if err != nil {
			if state.IsNotFoundError(err) {
				return "", fmt.Errorf("%s %q not found in namespace %s", rd.Type, id, namespace)
			}

			return "", errors.New(s.friendly(node, err))
		}

		text, err := resourceToYAML(r)
		if err != nil {
			return "", err
		}

		return toJSON(resourceYAML{YAML: text})
	})
}

// resolveResourceType finds the definition of a canonical type or alias (case-insensitive).
func (s *session) resolveResourceType(ctx context.Context, node, resourceType string) (*meta.ResourceDefinitionSpec, error) {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return nil, errors.New("no resource type given")
	}

	defs, err := s.resourceDefinitions(ctx, node)
	if err != nil {
		return nil, errors.New(s.friendly(node, err))
	}

	rd, err := matchResourceType(defs, resourceType)
	if err != nil {
		return nil, err
	}

	if rd == nil {
		return nil, fmt.Errorf("resource type %q: %s", resourceType, notAvailableOn(s.nodeVersion(ctx, node)))
	}

	return rd, nil
}

// matchResourceType returns nil when no definition matches: the node's Talos version does
// not have the type.
func matchResourceType(defs []*meta.ResourceDefinition, resourceType string) (*meta.ResourceDefinitionSpec, error) {
	var matched []*meta.ResourceDefinitionSpec

	for _, rd := range defs {
		spec := rd.TypedSpec()

		if strings.EqualFold(spec.Type, resourceType) {
			return spec, nil
		}

		for _, alias := range spec.AllAliases {
			if strings.EqualFold(alias, resourceType) {
				matched = append(matched, spec)

				break
			}
		}
	}

	switch len(matched) {
	case 0:
		return nil, nil //nolint:nilnil
	case 1:
		return matched[0], nil
	default:
		types := make([]string, len(matched))
		for i, m := range matched {
			types[i] = m.Type
		}

		sort.Strings(types)

		return nil, fmt.Errorf("resource type %q is ambiguous: %s", resourceType, strings.Join(types, ", "))
	}
}

// resourceToYAML renders a resource like `talosctl get -o yaml`: it works for the types
// this client knows and for newer ones, which carry their spec as YAML.
func resourceToYAML(r resource.Resource) (string, error) {
	doc, err := resource.MarshalYAML(r)
	if err != nil {
		return "", fmt.Errorf("encode resource: %w", err)
	}

	b, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("encode resource: %w", err)
	}

	return string(b), nil
}

// resourceSpecMap decodes a resource's spec into a generic map, for types this client has no
// Go definition of.
func resourceSpecMap(r resource.Resource) (map[string]any, error) {
	b, err := yaml.Marshal(r.Spec())
	if err != nil {
		return nil, fmt.Errorf("encode spec: %w", err)
	}

	spec := map[string]any{}
	if err := yaml.Unmarshal(b, &spec); err != nil {
		return nil, fmt.Errorf("decode spec: %w", err)
	}

	return spec, nil
}
