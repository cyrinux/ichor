package plugin

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// yaml writes the draft as a node tree, keys in kube-green.yaml's order.
func (d *draft) yaml() ([]byte, error) {
	spec := d.crd.Spec
	o := d.o

	root := yMapping(
		yPair("schemaVersion", yPlain("ichor.dev/plugin/v1alpha1")),
		yPair("id", yPlain(o.ID)),
		yPair("name", yPlain(o.Name)),
	)
	if o.Website != "" {
		root.Content = append(root.Content, yPair("website", yPlain(o.Website))...)
	}

	root.Content = append(root.Content, yPair("category", yPlain(o.Category))...)
	root.Content = append(root.Content, yPair("detect", ySequence(yMapping(yPair("group", yPlain(spec.Group)), yPair("resource", yPlain(spec.Names.Plural)))))...)
	root.Content = append(root.Content, yPair("resources", yMapping(yPair(d.key, yMapping(
		yPair("group", yPlain(spec.Group)),
		yPair("version", yPlain(d.v.Name)),
		yPair("resource", yPlain(spec.Names.Plural)),
		yPair("kind", yPlain(spec.Names.Kind)),
		yPair("scope", yPlain(d.scope())),
	))))...)

	if len(d.fields) > 0 {
		fields := yMapping()
		for _, f := range d.fields {
			kv := yPair(f.name, yExpr(f.expr))
			kv[0].HeadComment = yComment(f.comment)
			fields.Content = append(fields.Content, kv...)
		}

		root.Content = append(root.Content, yPair("fields", yMapping(yPair(d.key, fields)))...)
	}

	if len(d.rules) > 0 {
		rules := ySequence()
		for _, r := range d.rules {
			rules.Content = append(rules.Content, yMapping(
				yPair("when", yExpr(r.when)), yPair("health", yPlain(r.health)),
				yPair("reason", yPlain(r.reason)), yPair("message", yText(r.message)),
			))
		}

		root.Content = append(root.Content, yPair("health", yMapping(yPair(d.key, rules)))...)
	}

	root.Content = append(root.Content, yPair("dataServices", yMapping(yPair("summary", yMapping(
		yPair("aggregate", yMapping(
			yPair("total", yExpr("count("+d.key+")")),
			yPair("warnings", yExpr("count("+d.key+`, health == "warning")`)),
			yPair("critical", yExpr("count("+d.key+`, health == "critical")`)),
		)),
		yPair("health", yExpr("worst("+d.key+".health)")),
	))))...)

	root.Content = append(root.Content, yPair("cards", d.cards())...)

	if d.scale {
		root.Content = append(root.Content, yPair("actions", yMapping(yPair("scale", d.scaleAction())))...)
	}

	if f := d.findings(); len(f.Content) > 0 {
		root.Content = append(root.Content, yPair("findings", f)...)
	}

	samples, untriggered := d.samples()
	key := yScalar("samples")

	key.HeadComment = "# Invented names. The first sample is healthy, the second fails."
	if len(untriggered) > 0 {
		var whens []string
		for _, r := range untriggered {
			whens = append(whens, r.health+" ("+r.when+")")
		}

		key.HeadComment += "\n# Rules without a sample: " + strings.Join(whens, "; ") + "."
	}

	seq := ySequence()
	for _, s := range samples {
		seq.Content = append(seq.Content, yValueNode(s))
	}

	root.Content = append(root.Content, key, yMapping(yPair(d.key, seq)))

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	doc.HeadComment = "# yaml-language-server: $schema=../schema/plugin.schema.json\n" +
		fmt.Sprintf("# Draft written by probe plugin-from-crd from %s (%s). Review the health rules, translate the labels (i18n), then move it to go/plugin/builtin/.", d.crd.Metadata.Name, d.v.Name)

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(doc); err != nil {
		return nil, err
	}

	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (d *draft) scope() string {
	if d.namespaced {
		return "Namespaced"
	}

	return "Cluster"
}

func (d *draft) cards() *yaml.Node {
	kind := d.crd.Spec.Names.Kind
	summary := yMapping(
		yPair("title", yMapping(yPair("count", yExpr("dataServices.summary.total")), yPair("one", yText("%d "+kind)), yPair("other", yText("%d "+kind+"s")))),
		yPair("health", yExpr("dataServices.summary.health")),
		yPair("badges", ySequence(
			yMapping(yPair("label", yText("warnings")), yPair("value", yExpr("dataServices.summary.warnings")), yPair("tone", yPlain("warning")), yPair("hideWhenZero", yBoolean(true))),
			yMapping(yPair("label", yText("critical")), yPair("value", yExpr("dataServices.summary.critical")), yPair("tone", yPlain("critical")), yPair("hideWhenZero", yBoolean(true))),
		)),
	)

	sort := ySequence(yPlain("health desc"))
	if d.namespaced {
		sort.Content = append(sort.Content, yExpr("$.metadata.namespace"))
	}

	sort.Content = append(sort.Content, yExpr("$.metadata.name"))

	row := yMapping(yPair("title", yExpr("$.metadata.name")))
	if d.namespaced {
		row.Content = append(row.Content, yPair("subtitle", yExpr("$.metadata.namespace"))...)
	}

	status := yMapping(yPair("tone", yPlain("health")))
	if d.row.status != "" {
		status.Content = append(status.Content, yPair("text", yExpr(d.row.status))...)
	}

	row.Content = append(row.Content, yPair("status", status)...)

	if d.row.trailing != "" {
		row.Content = append(row.Content, yPair("trailing", yMapping(yPair("text", yExpr(d.row.trailing))))...)
	}

	detail := ySequence()

	items := d.row.items
	if d.row.status != "" {
		items = append([][2]string{{d.row.statusLabel, d.row.status}}, items...)
	}

	if len(items) > 0 {
		kv := ySequence()
		for _, it := range items {
			kv.Content = append(kv.Content, yMapping(yPair("label", yText(it[0])), yPair("value", yExpr(it[1]))))
		}

		detail.Content = append(detail.Content, yMapping(yPair("type", yPlain("keyValues")), yPair("items", kv)))
	}

	if d.hasConds {
		detail.Content = append(detail.Content, yMapping(yPair("type", yPlain("conditions"))))
	}

	if len(detail.Content) > 0 {
		row.Content = append(row.Content, yPair("detail", detail)...)
	}

	list := yMapping(
		yPair("from", yPlain(d.key)),
		yPair("sort", sort),
		yPair("empty", yText("No "+kind+"s.")),
		yPair("row", row),
	)

	return yMapping(yPair("summary", summary), yPair("list", list))
}

func (d *draft) scaleAction() *yaml.Node {
	spec := d.crd.Spec

	return yMapping(
		yPair("label", yText("Scale")),
		yPair("on", yPlain(d.key)),
		yPair("verb", yPlain("subresource")),
		yPair("subresource", yPlain("scale")),
		yPair("method", yPlain("PATCH")),
		yPair("body", yMapping(yPair("spec", yMapping(yPair("replicas", yText("{{ params.replicas }}")))))),
		yPair("rbac", yMapping(yPair("verbs", ySequence(yPlain("patch"))), yPair("group", yPlain(spec.Group)), yPair("resource", yPlain(spec.Names.Plural+"/scale")))),
		yPair("confirm", yPlain("plain")),
		yPair("audit", yPlain(d.o.ID+".scale")),
		yPair("params", ySequence(yMapping(yPair("name", yPlain("replicas")), yPair("label", yText("Replicas")), yPair("type", yPlain("int")), yPair("default", yInteger(1))))),
	)
}

// findings: one per severity the rules use (a finding's when reads health, not the reason).
func (d *draft) findings() *yaml.Node {
	out := ySequence()

	for _, sev := range []string{"critical", "warning"} {
		if !slices.ContainsFunc(d.rules, func(r draftRule) bool { return r.health == sev }) {
			continue
		}

		where := "{{ namespace }}/{{ name }}: {{ message }}"
		if !d.namespaced {
			where = "{{ name }}: {{ message }}"
		}

		out.Content = append(out.Content, yMapping(
			yPair("id", yPlain(d.o.ID+"-"+sev)),
			yPair("from", yPlain(d.key)),
			yPair("when", yExpr(`health == "`+sev+`"`)),
			yPair("severity", yPlain(sev)),
			yPair("title", yText("{{ name }}: {{ message }}")),
			yPair("detail", yText(where)),
			yPair("opens", yPlain("list")),
		))
	}

	return out
}

// Node helpers: the tree is built by hand so that the keys keep their order and the
// descriptions their comments.

func yScalar(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

func yPair(key string, value *yaml.Node) []*yaml.Node { return []*yaml.Node{yScalar(key), value} }

func yMapping(pairs ...[]*yaml.Node) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, p := range pairs {
		n.Content = append(n.Content, p...)
	}

	return n
}

func ySequence(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}
}

