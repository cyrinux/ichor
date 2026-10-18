package plugin

import (
	"cmp"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// Severities of a problem: an error stops the plugin from compiling, a warning does not.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Problem is something wrong in a plugin file, at a YAML path and line.
type Problem struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Msg      string `json:"msg"`
	Severity string `json:"severity"`
}

func (p Problem) String() string {
	if p.Path == "" {
		return fmt.Sprintf("%d: %s", p.Line, p.Msg)
	}

	return fmt.Sprintf("%d: %s: %s", p.Line, p.Path, p.Msg)
}

// HasErrors reports whether any problem is an error.
func HasErrors(problems []Problem) bool {
	return slices.ContainsFunc(problems, func(p Problem) bool { return p.Severity == SeverityError })
}

var (
	idPattern    = regexp.MustCompile(`^[a-z0-9-]+$`)
	keyPattern   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	nativeRef    = regexp.MustCompile(`^native:[A-Za-z0-9_-]+$`)
	localePatten = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
)

// Validate checks everything Load does not: identifiers, enums, references between
// resources, data services, cards and actions, required keys, and every expression in the
// scope where it runs. Problems are sorted by line.
func Validate(p *Plugin) []Problem {
	v := &validator{p: p, resources: sortedKeys(p.Resources), dataServices: keys(p.src, "dataServices", p.DataServices)}

	v.top()
	v.detects()
	v.resourceDecls()
	v.fields()
	v.healthRules()
	v.dataServiceDecls()
	v.cards()
	v.actions()
	v.findings()
	v.samples()
	v.i18n()

	slices.SortStableFunc(v.problems, func(a, b Problem) int { return cmp.Compare(a.Line, b.Line) })

	return v.problems
}

type validator struct {
	p            *Plugin
	resources    []string // sorted
	dataServices []string // file order
	problems     []Problem
}

func (v *validator) add(path string, line int, severity, format string, args ...any) {
	if line == 0 {
		line = v.p.src.line(path)
	}

	v.problems = append(v.problems, Problem{Path: path, Line: line, Msg: fmt.Sprintf(format, args...), Severity: severity})
}

func (v *validator) errorf(path, format string, args ...any) {
	v.add(path, 0, SeverityError, format, args...)
}

// enum checks a value against the enum of a schema field tag (the one source of truth).
func (v *validator) enum(path, value string, owner any, field string) {
	if value == "" {
		return
	}

	if allowed := tagEnum(owner, field); !slices.Contains(allowed, value) {
		v.errorf(path, "%q is not one of %s", value, strings.Join(allowed, ", "))
	}
}

func (v *validator) isResource(name string) bool { return slices.Contains(v.resources, name) }

func (v *validator) isDataService(name string) bool { return slices.Contains(v.dataServices, name) }

func (v *validator) resource(path, name string) bool {
	switch {
	case name == "":
		v.errorf(path, "is missing: name a resource (%s)", strings.Join(v.resources, ", "))
	case !v.isResource(name):
		v.errorf(path, "%q is not a declared resource (%s)", name, strings.Join(v.resources, ", "))
	default:
		return true
	}

	return false
}

// checkExpr reports a missing required expression, a parse error, and what expr.Check finds
// in scope, at the expression's own line.
func (v *validator) checkExpr(path string, e Expr, scope expr.Scope, required bool) {
	switch {
	case e.IsZero():
		if required {
			v.errorf(path, "is missing")
		}
	case e.err != nil:
		v.exprError(path, e.At, e.err)
	default:
		for _, pr := range e.node.Check(scope) {
			v.exprProblem(path, e.At, pr.Pos, pr.Message)
		}
	}
}

func (v *validator) checkTemplate(path string, t Template, scope expr.Scope, required bool) {
	switch {
	case t.IsZero():
		if required {
			v.errorf(path, "is missing")
		}
	case t.err != nil:
		v.exprError(path, t.At, t.err)
	default:
		for _, pr := range t.tpl.Check(scope) {
			v.exprProblem(path, t.At, pr.Pos, pr.Message)
		}
	}
}

