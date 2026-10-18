package plugin

import (
	"cmp"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Lint reports what a valid plugin probably lacks, as warnings: labels without a
// translation, resources no card shows, health rules no sample triggers, list cards without
// an empty text and, given the CRD the plugin reads (crd may be nil), printer columns no
// field or card uses, condition types of the schema no health rule names, a kind or scope
// that differs from the CRD's, and a scale subresource without a scale action; an action
// whose rbac does not ask for what its request needs. An action without rbac is not a
// warning: Validate refuses it. A plugin that does not validate gets
// Validate's problems instead, so a broken plugin never lints clean. Sorted by line.
func Lint(p *Plugin, crd *CRDManifest) []Problem {
	c, err := Compile(p)
	if err != nil {
		return Validate(p)
	}

	l := &linter{p: p, c: c}
	l.labels()
	l.unusedResources()
	l.untriggeredHealth(time.Now())
	l.emptyTexts()
	l.actionRBAC()

	if crd != nil {
		l.crd(crd)
	}

	slices.SortStableFunc(l.problems, func(a, b Problem) int { return cmp.Compare(a.Line, b.Line) })

	return l.problems
}

type linter struct {
	p        *Plugin
	c        *Compiled
	problems []Problem
}

func (l *linter) warn(path, format string, args ...any) {
	l.problems = append(l.problems, Problem{Path: path, Line: l.p.src.line(path), Msg: fmt.Sprintf(format, args...), Severity: SeverityWarning})
}

// labels: every shown text should be in every locale the plugin translates to; a plugin
// with no i18n at all gets one warning.
func (l *linter) labels() {
	refs := labelRefs(l.p)
	if len(refs) == 0 {
		return
	}

	locales := sortedKeys(l.p.I18n)
	if len(locales) == 0 {
		l.problems = append(l.problems, Problem{Line: l.p.src.line(refs[0].Path), Severity: SeverityWarning,
			Msg: fmt.Sprintf("no i18n: the %d labels stay English in every language (add i18n.fr, i18n.de…)", len(uniqueTexts(refs)))})

		return
	}

	seen := map[string]bool{}

	for _, ref := range refs {
		if seen[ref.Text] {
			continue
		}

		seen[ref.Text] = true

		var missing []string

		for _, locale := range locales {
			if l.p.I18n[locale][ref.Text] == "" {
				missing = append(missing, locale)
			}
		}

		if len(missing) > 0 {
			l.warn(ref.Path, "%q has no translation in %s", ref.Text, strings.Join(missing, ", "))
		}
	}
}

// unusedResources: a resource no card, data service, finding or expression (a join in a
// field, a finding or an action) names is read for nothing.
func (l *linter) unusedResources() {
	p := l.p

	var froms, sources []string

	for _, card := range p.Cards {
		froms = append(froms, strings.TrimPrefix(card.From, "dataServices."))

		if card.Row != nil {
			for _, s := range card.Row.Detail {
				froms = append(froms, strings.TrimPrefix(s.From, "dataServices."))
			}
		}
	}

	for _, ds := range p.DataServices {
		froms = append(froms, ds.From)
	}

	for _, f := range p.Findings {
		froms = append(froms, f.From)
	}

	for _, part := range []any{p.Cards, p.DataServices, p.Fields, p.Findings, p.Actions} {
		sources = exprSources(sources, reflect.ValueOf(part))
	}

	for _, res := range l.c.Resources {
		if slices.Contains(froms, res) {
			continue
		}

		name := identifier(res)
		if !slices.ContainsFunc(sources, name.MatchString) {
			l.warn("resources."+res, "resource %s is read but no card shows it (a card's or a finding's from, a data service, or a join)", res)
		}
	}
}

// untriggeredHealth: a health rule no sample matches is untested, and the demo never shows it.
// Rules are first-match, so a rule an earlier one always shadows is reported too.
func (l *linter) untriggeredHealth(now time.Time) {
	r := newRenderer(l.c, SamplesInput(l.c, "", nil, now), now)
	r.indexObjects()
	r.evalObjects()

	triggered := map[string]bool{}

	for res, objs := range r.objects {
		for _, o := range objs {
			if o.rule > 0 {
				triggered[res+"["+strconv.Itoa(o.rule-1)+"]"] = true
			}
		}
	}

	for _, res := range sortedKeys(l.p.Health) {
		for i, rule := range l.p.Health[res] {
			key := res + "[" + strconv.Itoa(i) + "]"
			if !triggered[key] {
				l.warn("health."+key, "no sample triggers this rule (health %s): add a sample it holds for", rule.Health)
			}
		}
	}
}

// emptyTexts: a list card should say what an empty list means.
func (l *linter) emptyTexts() {
	for _, nc := range l.c.Cards {
		if nc.Name != "summary" && nc.From != "" && nc.Empty == "" {
			l.warn("cards."+nc.Name, "card %s has no empty text: say what no row means (empty: \"No …\")", nc.Name)
		}
	}
}

// crd checks the plugin's resources against the CRD they read.
func (l *linter) crd(crd *CRDManifest) {
	spec := crd.Spec

	var matched bool

	for _, key := range l.c.Resources {
		res := l.p.Resources[key]
		if res.Group != spec.Group || res.Resource != spec.Names.Plural {
			continue
		}

		matched = true
		path := "resources." + key

		if res.Kind != spec.Names.Kind {
			l.warn(path+".kind", "kind %s: the CRD says %s", res.Kind, spec.Names.Kind)
		}

		if spec.Scope != "" && res.Scope != spec.Scope {
			l.warn(path+".scope", "scope %s: the CRD says %s", res.Scope, spec.Scope)
		}

		v := crd.StorageVersion()
		if res.Version != "preferred" {
			if v = crd.Version(res.Version); v == nil {
				l.warn(path+".version", "the CRD has no version %s", res.Version)

				continue
			}
		}

		l.printerColumns(path, v)
		l.conditionTypes(key, v)
		l.scale(key, v)
	}

	if !matched {
		l.warn("resources", "no resource reads the CRD's %s (group %s, resource %s)", spec.Names.Kind, spec.Group, spec.Names.Plural)
	}
}

// printerColumns: the columns kubectl shows are what the operator's authors thought matters.
func (l *linter) printerColumns(path string, v *CRDVersion) {
	var sources []string
	for _, part := range []any{l.p.Fields, l.p.Health, l.p.DataServices, l.p.Cards, l.p.Actions, l.p.Findings} {
		sources = exprSources(sources, reflect.ValueOf(part))
	}

	for _, col := range v.AdditionalPrinterColumns {
		used := func(src string) bool { return usesPath(src, col.JSONPath) }

		// A column of a condition ([?(@.type=="Ready")]) is read as conditions.Ready too.
		if m := conditionFilter.FindStringSubmatch(normalizeQuotes(strings.Join(strings.Fields(col.JSONPath), ""))); m != nil {
			byName := conditionRef(m[1])
			used = func(src string) bool { return usesPath(src, col.JSONPath) || byName.MatchString(src) }
		}

		if !slices.ContainsFunc(sources, used) {
			l.warn(path, "printer column %q (%s) is not used by any field or card", col.Name, col.JSONPath)
		}
	}
}

// conditionFilter is a jsonPath's filter on a condition type: [?(@.type=="Ready")].
var conditionFilter = regexp.MustCompile(`conditions\[\?\(@\.type=="([^"]+)"\)\]`)

// conditionRef matches an expression that names the condition type t: conditions.t, or t as
// a string in either quote style (condition("t"), @.type == 't').
func conditionRef(t string) *regexp.Regexp {
	q := regexp.QuoteMeta(t)

	return regexp.MustCompile(`conditions\.` + q + `\b|"` + q + `"|'` + q + `'`)
}

// normalizeQuotes writes single-quoted strings with double quotes, so that jsonPaths and
// expressions compare whatever quotes they use.
func normalizeQuotes(s string) string { return strings.ReplaceAll(s, "'", `"`) }

// usesPath reports whether src reads the printer column's jsonPath (.spec.x) as $.spec.x,
// not as a prefix of a longer name. Spaces and quote styles do not matter.
func usesPath(src, jsonPath string) bool {
	src = normalizeQuotes(strings.Join(strings.Fields(src), ""))
	want := "$" + normalizeQuotes(strings.Join(strings.Fields(jsonPath), ""))

	for i := 0; ; {
		j := strings.Index(src[i:], want)
		if j < 0 {
			return false
		}

		end := i + j + len(want)
		if end == len(src) || !isIdentByte(src[end]) {
			return true
		}

		i = end
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// conditionTypes: a condition the CRD names is one a health rule should read, directly or
// through a field.
func (l *linter) conditionTypes(key string, v *CRDVersion) {
	var sources []string
	sources = exprSources(sources, reflect.ValueOf(l.p.Health[key]))
	sources = exprSources(sources, reflect.ValueOf(l.p.Fields[key]))

	for _, t := range v.ConditionTypes() {
		if !slices.ContainsFunc(sources, conditionRef(t).MatchString) {
			l.warn("resources."+key, "condition %s (in the CRD's schema) has no health rule: read it as conditions.%s", t, t)
		}
	}
}

// scale: a scale subresource is an action the plugin could offer.
func (l *linter) scale(key string, v *CRDVersion) {
	if v.Subresources == nil || v.Subresources.Scale == nil {
		return
	}

	for _, a := range l.p.Actions {
		if a.On == key && a.Verb == "subresource" && a.Subresource == "scale" {
			return
		}
	}

	l.warn("resources."+key, "the CRD has a scale subresource but no action scales %s (verb: subresource, subresource: scale)", key)
}

// identifier matches name as a whole name in an expression, not as a path member ($.x.name).
func identifier(name string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^.$\w])` + regexp.QuoteMeta(name) + `\b`)
}

var (
	exprType     = reflect.TypeFor[Expr]()
	templateType = reflect.TypeFor[Template]()
)

// exprSources appends the source of every expression and template under v.
func exprSources(out []string, v reflect.Value) []string {
	switch v.Kind() {
	case reflect.Struct:
		switch v.Type() {
		case exprType:
			if src := v.Interface().(Expr).Src; src != "" { //nolint:forcetypeassert // checked by type
				out = append(out, src)
			}
		case templateType:
			if src := v.Interface().(Template).Src; src != "" { //nolint:forcetypeassert // checked by type
				out = append(out, src)
			}
		default:
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					out = exprSources(out, v.Field(i))
				}
			}
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			out = exprSources(out, iter.Value())
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			out = exprSources(out, v.Index(i))
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			out = exprSources(out, v.Elem())
		}
	}

	return out
}

// LabelRef is a text the render passes through i18n, at its YAML path.
type LabelRef struct {
	Path string
	Text string
}

var (
	quotedText = regexp.MustCompile(`"([^"\\]*)"`)
	// templateHole is a {{ … }} of a template: what is left outside them is words to translate.
	templateHole = regexp.MustCompile(`\{\{[^}]*\}\}`)
	letter       = regexp.MustCompile(`\pL`)
)

