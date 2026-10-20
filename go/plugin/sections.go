package plugin

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// section renders one primitive of a row's detail; conditions, relations, badges and text
// with nothing to show are left out (ok false).
func (r *renderer) section(where string, s Section, el element) (TreeSection, bool) {
	out := TreeSection{Type: s.Type}

	switch s.Type {
	case "keyValues":
		out.Items = r.keyValues(where, s.Items, el.env)
	case "badges":
		badges := r.badgeItems(where, s.Items, el.env)
		if len(badges) == 0 {
			return out, false
		}

		out.Items = badges
	case "gauge":
		r.gauge(where, s, el.env, &out)
	case "conditions":
		conds := r.conditions(el.env.Object)
		if len(conds) == 0 {
			return out, false
		}

		out.Items = conds
	case "relations":
		rels := r.relations(where, s.Items, el.env)
		if len(rels) == 0 {
			return out, false
		}

		out.Items = rels
	case "table":
		return r.table(where, s), true
	case "text":
		// A quoted text the template gives (cond ? "words" : ""), as a row's status text.
		out.Text = r.tr(r.template(where+".text", s.Text, el.env))
		if out.Text == "" {
			return out, false
		}

		out.Tone = r.tone(where+".tone", s.Tone, el.env, "")
		out.Mono = s.Mono
	case "actions":
		out.Items = r.actionItems(s.Items, el.env)
	}

	return out, true
}

func (r *renderer) keyValues(where string, items []Item, env expr.Env) []TreeItem {
	out := make([]TreeItem, 0, len(items))

	for i, it := range items {
		at := where + ".items[" + strconv.Itoa(i) + "]"
		out = append(out, TreeItem{
			Label: r.tr(it.Label), Value: r.text(at+".value", it.Value, env),
			Tone: r.tone(at+".tone", it.Tone, env, ""), Mono: it.Mono,
		})
	}

	return out
}

func (r *renderer) badgeItems(where string, items []Item, env expr.Env) []TreeItem {
	out := make([]TreeItem, 0, len(items))

	for i, it := range items {
		if b, ok := r.badge(where+".items["+strconv.Itoa(i)+"]", it, env); ok {
			out = append(out, TreeItem{Label: b.Label, Value: b.Value, Tone: b.Tone})
		}
	}

	return out
}

// gauge is a value out of a max; its text is the template, else the two values in unit.
func (r *renderer) gauge(where string, s Section, env expr.Env, out *TreeSection) {
	value, _ := number(r.eval(where+".value", s.Value, env))
	limit, _ := number(r.eval(where+".max", s.Max, env))

	out.Label, out.Value, out.Max, out.Unit = r.tr(s.Label), &value, &limit, s.Unit
	out.Tone = r.tone(where+".tone", s.Tone, env, "ok")
	out.Text = r.template(where+".text", s.Text, env)

	if out.Text != "" {
		return
	}

	switch s.Unit {
	case "bytes":
		out.Text = formatBytes(value) + " / " + formatBytes(limit)
	case "percent":
		out.Text = r.display(value) + "%"
	default:
		out.Text = strings.TrimSpace(r.display(value) + " / " + r.display(limit) + " " + s.Unit)
	}
}

func formatBytes(n float64) string {
	fn, _ := expr.Library("bytes")
	v, _ := fn.Call(expr.Ctx{}, []expr.Value{n})

	return expr.Display(v)
}

// conditions are the object's status.conditions, toned by the Go core's reading.
func (r *renderer) conditions(obj any) []TreeCondItem {
	m, _ := obj.(map[string]any)
	status, _ := m["status"].(map[string]any)
	list, _ := status["conditions"].([]any)

	toTime, _ := expr.Library("time")
	out := make([]TreeCondItem, 0, len(list))

	for _, item := range list {
		c, ok := item.(map[string]any)
		if !ok || c["type"] == nil {
			continue
		}

		tone := "unknown"
		if r.in.Heuristics != nil {
			tone = r.in.Heuristics.ConditionTone(c)
		}

		at := c["lastTransitionTime"]
		if at == nil {
			at = c["lastUpdateTime"]
		}

		when, _ := toTime.Call(expr.Ctx{}, []expr.Value{at})
		ms, _ := when.(int64)

		out = append(out, TreeCondItem{
			Type: expr.Display(c["type"]), Status: expr.Display(c["status"]), Reason: expr.Display(c["reason"]),
			Message: expr.Display(c["message"]), Tone: tone, LastTransition: ms,
		})
	}

	return out
}