func (v *validator) checkTone(path string, t Tone, scope expr.Scope) {
	if t.Literal == "" {
		v.checkExpr(path, t.Expr, scope, false)
	}
}

func (v *validator) exprError(path string, at At, err error) {
	var e *expr.Error
	if asExprError(err, &e) {
		v.exprProblem(path, at, e.Pos, e.Msg)

		return
	}

	v.add(path, at.Line, SeverityError, "%v", err)
}

// exprProblem reports msg at pos inside a value written at at, with the file column when
// it is known.
func (v *validator) exprProblem(path string, at At, pos expr.Pos, msg string) {
	if f := fileAt(at, pos); f.Col > 0 {
		v.add(path, f.Line, SeverityError, "column %d: %s", f.Col, msg)
	} else {
		v.add(path, f.Line, SeverityError, "%s", msg)
	}
}

func (v *validator) top() {
	p := v.p

	switch {
	case p.ID == "":
		v.errorf("id", "is missing")
	case !idPattern.MatchString(p.ID):
		v.errorf("id", "%q: use lowercase letters, digits and dashes", p.ID)
	}

	if p.Name == "" {
		v.errorf("name", "is missing")
	}

	v.enum("category", p.Category, Plugin{}, "Category")

	if p.Screen != "" && !nativeRef.MatchString(p.Screen) {
		v.errorf("screen", "%q: use native:<route>", p.Screen)
	}
}

func (v *validator) detects() {
	if len(v.p.Detect) == 0 {
		v.errorf("detect", "is missing: say how the product is detected (an API group, pods or a service)")
	}

	for i, d := range v.p.Detect {
		path := "detect[" + strconv.Itoa(i) + "]"

		set := 0
		for _, s := range []string{d.Group, d.Pods, d.Service} {
			if s != "" {
				set++
			}
		}

		if set != 1 {
			v.errorf(path, "set exactly one of group, pods or service")
		}

		if d.Resource != "" && d.Group == "" {
			v.errorf(path+".resource", "only applies with group")
		}
	}
}

func (v *validator) resourceDecls() {
	if len(v.p.Resources) == 0 {
		v.errorf("resources", "is missing: declare at least one resource")
	}

	for _, key := range v.resources {
		r, path := v.p.Resources[key], "resources."+key

		if !keyPattern.MatchString(key) {
			v.errorf(path, "%q: a resource key is a name (letters and digits, starting with a letter)", key)
		}

		for field, value := range map[string]string{"version": r.Version, "resource": r.Resource, "kind": r.Kind, "scope": r.Scope} {
			if value == "" {
				v.errorf(path+"."+field, "is missing")
			}
		}

		v.enum(path+".scope", r.Scope, Resource{}, "Scope")

		if r.Source != "" && !nativeRef.MatchString(r.Source) {
			v.errorf(path+".source", "%q: use native:<id>", r.Source)
		}

		if r.List != nil && (r.List.Limit < 0 || r.List.Limit > 5000) {
			v.errorf(path+".list.limit", "must be between 1 and 5000 (0 or absent is 500)")
		}
	}
}

// fieldNames are a resource's field names in file order; nil for "" (a data service).
func (v *validator) fieldNames(res string) []string {
	if res == "" {
		return nil
	}

	return keys(v.p.src, "fields."+res, v.p.Fields[res])
}

func (v *validator) fields() {
	for _, res := range keys(v.p.src, "fields", v.p.Fields) {
		path := "fields." + res
		if !v.resource(path, res) {
			continue
		}

		names := v.fieldNames(res)
		for i, name := range names {
			fpath := path + "." + name
			if !keyPattern.MatchString(name) {
				v.errorf(fpath, "%q: a field name is letters and digits, starting with a letter", name)
			}

			v.checkExpr(fpath, v.p.Fields[res][name], fieldScope(v.resources, names[:i]), true)
		}
	}
}

