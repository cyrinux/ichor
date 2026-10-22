package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// GenerateOptions are what a CRD does not say about the product; empty ones get defaults.
type GenerateOptions struct {
	// ID is the plugin id: by default the group's first label without its .io, .com, .dev or
	// .org (kube-green.com is kube-green, external-secrets.io is external-secrets).
	ID string
	// Name is the product name; the id by default.
	Name string
	// Website is the project's home page; none by default.
	Website string
	// Category picks the hub; data-services by default.
	Category string
}

// Generate writes a plugin draft for crd, following docs/plugins.md "Writing a plugin from a
// CRD": the same CRD gives the same bytes. The draft validates and passes Lint against the
// CRD, but for its i18n and the health rules no sample triggers (listed in a comment above
// the samples): a person or the /ichor-crd-plugin skill refines it before it becomes a built-in.
func Generate(crd *CRDManifest, o GenerateOptions) ([]byte, error) {
	if crd == nil {
		return nil, errors.New("no CRD")
	}

	if err := crd.check(); err != nil {
		return nil, err
	}

	d, err := newDraft(crd, o)
	if err != nil {
		return nil, err
	}

	return d.yaml()
}

// draft is what Generate decided, before it is written out.
type draft struct {
	crd        *CRDManifest
	v          *CRDVersion
	o          GenerateOptions
	key        string
	namespaced bool
	fields     []draftField
	// phaseField is the field holding status.phase ("" when the schema has no phase enum).
	phaseField string
	phase      phaseValues
	conditions []string
	hasConds   bool
	rules      []draftRule
	row        draftRow
	scale      bool
}

type draftField struct {
	name, expr, comment string
}

type draftRule struct {
	when, health, reason, message string
}

// phaseValues are a status.phase enum's values by the health they mean.
type phaseValues struct {
	critical, info, ok []string
}

type draftRow struct {
	status, statusLabel string
	trailing            string
	items               [][2]string // label, value
}

var (
	identRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	idCleanRe = regexp.MustCompile(`[^a-z0-9-]+`)
	// A printer column path the expressions can read as it is: .a.b.c with plain names.
	plainPathRe = regexp.MustCompile(`^(\.[a-zA-Z][a-zA-Z0-9]*)+$`)
)