// plain is a word the encoder may leave unquoted (it quotes one that would read as another type).
func yPlain(s string) *yaml.Node { return yScalar(s) }

// text is a label or a template, always double-quoted, as the built-in plugins write them.
func yText(s string) *yaml.Node {
	n := yScalar(s)
	n.Style = yaml.DoubleQuotedStyle

	return n
}

// expr is an expression: single-quoted when it holds quotes, ": " or starts with a YAML
// indicator (docs/plugins.md "Writing expressions in YAML"), else plain.
func yExpr(s string) *yaml.Node {
	n := yScalar(s)
	if s == "" || strings.ContainsAny(s, `"'`) || strings.Contains(s, ": ") || strings.Contains(s, " #") || strings.ContainsRune("!&*{}[]|>%@`#,?:-", rune(s[0])) {
		n.Style = yaml.SingleQuotedStyle
	}

	return n
}

func yBoolean(b bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(b)}
}

func yInteger(i int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(i)}
}

func yComment(s string) string {
	if s == "" {
		return ""
	}

	return "# " + s
}

// valueNode writes a sample value; map keys sorted, metadata first.
func yValueNode(v any) *yaml.Node {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}

		slices.SortFunc(keys, func(a, b string) int {
			if (a == "metadata") != (b == "metadata") {
				if a == "metadata" {
					return -1
				}

				return 1
			}

			return strings.Compare(a, b)
		})

		n := yMapping()
		for _, k := range keys {
			n.Content = append(n.Content, yPair(k, yValueNode(x[k]))...)
		}

		return n
	case []any:
		n := ySequence()
		for _, item := range x {
			n.Content = append(n.Content, yValueNode(item))
		}

		return n
	case bool:
		return yBoolean(x)
	case int:
		return yInteger(x)
	case float64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: fmt.Sprint(x)}
	case string:
		return yPlain(x)
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	default:
		return yPlain(fmt.Sprint(x))
	}
}