func (v *validator) healthRules() {
	for _, res := range keys(v.p.src, "health", v.p.Health) {
		path := "health." + res
		if !v.resource(path, res) {
			continue
		}

		scope := healthScope(v.fieldNames(res))

		for i, rule := range v.p.Health[res] {
			rpath := path + "[" + strconv.Itoa(i) + "]"
			v.checkExpr(rpath+".when", rule.When, scope, true)

			if rule.Health == "" {
				v.errorf(rpath+".health", "is missing")
			}

			v.enum(rpath+".health", rule.Health, HealthRule{}, "Health")
			v.checkTemplate(rpath+".message", rule.Message, withBool(scope, false), false)
		}
	}
}

func (v *validator) dataServiceDecls() {
	for i, name := range v.dataServices {
		ds, path := v.p.DataServices[name], "dataServices."+name
		if !keyPattern.MatchString(name) {
			v.errorf(path, "%q: a data service name is letters and digits, starting with a letter", name)
		}

		scope := dataServiceScope(v.resources, v.dataServices[:i])

		switch {
		case ds.From == "":
			if len(ds.Aggregate) == 0 {
				v.errorf(path, "set aggregate, or from with groupBy, where or select")
			}

			for _, field := range []struct {
				name string
				set  bool
			}{{"groupBy", !ds.GroupBy.IsZero()}, {"where", !ds.Where.IsZero()}, {"select", len(ds.Select) > 0}} {
				if field.set {
					v.errorf(path+"."+field.name, "needs from")
				}
			}
		case v.resource(path+".from", ds.From):
			elem := rowScope(v.fieldNames(ds.From), v.dataServices[:i])

			if !ds.GroupBy.IsZero() && len(ds.Select) > 0 {
				v.errorf(path, "set groupBy or select, not both")
			}

			if ds.GroupBy.IsZero() && len(ds.Select) == 0 {
				v.errorf(path, "with from, set groupBy (and aggregate) or select")
			}

			v.checkExpr(path+".groupBy", ds.GroupBy, elem, false)
			v.checkExpr(path+".where", ds.Where, withBool(elem, true), false)

			for _, key := range keys(v.p.src, path+".select", ds.Select) {
				v.checkExpr(path+".select."+key, ds.Select[key], elem, true)
			}

			// Grouped aggregations run over the group's objects.
			scope = groupScope(v.fieldNames(ds.From), v.dataServices[:i])
		}

		for _, key := range keys(v.p.src, path+".aggregate", ds.Aggregate) {
			v.checkExpr(path+".aggregate."+key, ds.Aggregate[key], scope, true)
		}

		v.checkExpr(path+".health", ds.Health, scope, false)
	}
}

func (v *validator) cards() {
	summary, ok := v.p.Cards["summary"]
	if !ok {
		v.errorf("cards.summary", "is missing: every plugin has a summary card (the tile)")
	} else {
		v.summaryCard(summary)
	}

	for _, name := range keys(v.p.src, "cards", v.p.Cards) {
		if name != "summary" {
			v.listCard("cards."+name, v.p.Cards[name])
		}
	}
}

func (v *validator) summaryCard(c Card) {
	scope := summaryScope(v.dataServices)

	switch {
	case c.Title == nil:
		v.errorf("cards.summary.title", "is missing")
	case c.Title.Expr.IsZero():
		v.checkExpr("cards.summary.title.count", c.Title.Count, scope, true)

		if c.Title.One == "" || c.Title.Other == "" {
			v.errorf("cards.summary.title", "set one and other (\"%%d schedule\", \"%%d schedules\")")
		}
	default:
		v.checkExpr("cards.summary.title", c.Title.Expr, scope, true)
	}

	v.checkExpr("cards.summary.health", c.Health, scope, false)
	v.items("cards.summary.badges", "badges", c.Badges, scope, "")

	if c.From != "" || c.Row != nil {
		v.errorf("cards.summary", "the summary is a tile: from and row belong to a list card")
	}
}

// source resolves a card's or a table's from: a resource, or dataServices.<name> (a bare
// data service name is accepted when no resource has it). It returns the resource whose
// fields are in scope, "" for a data service.
func (v *validator) source(path, from string) (string, bool) {
	name, isDS := strings.CutPrefix(from, "dataServices.")

	switch {
	case from == "":
		v.errorf(path, "is missing: name a resource or dataServices.<name>")
	case !isDS && v.isResource(from):
		return from, true
	case v.isDataService(name):
		return "", true
	default:
		v.errorf(path, "%q is neither a resource (%s) nor a data service (%s)", from,
			strings.Join(v.resources, ", "), strings.Join(v.dataServices, ", "))
	}

	return "", false
}

