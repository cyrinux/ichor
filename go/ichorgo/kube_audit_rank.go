package ichorgo

import (
	"cmp"
	"slices"
	"strings"
)

// Ordering, merging and wording the findings detect produced, for the report.

var severityRank = map[string]int{sevCritical: 0, sevWarning: 1, sevInfo: 2}

// rankFindings puts the worst first, then the busiest, and keeps auditMaxFindings.
func rankFindings(findings []auditFinding) []auditFinding {
	slices.SortFunc(findings, func(a, b auditFinding) int {
		return cmp.Or(
			cmp.Compare(severityRank[a.Severity], severityRank[b.Severity]),
			// The server's problems first: they explain the clients'.
			cmp.Compare(serverRank(b), serverRank(a)),
			cmp.Compare(b.Count, a.Count),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Actor.User, b.Actor.User),
			cmp.Compare(a.Resource, b.Resource),
			cmp.Compare(a.Name, b.Name),
		)
	})

	return findings[:min(len(findings), auditMaxFindings)]
}

// wideActors is how many actors must show a symptom for it to be the server's.
const wideActors = 4

// widespread replaces the server errors or slow requests that many actors share by one
// finding about the API server: when kubelets, controllers and operators all get 5xx on
// their lease renewals, etcd or the API server is struggling, they are its victims.
func widespread(findings []auditFinding) []auditFinding {
	out := make([]auditFinding, 0, len(findings))
	groups := map[string][]auditFinding{findServerErrors: nil, findSlow: nil, findWatchChurn: nil}

	for _, f := range findings {
		if _, ok := groups[f.Kind]; ok {
			groups[f.Kind] = append(groups[f.Kind], f)
		} else {
			out = append(out, f)
		}
	}

	for kind, wide := range map[string]string{findServerErrors: findWideErrors, findSlow: findWideSlow, findWatchChurn: findWideWatch} {
		if distinctActors(groups[kind]) < wideAt(kind) {
			out = append(out, groups[kind]...)
		} else {
			out = append(out, summarizeWide(wide, groups[kind]))
		}
	}

	return out
}

func distinctActors(findings []auditFinding) int {
	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.Actor.User+"\x00"+f.Actor.Agent] = true
	}

	return len(seen)
}

// summarizeWide sums findings into one about the server: the total, the actors, the most
// affected verb and resource, the most frequent code, the mean latency.
func summarizeWide(kind string, findings []auditFinding) auditFinding {
	sum := auditFinding{Kind: kind, Severity: sevCritical, Actors: distinctActors(findings), Examples: actorExamples(findings)}
	if kind == findWideWatch {
		sum.Severity = sevWarning
	}
	ops, codes := map[opKey]int{}, map[int]int{}

	var weighted float64

	for _, f := range findings {
		sum.Count += f.Count
		sum.Rate += f.Rate
		weighted += f.Value * float64(f.Count)
		ops[opKey{Verb: f.Verb, Resource: f.Resource}] += f.Count
		codes[f.Code] += f.Count
	}

	top := topKey(ops, func(a, b opKey) bool { return a.Resource+" "+a.Verb < b.Resource+" "+b.Verb })
	sum.Verb, sum.Resource = top.Verb, top.Resource
	sum.Code = topKey(codes, func(a, b int) bool { return a < b })

	if (kind == findWideSlow || kind == findWideWatch) && sum.Count > 0 {
		sum.Value = weighted / float64(sum.Count)
	}

	return sum
}

// topKey is the key with the largest count, ties broken by less.
func topKey[K comparable](counts map[K]int, less func(a, b K) bool) K {
	var (
		best  K
		count int
	)

	for k, n := range counts {
		if n > count || n == count && less(k, best) {
			best, count = k, n
		}
	}

	return best
}

// auditMaxExamples is how many objects a grouped finding names.
const auditMaxExamples = 3

// intervalKinds have Value as the mean interval between requests, for one object each.
var intervalKinds = map[string]bool{findListLoop: true, findWatchChurn: true, findHotObject: true}

// groupByActor merges the findings one actor has of one kind on one resource across
// namespaces and objects: an operator rewriting the status of 14 clusters is one problem,
// told once with how many and the worst few.
func groupByActor(findings []auditFinding) []auditFinding {
	type key struct{ kind, user, agent, verb, resource string }

	groups := map[key][]auditFinding{}
	order := []key{}

	for _, f := range findings {
		k := key{f.Kind, f.Actor.User, f.Actor.Agent, f.Verb, f.Resource}
		if f.Kind == findWatchChurn {
			k.resource = "" // an informer restarts all its watches at once: one finding
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}

		groups[k] = append(groups[k], f)
	}

	out := make([]auditFinding, 0, len(order))

	for _, k := range order {
		group := groups[k]
		if len(group) == 1 || k.kind == findStaleLog {
			out = append(out, group...)
		} else {
			out = append(out, mergeGroup(group))
		}
	}

	return out
}

func mergeGroup(group []auditFinding) auditFinding {
	slices.SortFunc(group, func(a, b auditFinding) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(example(a), example(b)))
	})

	merged := group[0]
	merged.Count, merged.Rate, merged.Value = 0, 0, 0
	merged.Namespace, merged.Name = "", ""
	merged.Objects = len(group)

	var weighted, intervals float64

	for i, f := range group {
		merged.Count += f.Count
		merged.Rate += f.Rate
		weighted += f.Value * float64(f.Count)
		intervals += f.Value

		if severityRank[f.Severity] < severityRank[merged.Severity] {
			merged.Severity = f.Severity
		}

		if i < auditMaxExamples {
			merged.Examples = append(merged.Examples, example(f))
		}
	}

	switch {
	case intervalKinds[merged.Kind]:
		merged.Value = intervals / float64(len(group)) // each object's own interval: "every ~9 s each"
	case merged.Count > 0:
		merged.Value = weighted / float64(merged.Count)
	}

	return merged
}

// example names what a grouped finding is about: the resource for watches restarting, the
// object or namespace otherwise.
func example(f auditFinding) string {
	if f.Kind == findWatchChurn {
		return strings.Trim(f.Namespace+"/"+f.Resource, "/")
	}

	return strings.Trim(f.Namespace+"/"+f.Name, "/")
}

// wideAt is how many actors must share a symptom for it to be the server's. Watches cut
// early are rare enough that three clients at once point at the path to the server.
func wideAt(kind string) int {
	if kind == findWatchChurn {
		return 3
	}

	return wideActors
}

// serverRank is 1 for a finding about the API server rather than one client.
func serverRank(f auditFinding) int {
	switch f.Kind {
	case findWideErrors, findWideSlow, findWideWatch, findStaleLog:
		return 1
	}

	return 0
}

// actorExamples names the clients most affected, auditMaxExamples at most.
func actorExamples(findings []auditFinding) []string {
	byActor := map[string]int{}
	for _, f := range findings {
		byActor[actorLabel(f.Actor)] += f.Count
	}

	labels := make([]string, 0, len(byActor))
	for label := range byActor {
		labels = append(labels, label)
	}

	slices.SortFunc(labels, func(a, b string) int { return cmp.Or(cmp.Compare(byActor[b], byActor[a]), cmp.Compare(a, b)) })

	return labels[:min(len(labels), auditMaxExamples)]
}

// actorLabel is how the apps name an actor: "namespace/name" for a service account.
func actorLabel(a auditActor) string {
	if a.Kind == "serviceAccount" {
		return a.Namespace + "/" + a.Name
	}

	return a.Name
}
