package plugin

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// Render evaluates a compiled plugin over the objects of in into the render tree: each
// object's fields (in file order) and health, the data services, the summary and list cards,
// and the findings. A value that fails to evaluate is left empty and reported in the tree's
// errors and in the returned list; the rest still renders. now is the reference of now,
// age() and relativeTime().
func Render(c *Compiled, in Input, now time.Time) (Tree, []error) {
	r := newRenderer(c, in, now)
	r.indexObjects()
	r.evalObjects()
	r.evalDataServices()

	return r.tree(), r.errs
}

// object is one listed object and what the plugin derived from it.
type object struct {
	raw     map[string]any
	res     string
	fields  map[string]expr.Value
	health  string
	reason  string
	message string
	// rule is the 1-based index of the health rule that matched, 0 when none did (Lint).
	rule int
	// ref is the object the resource's ref names, evaluated once (refDone).
	ref     *Ref
	refDone bool
}

// renderer is one Render: the objects by resource, the data services computed so far, and
// the problems met.
type renderer struct {
	c       *Compiled
	in      Input
	now     time.Time
	i18n    map[string]string
	objects map[string][]*object
	// vars are the resources' objects by key, as join() and the aggregations name them.
	vars map[string]expr.Value
	// owner maps a listed object (by its map's identity) to what was derived from it, so
	// that a joined object becomes a ref.
	owner  map[uintptr]*object
	ds     map[string]expr.Value
	dsRefs map[string][]*Ref
	tables map[string]TreeSection
	errs   []error
	failed map[string]bool
	tErrs  []TreeError
}

func newRenderer(c *Compiled, in Input, now time.Time) *renderer {
	return &renderer{
		c: c, in: in, now: now, i18n: localeMap(c.Plugin.I18n, in.Locale),
		objects: map[string][]*object{}, vars: map[string]expr.Value{}, owner: map[uintptr]*object{},
		ds: map[string]expr.Value{}, dsRefs: map[string][]*Ref{}, tables: map[string]TreeSection{},
		failed: map[string]bool{},
	}
}

func identity(m map[string]any) uintptr { return reflect.ValueOf(m).Pointer() }

// indexObjects records every object before any field is evaluated: a join may name any
// resource.
func (r *renderer) indexObjects() {
	for _, res := range r.c.Resources {
		list := r.in.Objects[res]
		raws := make([]any, 0, len(list))

		for _, raw := range list {
			if raw == nil {
				continue
			}

			o := &object{raw: raw, res: res, fields: map[string]expr.Value{}}
			r.owner[identity(raw)] = o
			r.objects[res] = append(r.objects[res], o)
			raws = append(raws, raw)
		}

		r.vars[res] = raws
	}
}

func (r *renderer) evalObjects() {
	funcs := map[string]expr.Func{"join": {Min: 2, Max: 2, Lazy: r.join}}

	for _, res := range r.c.Resources {
		for _, o := range r.objects[res] {
			env := expr.Env{Object: o.raw, Fields: o.fields, Now: r.now, Vars: r.vars, Funcs: funcs}

			for _, f := range r.c.Fields[res] {
				o.fields[f.Name] = r.eval("fields."+res+"."+f.Name, f.Expr, env)
			}

			r.evalHealth(o)
		}
	}
}

// evalHealth applies the first matching rule; an object of a resource without rules takes
// the generic reading of its conditions and phase, one no rule matches is ok.
func (r *renderer) evalHealth(o *object) {
	rules, ok := r.c.Plugin.Health[o.res]
	if !ok {
		o.health = "ok"
		if r.in.Heuristics != nil {
			o.health, o.message = r.in.Heuristics.ObjectHealth(o.raw)
		}

		return
	}

	env := expr.Env{Object: o.raw, Fields: o.fields, Now: r.now}

	for i, rule := range rules {
		where := "health." + o.res + "[" + strconv.Itoa(i) + "]"
		if !expr.Truthy(r.eval(where+".when", rule.When, env)) {
			continue
		}

		o.health, o.reason, o.rule = rule.Health, rule.Reason, i+1
		o.message = r.template(where+".message", rule.Message, env)

		return
	}

	o.health = "ok"
}

// join(resource, predicate) is the first object of resource for which predicate holds, $
// being the candidate and left the object the field belongs to; null when none. Over
// anything but a resource name it is the string join(list, separator).
func (r *renderer) join(ctx expr.Ctx, args []expr.Arg) (expr.Value, error) {
	if path := args[0].Path(); len(path) == 1 {
		if candidates, ok := r.vars[path[0]].([]any); ok {
			for _, cand := range candidates {
				env := expr.Env{Object: cand, Left: ctx.Env.Object, Fields: ctx.Env.Fields, Now: ctx.Now}

				v, err := args[1].Eval(env)
				if err != nil {
					return nil, err
				}

				if expr.Truthy(v) {
					return cand, nil
				}
			}

			return nil, nil
		}
	}

	return callLibrary(ctx, "join", args)
}

// callLibrary evaluates args and calls the library function name with them.
func callLibrary(ctx expr.Ctx, name string, args []expr.Arg) (expr.Value, error) {
	fn, _ := expr.Library(name)

	vals := make([]expr.Value, len(args))
	for i, a := range args {
		v, err := a.Value()
		if err != nil {
			return nil, err
		}

		vals[i] = v
	}

	return fn.Call(ctx, vals)
}