func (v *validator) listCard(path string, c Card) {
	res, ok := v.source(path+".from", c.From)
	if !ok {
		return
	}

	if c.Title != nil || len(c.Badges) > 0 {
		v.errorf(path, "title and badges belong to the summary card")
	}

	scope := rowScope(v.fieldNames(res), v.dataServices)

	for i, key := range c.Sort {
		src := strings.TrimSpace(key)
		src = strings.TrimSuffix(strings.TrimSuffix(src, " desc"), " asc")
		v.checkExpr(path+".sort["+strconv.Itoa(i)+"]", NewExpr(src, At{Line: v.p.src.line(path + ".sort[" + strconv.Itoa(i) + "]")}), scope, true)
	}

	if c.Row == nil {
		v.errorf(path+".row", "is missing")

		return
	}

	row := path + ".row"
	v.checkExpr(row+".title", c.Row.Title, scope, true)
	v.checkExpr(row+".subtitle", c.Row.Subtitle, scope, false)

	if c.Row.Status != nil {
		v.checkTone(row+".status.tone", c.Row.Status.Tone, scope)
		v.checkExpr(row+".status.text", c.Row.Status.Text, scope, false)
	}

	if c.Row.Trailing != nil {
		v.checkExpr(row+".trailing.text", c.Row.Trailing.Text, scope, true)
	}

	for i, s := range c.Row.Detail {
		v.section(row+".detail["+strconv.Itoa(i)+"]", s, scope, res)
	}
}

// sectionKeys are the keys each section type takes besides type.
var sectionKeys = map[string][]string{
	"keyValues":  {"items"},
	"badges":     {"items"},
	"gauge":      {"label", "value", "max", "unit", "tone", "text"},
	"conditions": {},
	"relations":  {"items"},
	"table":      {"from", "columns"},
	"text":       {"text", "tone", "mono"},
	"actions":    {"items"},
}

func (v *validator) section(path string, s Section, scope expr.Scope, res string) {
	allowed, ok := sectionKeys[s.Type]
	if !ok {
		v.enum(path+".type", s.Type, Section{}, "Type")

		if s.Type == "" {
			v.errorf(path+".type", "is missing")
		}

		return
	}

	for _, key := range v.p.src.order[path] {
		if key != "type" && !slices.Contains(allowed, key) {
			v.errorf(path+"."+key, "does not apply to a %s section", s.Type)
		}
	}

	switch s.Type {
	case "keyValues", "badges", "relations":
		v.items(path+".items", s.Type, s.Items, scope, res)
	case "actions":
		v.items(path+".items", s.Type, s.Items, scope, res)
	case "gauge":
		v.checkExpr(path+".value", s.Value, scope, true)
		v.checkExpr(path+".max", s.Max, scope, true)
		v.checkTone(path+".tone", s.Tone, scope)
		v.checkTemplate(path+".text", s.Text, scope, false)
	case "table":
		v.table(path, s)
	case "text":
		v.checkTemplate(path+".text", s.Text, scope, true)
		v.checkTone(path+".tone", s.Tone, scope)
	}
}

func (v *validator) table(path string, s Section) {
	res, ok := v.source(path+".from", s.From)
	if !ok {
		return
	}

	if len(s.Columns) == 0 {
		v.errorf(path+".columns", "is missing")
	}

	scope := rowScope(v.fieldNames(res), v.dataServices)
	for i, col := range s.Columns {
		cpath := path + ".columns[" + strconv.Itoa(i) + "]"
		if col.Label == "" {
			v.errorf(cpath+".label", "is missing")
		}

		v.checkExpr(cpath+".value", col.Value, scope, true)
	}
}

