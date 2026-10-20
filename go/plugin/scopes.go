package plugin

import (
	"errors"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// The scope of each place an expression is written: which names, fields, data services and
// functions it may use. Fields are evaluated in file order (a field sees the earlier ones),
// health after the fields, data services after the objects, cards and findings last.

// Aggregations exist only in data services (aggregate.go).

// relationJoin is join(resource, predicate) in a field: it returns an object, not the string
// join(list, separator) of the library.
var relationJoin = map[string]expr.Func{"join": {Min: 2, Max: 2, Returns: expr.KindUnknown}}

// fieldScope: one object, the fields declared before this one, and the resources by key for
// join(resource, predicate), whose predicate sees left (the object) and $ (a candidate).
func fieldScope(resources, earlier []string) expr.Scope {
	return expr.Scope{
		Names:  []string{"fields", "conditions", "now", "left"},
		Fields: nonNil(earlier),
		Vars:   resources,
		Funcs:  relationJoin,
	}
}

// healthScope: one object with all its fields; a rule's when is a condition.
func healthScope(fields []string) expr.Scope {
	return expr.Scope{Names: []string{"fields", "conditions", "now"}, Fields: nonNil(fields), Bool: true}
}

// dataServiceScope: aggregations over the resources and the data services declared before.
func dataServiceScope(resources, earlier []string) expr.Scope {
	return expr.Scope{
		Names:        []string{"fields", "health", "conditions", "now", "dataServices"},
		DataServices: nonNil(earlier),
		Vars:         resources,
		Funcs:        aggregations,
	}
}

// groupScope: a grouped data service's aggregations, over the objects of one group.
func groupScope(fields, earlier []string) expr.Scope {
	s := rowScope(fields, earlier)
	s.Funcs = aggregations

	return s
}

// rowScope: one object of a resource with its fields and health, or (fields nil) one
// element of a data service; the data services are in scope either way.
func rowScope(fields, dataServices []string) expr.Scope {
	if fields == nil {
		return expr.Scope{Names: []string{"now", "dataServices"}, DataServices: nonNil(dataServices)}
	}

	return expr.Scope{
		Names:        []string{"fields", "health", "conditions", "now", "dataServices"},
		Fields:       fields,
		DataServices: nonNil(dataServices),
	}
}

// refScope: a resource's ref sees one object with its fields and health; not the data
// services, which a ref asked for while they are computed would read half done.
func refScope(fields []string) expr.Scope {
	return expr.Scope{Names: []string{"fields", "health", "conditions", "now"}, Fields: nonNil(fields)}
}

// summaryScope: the summary card sees the data services only.
func summaryScope(dataServices []string) expr.Scope {
	return expr.Scope{Names: []string{"now", "dataServices"}, DataServices: nonNil(dataServices)}
}

// bodyScope: an action body's templates see the object's name and namespace, its fields,
// the parameters and now.
func bodyScope(fields, params []string) expr.Scope {
	names := []string{"now", "params"}
	if fields != nil {
		names = append(names, "fields")
	}

	return expr.Scope{Names: names, Fields: fields, Params: nonNil(params), Vars: []string{"name", "namespace"}}
}

// findingScope: a finding's title and detail see the object and the health rule that
// matched (message, reason).
func findingScope(fields, dataServices []string) expr.Scope {
	s := rowScope(fields, dataServices)
	s.Vars = []string{"name", "namespace", "message", "reason"}

	return s
}

func withBool(s expr.Scope, b bool) expr.Scope {
	s.Bool = b

	return s
}

// nonNil turns "none declared" into an empty list: a nil list would skip the check.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

func asExprError(err error, target **expr.Error) bool { return errors.As(err, target) }