// objectEnv is what a card, a data service or a finding sees of one object.
func (r *renderer) objectEnv(o *object) expr.Env {
	return expr.Env{Object: o.raw, Fields: o.fields, Health: o.health, DataServices: r.ds, Now: r.now}
}

// eval evaluates e (null when absent); a failure is reported once per place and reads null.
func (r *renderer) eval(where string, e Expr, env expr.Env) expr.Value {
	if e.IsZero() || e.Node() == nil {
		return nil
	}

	v, err := e.Node().Eval(env)
	if err != nil {
		r.fail(where, err)

		return nil
	}

	return v
}

// template renders t, translated when the locale has its text.
func (r *renderer) template(where string, t Template, env expr.Env) string {
	tpl := t.Parsed()
	if tpl == nil {
		return ""
	}

	if tr := r.tr(t.Src); tr != t.Src {
		if parsed, err := expr.ParseTemplate(tr); err == nil {
			tpl = parsed
		}
	}

	s, err := tpl.Render(env)
	if err != nil {
		r.fail(where, err)
	}

	return s
}

func (r *renderer) fail(where string, err error) {
	if r.failed[where] {
		return
	}

	r.failed[where] = true
	r.errs = append(r.errs, fmt.Errorf("%s: %w", where, err))
	r.tErrs = append(r.tErrs, TreeError{Where: where, Message: err.Error()})
}

// text is an expression's display form.
func (r *renderer) text(where string, e Expr, env expr.Env) string {
	return r.display(r.eval(where, e, env))
}

// display writes a value for a person: yes or no for a bool, at most two decimals.
func (r *renderer) display(v expr.Value) string {
	switch x := expr.Normalize(v).(type) {
	case bool:
		if x {
			return r.tr("yes")
		}

		return r.tr("no")
	case float64:
		return strconv.FormatFloat(math.Round(x*100)/100, 'f', -1, 64)
	default:
		return expr.Display(x)
	}
}

// tone is a tone written as a word or an expression; def when absent.
func (r *renderer) tone(where string, t Tone, env expr.Env, def string) string {
	switch {
	case t.Literal != "":
		return t.Literal
	case !t.Expr.IsZero():
		return toneOf(r.eval(where, t.Expr, env))
	default:
		return def
	}
}

// refOf is the ref of an object listed by the plugin, nil for anything else. A resource
// with a ref opens the object it names instead (nil when it names none).
func (r *renderer) refOf(v any) *Ref {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return nil
	}

	o, ok := r.owner[identity(m)]
	if !ok {
		return nil
	}

	decl := r.c.Plugin.Resources[o.res]
	if !decl.Ref.IsZero() {
		return r.declaredRef(o, decl.Ref)
	}

	meta, _ := m["metadata"].(map[string]any)
	ref := &Ref{Group: decl.Group, Version: r.version(o.res), Resource: decl.Resource}
	ref.Namespace, _ = meta["namespace"].(string)
	ref.Name, _ = meta["name"].(string)

	return ref
}

// declaredRef evaluates a resource's ref over o once: rows, findings and relations all ask.
func (r *renderer) declaredRef(o *object, e Expr) *Ref {
	if !o.refDone {
		o.refDone = true
		o.ref = refFromMap(r.eval("resources."+o.res+".ref", e, r.objectEnv(o)))
	}

	return o.ref
}

// refFromMap reads {group, version, resource, kind, namespace, name}; nil without a name
// or a resource.
func refFromMap(v expr.Value) *Ref {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}

	str := func(key string) string {
		s, _ := m[key].(string)

		return s
	}

	ref := &Ref{Group: str("group"), Version: str("version"), Resource: str("resource"), Kind: str("kind"), Namespace: str("namespace"), Name: str("name")}
	if ref.Name == "" || ref.Resource == "" {
		return nil
	}

	return ref
}

// version is the version a resource was read at: the resolved one, else the declared one
// ("" for preferred, unresolved in the demo).
func (r *renderer) version(res string) string {
	if v := r.in.APIVersions[res]; v != "" {
		return v
	}

	if v := r.c.Plugin.Resources[res].Version; v != "preferred" {
		return v
	}

	return ""
}

// tr translates a label; a label the locale lacks stays English.
func (r *renderer) tr(s string) string {
	if t, ok := r.i18n[s]; ok && t != "" {
		return t
	}

	return s
}

// Translate is s in the plugin's locale map (a tool's texts); a text it lacks stays English.
func (c *Compiled) Translate(locale, s string) string {
	if t := localeMap(c.Plugin.I18n, locale)[s]; t != "" {
		return t
	}

	return s
}

// localeMap is the i18n map of locale, else of its language ("pt" for "pt-BR").
func localeMap(i18n map[string]map[string]string, locale string) map[string]string {
	if m, ok := i18n[locale]; ok {
		return m
	}

	if lang, _, ok := strings.Cut(locale, "-"); ok {
		return i18n[lang]
	}

	return nil
}

// number reads a value as a number.
func number(v expr.Value) (float64, bool) {
	switch x := expr.Normalize(v).(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	default:
		return 0, false
	}
}