// itemKeys are the keys an item takes, per section type.
var itemKeys = map[string][]string{
	"keyValues": {"label", "value", "tone", "mono"},
	"badges":    {"label", "value", "tone", "on", "hideWhenZero"},
	"relations": {"label", "ref", "text"},
}

// items checks a section's (or the summary's badges') items; res is the row's resource,
// which an actions section's actions must apply to.
func (v *validator) items(path, kind string, items []Item, scope expr.Scope, res string) {
	for i, it := range items {
		ipath := path + "[" + strconv.Itoa(i) + "]"

		if kind == "actions" {
			v.actionRef(ipath, it, res)

			continue
		}

		if it.Action != "" {
			v.errorf(ipath, "a %s item is a map (label, …), not a plain string", kind)

			continue
		}

		for _, key := range v.p.src.order[ipath] {
			if !slices.Contains(itemKeys[kind], key) {
				v.errorf(ipath+"."+key, "does not apply to a %s item", kind)
			}
		}

		if it.Label == "" {
			v.errorf(ipath+".label", "is missing")
		}

		v.checkTone(ipath+".tone", it.Tone, scope)
		v.checkExpr(ipath+".on", it.On, withBool(scope, true), false)
		v.checkExpr(ipath+".value", it.Value, scope, kind == "keyValues")
		v.checkExpr(ipath+".ref", it.Ref, scope, kind == "relations")
		v.checkExpr(ipath+".text", it.Text, scope, false)

		if kind == "badges" && it.Value.IsZero() && it.On.IsZero() {
			v.errorf(ipath, "a badge needs value or on")
		}
	}
}

func (v *validator) actionRef(path string, it Item, res string) {
	a, ok := v.p.Actions[it.Action]

	switch {
	case it.Action == "":
		v.errorf(path, "an actions item is an action id")
	case !ok:
		v.errorf(path, "%q is not a declared action (%s)", it.Action, strings.Join(sortedKeys(v.p.Actions), ", "))
	case res == "":
		v.errorf(path, "a row of a data service has no object to act on: actions belong to a card of a resource")
	case a.On != res:
		v.errorf(path, "action %q applies to %s, not to this card's %s", it.Action, a.On, res)
	}
}

func (v *validator) actions() {
	for _, id := range sortedKeys(v.p.Actions) {
		a, path := v.p.Actions[id], "actions."+id
		if !keyPattern.MatchString(id) {
			v.errorf(path, "%q: an action id is letters and digits, starting with a letter", id)
		}

		if a.Label == "" {
			v.errorf(path+".label", "is missing")
		}

		onOK := v.resource(path+".on", a.On)
		v.enum(path+".verb", a.Verb, Action{}, "Verb")

		switch {
		case a.Verb == "":
			v.errorf(path+".verb", "is missing")
		case a.Verb == "subresource" && a.Subresource == "":
			v.errorf(path+".subresource", "is missing: verb subresource names one (start, scale…)")
		case a.Verb != "subresource" && a.Subresource != "":
			v.errorf(path+".subresource", "only applies with verb subresource")
		}

		v.rbac(path+".rbac", a.RBAC)
		v.enum(path+".confirm", a.Confirm, Action{}, "Confirm")
		v.actionRequest(path, a, onOK)

		if a.Audit == "" {
			v.errorf(path+".audit", "is missing: name the audit entry (%s.%s)", v.p.ID, id)
		}

		params := v.params(path+".params", a.Params)

		var fields []string
		if onOK {
			fields = v.fieldNames(a.On)
		}

		row := rowScope(fields, v.dataServices)
		v.checkExpr(path+".enabledWhen", a.EnabledWhen, withBool(row, true), false)
		v.checkTemplate(path+".disabledReason", a.DisabledReason, row, false)
		v.body(path+".body", a.Body, bodyScope(fields, params))
	}
}

func (v *validator) rbac(path string, r *RBAC) {
	if r == nil {
		v.errorf(path, "is missing: an action declares the permission it needs (verbs, group, resource)")

		return
	}

	if len(r.Verbs) == 0 {
		v.errorf(path+".verbs", "is missing")
	}

	if r.Resource == "" {
		v.errorf(path+".resource", "is missing")
	}
}

