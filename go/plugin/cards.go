package plugin

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// tree assembles the render tree once the objects and the data services are evaluated.
func (r *renderer) tree() Tree {
	p := r.c.Plugin

	t := Tree{
		Plugin: TreePlugin{
			ID: p.ID, Name: p.Name, Icon: p.Icon, Category: cmp.Or(p.Category, "data-services"),
			Version: formatVersion(p.SchemaVersion), Screen: p.Screen, Website: p.Website,
		},
		Active: r.in.Detected.Active, DetectedVia: r.in.Detected.Via, APIVersion: r.apiVersion(),
		Summary: r.summary(), Cards: []TreeCard{},
	}

	// A cut list says so, and its health is never a plain ok: the objects left out are unknown.
	if r.in.Partial {
		t.Partial, t.Summary.Partial = true, true
		t.Summary.Health = worse(t.Summary.Health, "unknown")
	}

	t.Health = t.Summary.Health

	for _, card := range r.c.Cards[1:] {
		t.Cards = append(t.Cards, r.listCard(card))
	}

	t.Findings = r.findings()
	t.Errors = append(append([]TreeError{}, r.in.Errors...), r.tErrs...)

	return t
}

// apiVersion is the version of the resource the first API detection names, else of the
// first resource.
func (r *renderer) apiVersion() string {
	for _, d := range r.c.Plugin.Detect {
		if d.Group == "" {
			continue
		}

		for _, res := range r.c.Resources {
			decl := r.c.Plugin.Resources[res]
			if decl.Group == d.Group && (d.Resource == "" || d.Resource == decl.Resource) {
				return r.version(res)
			}
		}
	}

	if len(r.c.Resources) > 0 {
		return r.version(r.c.Resources[0])
	}

	return ""
}

// summary is the tile: its title, its health (the worst object's when not declared) and
// its badges.
func (r *renderer) summary() TreeSummary {
	card := r.c.Cards[0].Card
	env := expr.Env{DataServices: r.ds, Now: r.now}
	s := TreeSummary{Title: r.title(card.Title, env), Badges: []TreeBadge{}}

	if card.Health.IsZero() {
		s.Health = "ok"
		for _, objs := range r.objects {
			for _, o := range objs {
				s.Health = worse(s.Health, o.health)
			}
		}
	} else {
		s.Health = toneOf(r.eval("cards.summary.health", card.Health, env))
	}

	for i, it := range card.Badges {
		if b, ok := r.badge("cards.summary.badges["+strconv.Itoa(i)+"]", it, env); ok {
			s.Badges = append(s.Badges, b)
		}
	}

	return s
}

// title is an expression's text, or {count, one, other} with %d replaced by the count.
func (r *renderer) title(t *Title, env expr.Env) string {
	if t == nil {
		return r.c.Plugin.Name
	}

	if !t.Expr.IsZero() {
		return r.tr(r.text("cards.summary.title", t.Expr, env))
	}

	n, _ := number(r.eval("cards.summary.title.count", t.Count, env))

	form := t.Other
	if n == 1 {
		form = t.One
	}

	return strings.Replace(r.tr(form), "%d", strconv.FormatInt(int64(n), 10), 1)
}

// badge is a chip: its value (dropped at 0 with hideWhenZero), its tone (info by default),
// lit (ok, or its tone) or dimmed (idle) by on.
func (r *renderer) badge(where string, it Item, env expr.Env) (TreeBadge, bool) {
	b := TreeBadge{Label: r.tr(it.Label)}

	if !it.Value.IsZero() {
		v := r.eval(where+".value", it.Value, env)
		if n, ok := number(v); it.HideWhenZero && (v == nil || ok && n == 0) {
			return b, false
		}

		b.Value = r.display(v)
	}

	b.Tone = r.tone(where+".tone", it.Tone, env, "info")

	if !it.On.IsZero() {
		switch on := expr.Truthy(r.eval(where+".on", it.On, env)); {
		case !on:
			b.Tone = "idle"
		case it.Tone.IsZero():
			b.Tone = "ok"
		}
	}

	return b, true
}

// element is what one row shows: an object of a resource, or an element of a data service.
type element struct {
	obj *object
	env expr.Env
	ref *Ref
}

