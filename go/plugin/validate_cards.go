package plugin

import (
	"slices"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

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
