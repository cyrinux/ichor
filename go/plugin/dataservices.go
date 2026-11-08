package plugin

import (
	"cmp"
	"slices"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// evalDataServices computes the data services in file order, each seeing the earlier ones:
// an aggregate is a map of its values (and health), a grouped one a list of one map per key
// (key, its values, health), a projection a list of the selected values of each kept object.
func (r *renderer) evalDataServices() {
	for _, ds := range r.c.DataServices {
		where := "dataServices." + ds.Name

		switch {
		case ds.From == "":
			r.ds[ds.Name] = r.aggregateService(where, ds)
		case !ds.GroupBy.IsZero():
			r.ds[ds.Name] = r.groupService(where, ds)
		default:
			r.ds[ds.Name], r.dsRefs[ds.Name] = r.selectService(where, ds)
		}
	}
}

func (r *renderer) aggregateService(where string, ds NamedDataService) map[string]any {
	return r.aggregateInto(where, ds, r.dataServiceEnv(), map[string]any{})
}

// dataServiceEnv is what an aggregate sees: the resources by key, the data services so far.
func (r *renderer) dataServiceEnv() expr.Env {
	return expr.Env{DataServices: r.ds, Now: r.now, Vars: r.vars, Funcs: aggregationFuncs(r, nil)}
}

// aggregateInto evaluates the aggregates and the health of ds into out.
func (r *renderer) aggregateInto(where string, ds NamedDataService, env expr.Env, out map[string]any) map[string]any {
	for _, key := range sortedKeys(ds.Aggregate) {
		out[key] = r.eval(where+".aggregate."+key, ds.Aggregate[key], env)
	}

	if !ds.Health.IsZero() {
		out["health"] = toneOf(r.eval(where+".health", ds.Health, env))
	}

	return out
}

// groupService groups the objects of ds.From by the display form of groupBy, sorted by key.
func (r *renderer) groupService(where string, ds NamedDataService) []any {
	type group struct {
		key  expr.Value
		objs []*object
	}

	byKey := map[string]*group{}

	for _, o := range r.objects[ds.From] {
		key := r.eval(where+".groupBy", ds.GroupBy, r.objectEnv(o))

		k := expr.Display(key)
		if byKey[k] == nil {
			byKey[k] = &group{key: key}
		}

		byKey[k].objs = append(byKey[k].objs, o)
	}

	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}

	slices.SortFunc(keys, cmp.Compare)

	out := make([]any, 0, len(keys))

	for _, k := range keys {
		g := byKey[k]
		env := expr.Env{DataServices: r.ds, Now: r.now, Funcs: aggregationFuncs(r, g.objs)}
		out = append(out, r.aggregateInto(where, ds, env, map[string]any{"key": g.key}))
	}

	return out
}

// selectService keeps the objects of ds.From where holds and projects each; the refs are the
// kept objects', so that a row of the projection still opens its object.
func (r *renderer) selectService(where string, ds NamedDataService) ([]any, []*Ref) {
	out, refs := []any{}, []*Ref{}

	for _, o := range r.objects[ds.From] {
		env := r.objectEnv(o)
		if !ds.Where.IsZero() && !expr.Truthy(r.eval(where+".where", ds.Where, env)) {
			continue
		}

		row := make(map[string]any, len(ds.Select))
		for _, key := range sortedKeys(ds.Select) {
			row[key] = r.eval(where+".select."+key, ds.Select[key], env)
		}

		out, refs = append(out, row), append(refs, r.refOf(o.raw))
	}

	return out, refs
}