// labelRefs are the labels, titles and messages the render translates, and the string
// literals of the row status texts, in file order; texts without a letter are left out.
func labelRefs(p *Plugin) []LabelRef {
	var out []LabelRef

	add := func(path, s string) {
		if s != "" && letter.MatchString(templateHole.ReplaceAllString(s, "")) {
			out = append(out, LabelRef{Path: path, Text: s})
		}
	}

	addItems := func(path string, items []Item) {
		for i, it := range items {
			add(path+"["+strconv.Itoa(i)+"].label", it.Label)
		}
	}

	for _, name := range keys(p.src, "cards", p.Cards) {
		card, path := p.Cards[name], "cards."+name
		if card.Title != nil {
			add(path+".title.one", card.Title.One)
			add(path+".title.other", card.Title.Other)
		}

		add(path+".empty", card.Empty)
		addItems(path+".badges", card.Badges)

		if card.Row != nil {
			labelRow(path+".row", card.Row, add, addItems)
		}
	}

	for _, res := range keys(p.src, "health", p.Health) {
		for i, r := range p.Health[res] {
			add("health."+res+"["+strconv.Itoa(i)+"].message", r.Message.Src)
		}
	}

	for _, id := range keys(p.src, "actions", p.Actions) {
		a, path := p.Actions[id], "actions."+id
		add(path+".label", a.Label)
		add(path+".disabledReason", a.DisabledReason.Src)

		for i, prm := range a.Params {
			add(path+".params["+strconv.Itoa(i)+"].label", prm.Label)
		}
	}

	for i, f := range p.Findings {
		path := "findings[" + strconv.Itoa(i) + "]"
		add(path+".title", f.Title.Src)
		add(path+".detail", f.Detail.Src)
	}

	slices.SortStableFunc(out, func(a, b LabelRef) int { return cmp.Compare(p.src.line(a.Path), p.src.line(b.Path)) })

	return out
}

func labelRow(path string, row *Row, add func(path, s string), addItems func(path string, items []Item)) {
	if row.Status != nil {
		for _, m := range quotedText.FindAllStringSubmatch(row.Status.Text.Src, -1) {
			add(path+".status.text", m[1])
		}
	}

	for i, s := range row.Detail {
		spath := path + ".detail[" + strconv.Itoa(i) + "]"
		add(spath+".label", s.Label)
		addItems(spath+".items", s.Items)

		if s.Type == "text" {
			add(spath+".text", s.Text.Src)
		}

		for j, col := range s.Columns {
			add(spath+".columns["+strconv.Itoa(j)+"].label", col.Label)
		}
	}
}

// uniqueTexts are the distinct texts of refs, sorted.
func uniqueTexts(refs []LabelRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Text)
	}

	slices.Sort(out)

	return slices.Compact(out)
}