func (v *validator) params(path string, params []Param) []string {
	var names []string

	for i, prm := range params {
		ppath := path + "[" + strconv.Itoa(i) + "]"

		switch {
		case !keyPattern.MatchString(prm.Name):
			v.errorf(ppath+".name", "%q: a parameter name is letters and digits, starting with a letter", prm.Name)
		case slices.Contains(names, prm.Name):
			v.errorf(ppath+".name", "%q is declared twice", prm.Name)
		}

		names = append(names, prm.Name)

		if prm.Label == "" {
			v.errorf(ppath+".label", "is missing")
		}

		if prm.Type == "" {
			v.errorf(ppath+".type", "is missing")
		}

		v.enum(ppath+".type", prm.Type, Param{}, "Type")

		if prm.Type == "enum" && len(prm.Values) == 0 {
			v.errorf(ppath+".values", "an enum parameter lists its values")
		}

		v.paramDefault(ppath, prm)
	}

	return names
}

// body checks every string of an action body as a template.
func (v *validator) body(path string, body any, scope expr.Scope) {
	switch b := body.(type) {
	case string:
		v.checkTemplate(path, NewTemplate(b, At{Line: v.p.src.line(path)}), scope, false)
	case map[string]any:
		for _, k := range sortedKeys(b) {
			v.body(path+"."+k, b[k], scope)
		}
	case []any:
		for i, item := range b {
			v.body(path+"["+strconv.Itoa(i)+"]", item, scope)
		}
	}
}

func (v *validator) findings() {
	var ids []string

	for i, f := range v.p.Findings {
		path := "findings[" + strconv.Itoa(i) + "]"

		switch {
		case !idPattern.MatchString(f.ID):
			v.errorf(path+".id", "%q: use lowercase letters, digits and dashes", f.ID)
		case slices.Contains(ids, f.ID):
			v.errorf(path+".id", "%q is declared twice", f.ID)
		}

		ids = append(ids, f.ID)

		var fields []string
		if v.resource(path+".from", f.From) {
			fields = v.fieldNames(f.From)
		}

		row := rowScope(fields, v.dataServices)
		v.checkExpr(path+".when", f.When, withBool(row, true), true)

		if f.Severity == "" {
			v.errorf(path+".severity", "is missing")
		}

		v.enum(path+".severity", f.Severity, Finding{}, "Severity")

		text := findingScope(fields, v.dataServices)
		v.checkTemplate(path+".title", f.Title, text, true)
		v.checkTemplate(path+".detail", f.Detail, text, false)

		if _, ok := v.p.Cards[f.Opens]; f.Opens != "" && !ok {
			v.errorf(path+".opens", "%q is not a card (%s)", f.Opens, strings.Join(sortedKeys(v.p.Cards), ", "))
		}
	}
}

func (v *validator) samples() {
	for _, res := range v.resources {
		if len(v.p.Samples[res]) == 0 {
			v.errorf("samples", "%s has no sample: add at least one object (demo data and tests render them)", res)
		}
	}

	for _, res := range sortedKeys(v.p.Samples) {
		if !v.isResource(res) {
			v.errorf("samples."+res, "%q is not a declared resource", res)
		}
	}
}

func (v *validator) i18n() {
	for _, locale := range sortedKeys(v.p.I18n) {
		if !localePatten.MatchString(locale) {
			v.errorf("i18n."+locale, "%q is not a locale (fr, pt-BR…)", locale)
		}
	}
}

// tagEnum is the enum=a|b|c of a field's schema tag.
func tagEnum(owner any, field string) []string {
	f, ok := reflect.TypeOf(owner).FieldByName(field)
	if !ok {
		panic("plugin: no field " + field)
	}

	for part := range strings.SplitSeq(f.Tag.Get("schema"), ",") {
		if values, ok := strings.CutPrefix(part, "enum="); ok {
			return strings.Split(values, "|")
		}
	}

	panic("plugin: no enum on " + field)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return sortedStrings(out)
}

func sortedStrings(s []string) []string {
	slices.Sort(s)

	return s
}