func newDraft(crd *CRDManifest, o GenerateOptions) (*draft, error) {
	spec := crd.Spec
	if spec.Names.Plural == "" || spec.Names.Kind == "" {
		return nil, errors.New("the CRD has no spec.names.plural or spec.names.kind")
	}

	o.ID = generatedID(o.ID, spec.Group)
	if o.ID == "" {
		return nil, fmt.Errorf("no plugin id can be made from the group %q: give one", spec.Group)
	}

	if o.Name == "" {
		o.Name = o.ID
	}

	if o.Category == "" {
		o.Category = "data-services"
	}

	d := &draft{crd: crd, v: crd.StorageVersion(), o: o, namespaced: spec.Scope != "Cluster"}
	d.key = camel(strings.Split(cmpOr(spec.Names.Singular, strings.ToLower(spec.Names.Kind)), "-"))

	if !identRe.MatchString(d.key) {
		d.key = "object"
	}

	d.readFields()
	d.readHealth()
	d.readColumns()
	d.scale = d.v.Subresources != nil && d.v.Subresources.Scale != nil

	return d, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

// generatedID is id, else made from group (see GenerateOptions.ID).
func generatedID(id, group string) string {
	if id != "" {
		return id
	}

	label := group
	for _, tld := range []string{".io", ".com", ".dev", ".org"} {
		label = strings.TrimSuffix(label, tld)
	}

	// The first label: kube-green.com and a.b.example.io keep their product part.
	if i := strings.Index(label, "."); i >= 0 {
		label = label[:i]
	}

	return strings.Trim(idCleanRe.ReplaceAllString(strings.ToLower(label), "-"), "-")
}

// camel joins parts in camel case: [target name] is targetName.
func camel(parts []string) string {
	var b strings.Builder

	for i, p := range parts {
		if p == "" {
			continue
		}

		if i == 0 || b.Len() == 0 {
			b.WriteString(strings.ToLower(p[:1]) + p[1:])
		} else {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}

	return b.String()
}

// readFields: spec scalars at depth 2 at most, then status scalars, in name order.
func (d *draft) readFields() {
	seen := map[string]bool{}

	add := func(name, expr, comment string) string {
		if seen[name] {
			name = "status" + strings.ToUpper(name[:1]) + name[1:]
		}

		seen[name] = true
		d.fields = append(d.fields, draftField{name: name, expr: expr, comment: comment})

		return name
	}

	for _, p := range sortedProps(d.v.Property("spec")) {
		node := d.v.Property("spec", p)
		if isScalar(node) {
			add(p, scalarExpr("$.spec."+p, node), description(node))

			continue
		}

		for _, q := range sortedProps(node) {
			if child := d.v.Property("spec", p, q); isScalar(child) {
				add(camel([]string{p, q}), scalarExpr("$.spec."+p+"."+q, child), description(child))
			}
		}
	}

	for _, p := range sortedProps(d.v.Property("status")) {
		node := d.v.Property("status", p)
		if !isScalar(node) {
			continue
		}

		name := add(p, scalarExpr("$.status."+p, node), description(node))
		if p == "phase" {
			d.phaseField = name
		}
	}
}

// sortedProps are a schema node's property names the expressions can name, sorted.
func sortedProps(node map[string]any) []string {
	props, _ := node["properties"].(map[string]any) //nolint:errcheck // nil when absent

	var out []string

	for name := range props {
		if identRe.MatchString(name) {
			out = append(out, name)
		}
	}

	slices.Sort(out)

	return out
}

func isScalar(node map[string]any) bool {
	switch node["type"] {
	case "string", "integer", "number", "boolean":
		return true
	}

	return false
}

// scalarExpr reads path, with the schema's default when it has one.
func scalarExpr(path string, node map[string]any) string {
	def, ok := node["default"]
	if !ok {
		return path
	}

	lit, err := json.Marshal(def)
	if err != nil {
		return path
	}

	return path + " | default(" + string(lit) + ")"
}

// description is a schema node's description on one line, short enough for a comment.
func description(node map[string]any) string {
	s, _ := node["description"].(string) //nolint:errcheck // "" when absent
	s = strings.Join(strings.Fields(s), " ")

	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}

	if len(s) > 120 {
		s = strings.TrimSpace(s[:117]) + "…"
	}

	return s
}

// readHealth: rules from the condition types and the phase enum, critical first, the ok phase last.
func (d *draft) readHealth() {
	d.hasConds = d.v.Property("status", "conditions") != nil
	d.conditions = d.v.ConditionTypes()

	// The condition types the printer columns filter on count too.
	for _, col := range d.v.AdditionalPrinterColumns {
		if t := columnCondition(col.JSONPath); t != "" && !slices.Contains(d.conditions, t) {
			d.conditions = append(d.conditions, t)
		}
	}

	d.conditions = slices.DeleteFunc(d.conditions, func(t string) bool { return !identRe.MatchString(t) })

	if d.phaseField != "" {
		enum, _ := d.v.Property("status", "phase")["enum"].([]any) //nolint:errcheck // nil when absent
		for _, e := range enum {
			s, _ := e.(string) //nolint:errcheck // a non-string value is skipped
			switch {
			case containsAny(s, "Fail", "Error", "Degraded"):
				d.phase.critical = append(d.phase.critical, s)
			case containsAny(s, "Pending", "Progressing", "Creating"):
				d.phase.info = append(d.phase.info, s)
			case containsAny(s, "Ready", "Running", "Succeeded", "Bound", "Active"):
				d.phase.ok = append(d.phase.ok, s)
			}
		}
	}

	if len(d.phase.critical) > 0 {
		d.rules = append(d.rules, draftRule{when: d.phaseIn(d.phase.critical), health: "critical", reason: "phase-failed", message: "phase {{ fields." + d.phaseField + " }}"})
	}

	for _, t := range d.conditions {
		d.rules = append(d.rules, draftRule{
			when: "conditions." + t + " == false", health: "warning",
			reason: strings.ToLower(t) + "-false", message: t + " is not true",
		})
	}

	if len(d.phase.info) > 0 {
		d.rules = append(d.rules, draftRule{when: d.phaseIn(d.phase.info), health: "info", reason: "phase-pending", message: "phase {{ fields." + d.phaseField + " }}"})
	}

	if len(d.phase.ok) > 0 {
		d.rules = append(d.rules, draftRule{when: d.phaseIn(d.phase.ok), health: "ok", reason: "phase-ok", message: "phase {{ fields." + d.phaseField + " }}"})
	}
}

func containsAny(s string, words ...string) bool {
	return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(s, w) })
}

func (d *draft) phaseIn(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = "fields." + d.phaseField + " == " + fmt.Sprintf("%q", v)
	}

	return strings.Join(parts, " || ")
}

// columnCondition is the condition type a printer column filters on, "" for none.
func columnCondition(jsonPath string) string {
	if m := conditionFilter.FindStringSubmatch(normalizeQuotes(strings.Join(strings.Fields(jsonPath), ""))); m != nil {
		return m[1]
	}

	return ""
}

// columnExpr reads a printer column, "" when the expressions cannot (a filter other than a
// condition's, an index, a name with a dash).
func columnExpr(jsonPath string) string {
	if t := columnCondition(jsonPath); t != "" {
		return `condition("` + t + `").status | default("")`
	}

	path := strings.TrimSpace(jsonPath)
	if !plainPathRe.MatchString(path) {
		return ""
	}

	return "$" + path
}

// readColumns: the list row from the printer columns (docs/plugins.md table).
func (d *draft) readColumns() {
	for _, col := range d.v.AdditionalPrinterColumns {
		path := strings.TrimSpace(col.JSONPath)
		if path == ".metadata.name" || path == ".metadata.namespace" {
			continue
		}

		expr := columnExpr(path)
		if expr == "" {
			continue
		}

		switch {
		case col.Type == "date" && d.row.trailing == "":
			d.row.trailing = expr + " | relativeTime()"
		case col.Priority == 0 && d.row.status == "" && col.Type != "date":
			d.row.status, d.row.statusLabel = expr, col.Name
		default:
			value := expr
			if col.Type == "date" {
				value += " | relativeTime()"
			}

			d.row.items = append(d.row.items, [2]string{col.Name, value})
		}
	}
}

// sampled: the rules the two samples trigger (see samples).
func (d *draft) sampled() (ok, failing *draftRule) {
	for i := range d.rules {
		if d.rules[i].health == "ok" {
			ok = &d.rules[i]
		}
	}

	for _, health := range []string{"critical", "warning"} {
		for i := range d.rules {
			if d.rules[i].health == health {
				return ok, &d.rules[i]
			}
		}
	}

	return ok, nil
}