// elements are the rows of a resource or a data service; a data service that is a map is
// one row.
func (r *renderer) elements(res, ds string) []element {
	if res != "" {
		out := make([]element, 0, len(r.objects[res]))
		for _, o := range r.objects[res] {
			out = append(out, element{obj: o, env: r.objectEnv(o), ref: r.refOf(o.raw)})
		}

		return out
	}

	vals := elementsOf(r.ds[ds])
	refs := r.dsRefs[ds]
	out := make([]element, 0, len(vals))

	for i, v := range vals {
		el := element{env: expr.Env{Object: v, DataServices: r.ds, Now: r.now}}
		if i < len(refs) {
			el.ref = refs[i]
		}

		out = append(out, el)
	}

	return out
}

func (r *renderer) listCard(nc NamedCard) TreeCard {
	where := "cards." + nc.Name
	card := TreeCard{ID: nc.Name, Type: "list", Empty: r.tr(nc.Empty), Rows: []TreeRow{}}

	els := r.elements(nc.Resource, nc.DataService)
	r.sortElements(where, nc.Sort, els)

	for _, el := range els {
		card.Rows = append(card.Rows, r.row(where+".row", nc.Row, el))
	}

	return card
}

// sortElements orders the rows by the sort keys, each "expr", "expr asc" or "expr desc";
// health sorts by severity, numbers by value, the rest as text, null first.
func (r *renderer) sortElements(where string, keys []string, els []element) {
	if len(keys) == 0 {
		return
	}

	type key struct {
		e      Expr
		desc   bool
		health bool
	}

	parsed := make([]key, len(keys))

	for i, k := range keys {
		src := strings.TrimSpace(k)
		desc := strings.HasSuffix(src, " desc")
		src = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(src, " desc"), " asc"))
		parsed[i] = key{e: NewExpr(src, At{}), desc: desc, health: src == "health"}
	}

	byPos := make([][]expr.Value, len(els))
	for i := range els {
		for j, k := range parsed {
			byPos[i] = append(byPos[i], r.eval(where+".sort["+strconv.Itoa(j)+"]", k.e, els[i].env))
		}
	}

	idx := make([]int, len(els))
	for i := range idx {
		idx[i] = i
	}

	slices.SortStableFunc(idx, func(a, b int) int {
		for j, k := range parsed {
			c := compareValues(byPos[a][j], byPos[b][j], k.health)
			if k.desc {
				c = -c
			}

			if c != 0 {
				return c
			}
		}

		return 0
	})

	sorted := make([]element, len(els))
	for i, j := range idx {
		sorted[i] = els[j]
	}

	copy(els, sorted)
}

func compareValues(a, b expr.Value, health bool) int {
	if health {
		return cmp.Compare(rank(toneOf(a)), rank(toneOf(b)))
	}

	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}

	x, okA := number(a)
	y, okB := number(b)

	if okA && okB {
		return cmp.Compare(x, y)
	}

	return strings.Compare(expr.Display(a), expr.Display(b))
}

// row renders one row: an object's carries its ref and health, and its status tone is its
// health unless declared.
func (r *renderer) row(where string, row *Row, el element) TreeRow {
	out := TreeRow{Ref: el.ref, Title: r.text(where+".title", row.Title, el.env)}
	out.Subtitle = r.text(where+".subtitle", row.Subtitle, el.env)

	def := "unknown"
	if el.obj != nil {
		def = el.obj.health
		out.Health = &TreeHealth{Tone: el.obj.health, Reason: el.obj.reason, Message: el.obj.message}
	}

	if row.Status != nil {
		out.Status = &TreeStatus{
			Tone: r.tone(where+".status.tone", row.Status.Tone, el.env, def),
			Text: r.tr(r.text(where+".status.text", row.Status.Text, el.env)),
		}
	}

	if row.Trailing != nil {
		out.Trailing = &TreeTrailing{Text: r.text(where+".trailing.text", row.Trailing.Text, el.env)}
	}

	for i, s := range row.Detail {
		if sec, ok := r.section(where+".detail["+strconv.Itoa(i)+"]", s, el); ok {
			out.Detail = append(out.Detail, sec)
		}
	}

	return out
}