// relations are the related objects the items name (one object, or a list of them); an item
// whose object is absent is left out.
func (r *renderer) relations(where string, items []Item, env expr.Env) []TreeItem {
	var out []TreeItem

	for i, it := range items {
		at := where + ".items[" + strconv.Itoa(i) + "]"

		for _, v := range elementsOf(r.eval(at+".ref", it.Ref, env)) {
			if ref := r.refOf(v); ref != nil {
				out = append(out, TreeItem{Label: r.tr(it.Label), Ref: ref, Text: r.text(at+".text", it.Text, env)})
			}
		}
	}

	return out
}

// table walks its resource or data service; the same table in every row is rendered once.
func (r *renderer) table(where string, s Section) TreeSection {
	key := where + "\x00" + s.From
	if t, ok := r.tables[key]; ok {
		return t
	}

	res, ds := r.source(s.From)
	out := TreeSection{Type: "table", Columns: make([]TreeColumn, len(s.Columns)), Rows: [][]string{}}

	for i, col := range s.Columns {
		out.Columns[i] = TreeColumn{Label: r.tr(col.Label)}
	}

	for _, el := range r.elements(res, ds) {
		cells := make([]string, len(s.Columns))
		for i, col := range s.Columns {
			cells[i] = r.text(where+".columns["+strconv.Itoa(i)+"].value", col.Value, el.env)
		}

		out.Rows = append(out.Rows, cells)
	}

	r.tables[key] = out

	return out
}

// source resolves a from as the compiler does: dataServices.<name>, a resource, else a bare
// data service name.
func (r *renderer) source(from string) (res, ds string) {
	if name, ok := strings.CutPrefix(from, "dataServices."); ok {
		return "", name
	}

	if _, ok := r.c.Plugin.Resources[from]; ok {
		return from, ""
	}

	return "", from
}

// actionItems are the buttons: enabled by enabledWhen, with the reason when not. Whether the
// account may run them is set afterwards (Tree.SetAllowed).
func (r *renderer) actionItems(items []Item, env expr.Env) []TreeAction {
	out := make([]TreeAction, 0, len(items))

	for _, it := range items {
		a, ok := r.c.Plugin.Actions[it.Action]
		if !ok {
			continue
		}

		at := "actions." + it.Action
		ta := TreeAction{ID: it.Action, Label: r.tr(a.Label), Enabled: true, Confirm: cmp.Or(a.Confirm, "plain")}

		if !a.EnabledWhen.IsZero() {
			ta.Enabled = expr.Truthy(r.eval(at+".enabledWhen", a.EnabledWhen, env))
		}

		if !ta.Enabled {
			ta.DisabledReason = r.template(at+".disabledReason", a.DisabledReason, env)
		}

		for _, p := range a.Params {
			ta.Params = append(ta.Params, TreeParam{Name: p.Name, Label: r.tr(p.Label), Type: p.Type, Values: p.Values, Default: p.Default})
		}

		out = append(out, ta)
	}

	return out
}

// findings are one per object a finding's when holds for, in file order.
func (r *renderer) findings() []TreeFind {
	out := []TreeFind{}

	for i, f := range r.c.Plugin.Findings {
		where := "findings[" + strconv.Itoa(i) + "]"

		for _, o := range r.objects[f.From] {
			env := r.objectEnv(o)
			if !expr.Truthy(r.eval(where+".when", f.When, env)) {
				continue
			}

			ref := r.refOf(o.raw)
			env.Vars = map[string]expr.Value{"name": ref.Name, "namespace": ref.Namespace, "message": o.message, "reason": o.reason}

			out = append(out, TreeFind{
				ID: f.ID, Severity: f.Severity, Ref: ref, Opens: f.Opens,
				Title:  r.template(where+".title", f.Title, env),
				Detail: r.template(where+".detail", f.Detail, env),
			})
		}
	}

	return out
}
