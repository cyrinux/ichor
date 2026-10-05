package ichorgo

import (
	"cmp"
	"slices"
	"strings"
)

// The kinds of auditFinding: a pattern in what one actor does that loads the API server or
// shows something broken. The apps word each with its likely cause and fix.
const (
	findThrottled    = "throttled"     // the server answered 429: it sends more than its share
	findHotClient    = "hotClient"     // a large share of all requests
	findListLoop     = "listLoop"      // lists the same resource again and again instead of watching
	findWatchChurn   = "watchChurn"    // its watches keep ending and starting over
	findForbidden    = "forbidden"     // keeps being refused (403): missing RBAC
	findMissingAPI   = "missingAPI"    // lists or watches a resource the cluster no longer serves
	findMissingObj   = "missingObject" // keeps reading an object that does not exist
	findConflicts    = "conflicts"     // its updates keep conflicting (409): another writer
	findExists       = "alreadyExists" // keeps creating objects that exist (409 on create): a reconcile bug
	findHotObject    = "hotObject"     // rewrites one object again and again
	findEventSpam    = "eventSpam"     // creates events at a high rate
	findSlow         = "slow"          // its requests take seconds
	findServerErrors = "serverErrors"  // gets 5xx answers: a webhook or aggregated API failing
	// The same symptom for many actors: the API server or etcd is the problem, not them.
	findWideErrors = "widespreadErrors"
	findWideSlow   = "widespreadSlow"
	// A control plane's audit log stopped well before the others' (Name: the node, Value:
	// how long ago, in seconds).
	findStaleLog = "staleLog"
	// Refused for its credentials (401) again and again: an expired or deleted token.
	findUnauthorized = "unauthorized"
	// Many actors' watches end early and start over, at about the same interval (Value):
	// something between the clients and the API server cuts long connections.
	findWideWatch = "widespreadWatchChurn"
)

const (
	sevCritical = "critical"
	sevWarning  = "warning"
	sevInfo     = "info"
)

// auditActor is who sent requests, as the apps show it: a service account's namespace and
// name, a node, a control-plane component or a person.
type auditActor struct {
	User      string `json:"user"`
	Agent     string `json:"agent"`
	Kind      string `json:"kind"` // serviceAccount, node, controlPlane, user, anonymous
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// auditFinding is one problem, with its evidence. Value depends on the kind: the share of
// all requests in percent (hotClient), the mean interval in seconds (listLoop, watchChurn,
// hotObject) or the mean latency in milliseconds (slow).
type auditFinding struct {
	Kind      string     `json:"kind"`
	Severity  string     `json:"severity"`
	Actor     auditActor `json:"actor"`
	Count     int        `json:"count"`
	Rate      float64    `json:"rate"` // per second over the window
	Verb      string     `json:"verb,omitempty"`
	Resource  string     `json:"resource,omitempty"`
	Namespace string     `json:"namespace,omitempty"`
	Name      string     `json:"name,omitempty"`
	Value     float64    `json:"value,omitempty"`
	Code      int        `json:"code,omitempty"`   // serverErrors: the most frequent 5xx
	Actors    int        `json:"actors,omitempty"` // widespread*: how many actors have it
	// Objects is how many objects or namespaces the actor does it on, when more than one;
	// Examples names the worst few ("namespace/name", or the namespace).
	Objects  int      `json:"objects,omitempty"`
	Examples []string `json:"examples,omitempty"`
}

// The thresholds, per window: what a well-behaved client stays below.
const (
	hotClientShare   = 0.25 // of all requests,
	hotClientRate    = 5.0  // and at least this many per second
	listLoopInterval = 30.0 // seconds between lists of the same thing, or less
	listLoopCritical = 10.0
	loopMinCount     = 10  // 403, 404, 409, 5xx answers before they count as a loop
	hotObjectPerMin  = 6.0 // writes to one object per minute (leases renew every few seconds)
	leaseWritesPerS  = 1.0
	eventSpamRate    = 1.0    // events created per second
	slowMs           = 1000.0 // mean latency
	slowCriticalMs   = 5000.0
	slowMinCount     = 10
	auditMaxFindings = 40
)

// detect finds the problems of one actor over seconds of log.
func detect(actor auditActor, s *auditStats, total int, seconds float64) []auditFinding {
	if seconds < 60 {
		seconds = 60 // a minute at least: rates over a few seconds would be noise
	}

	f := findingMaker{actor: actor, seconds: seconds}

	f.client(s, total)

	for op, stats := range s.Ops {
		f.op(op, stats)
	}

	for obj, n := range s.Mutations {
		f.mutations(obj, n)
	}

	for obj, n := range s.Missing {
		if n >= 2*loopMinCount {
			f.add(findMissingObj, sevInfo, n, "get", obj, 0)
		}
	}

	return f.out
}

type findingMaker struct {
	actor   auditActor
	seconds float64
	out     []auditFinding
}

func (f *findingMaker) add(kind, severity string, count int, verb string, obj objKey, value float64) {
	f.out = append(f.out, auditFinding{
		Kind: kind, Severity: severity, Actor: f.actor, Count: count, Rate: float64(count) / f.seconds,
		Verb: verb, Resource: obj.Resource, Namespace: obj.Namespace, Name: obj.Name, Value: value,
	})
}

// client looks at the actor as a whole: its share, whether it was throttled or refused
// for its credentials, and the events it writes.
func (f *findingMaker) client(s *auditStats, total int) {
	throttled, unauthorized, events := 0, 0, 0

	for op, stats := range s.Ops {
		throttled += stats.Codes[429]
		unauthorized += stats.Codes[401]

		if base, _, _ := strings.Cut(op.Resource, "/"); (base == "events" || base == "events.events.k8s.io") && (op.Verb == "create" || op.Verb == "patch") {
			events += stats.Count
		}
	}

	if throttled > 0 {
		f.add(findThrottled, severityAt(throttled, loopMinCount, sevWarning, sevCritical), throttled, "", objKey{}, 0)
	}

	if unauthorized >= loopMinCount {
		f.add(findUnauthorized, sevWarning, unauthorized, "", objKey{}, 0)
	}

	// Creates and the patches that count repeats: an event recorded again and again.
	if float64(events)/f.seconds >= eventSpamRate {
		f.add(findEventSpam, sevWarning, events, "create", objKey{Resource: "events"}, 0)
	}

	rate := float64(s.Requests) / f.seconds
	if total > 0 && float64(s.Requests) >= hotClientShare*float64(total) && rate >= hotClientRate {
		top := topOp(s)
		f.add(findHotClient, sevInfo, s.Requests, top.Verb, objKey{Resource: top.Resource, Namespace: top.Namespace},
			100*float64(s.Requests)/float64(total))
	}
}

// metricsAPI is a resource served by the metrics APIs, which the autoscalers list on a
// timer by design (every 15 s for the HPA).
func metricsAPI(resource string) bool {
	base, _, _ := strings.Cut(resource, "/")

	return strings.HasSuffix(base, "metrics.k8s.io")
}

// op looks at one kind of request: loops of lists, watches that end early, and failing
// answers.
func (f *findingMaker) op(op opKey, s *opStats) {
	obj := objKey{Resource: op.Resource, Namespace: op.Namespace}
	// Per client address: the replicas of a DaemonSet share the actor, each lists on its own.
	interval := f.seconds * float64(max(1, len(s.Sources))) / float64(s.Count)

	switch {
	case op.Verb == "list" && !metricsAPI(op.Resource) && interval <= listLoopInterval && s.Count >= 5 && s.Codes[200] > 0:
		severity := sevWarning
		if op.Namespace == "" && interval <= listLoopCritical {
			severity = sevCritical
		}

		f.add(findListLoop, severity, s.Count, op.Verb, obj, interval)
	case op.Verb == "watch" && s.ShortWatches >= loopMinCount && 2*s.ShortWatches >= s.WatchEnds:
		// Value: how long its watches last; client-go's last 5 to 10 minutes.
		f.add(findWatchChurn, sevWarning, s.ShortWatches, op.Verb, obj, s.ShortSeconds/float64(s.ShortWatches))
	}

	f.failures(op, obj, s)

	if s.Timed >= slowMinCount {
		if mean := s.LatencyMs / float64(s.Timed); mean >= slowMs {
			f.add(findSlow, severityAt(int(mean), int(slowCriticalMs), sevWarning, sevCritical), s.Timed, op.Verb, obj, mean)
		}
	}
}

// failures reports the answers that keep failing the same way.
func (f *findingMaker) failures(op opKey, obj objKey, s *opStats) {
	if n := s.Codes[403]; n >= loopMinCount {
		f.add(findForbidden, sevWarning, n, op.Verb, obj, 0)
	}

	if n := s.Codes[404]; n >= loopMinCount && (op.Verb == "list" || op.Verb == "watch") {
		f.add(findMissingAPI, sevWarning, n, op.Verb, obj, 0)
	}

	if n := s.Codes[409]; n >= loopMinCount {
		kind := findConflicts
		if op.Verb == "create" {
			kind = findExists
		}

		f.add(kind, sevWarning, n, op.Verb, obj, 0)
	}

	errors := 0
	fives := map[int]int{}

	for code, n := range s.Codes {
		if code >= 500 {
			errors += n
			fives[code] = n
		}
	}

	if errors >= loopMinCount {
		f.add(findServerErrors, sevCritical, errors, op.Verb, obj, 0)
		f.out[len(f.out)-1].Code = topKey(fives, func(a, b int) bool { return a < b })
	}
}

// mutations reports an object rewritten far more often than a controller needs to.
func (f *findingMaker) mutations(obj objKey, n int) {
	perMin := float64(n) / (f.seconds / 60)
	// Leader election and node heartbeats renew leases every few seconds by design.
	lease := obj.Resource == "leases" || obj.Resource == "leases.coordination.k8s.io"

	if (!lease && perMin >= hotObjectPerMin && n >= loopMinCount) || (lease && float64(n)/f.seconds >= leaseWritesPerS) {
		f.add(findHotObject, sevWarning, n, "update", obj, f.seconds/float64(n))
	}
}

func severityAt(n, critical int, below, above string) string {
	if n >= critical {
		return above
	}

	return below
}

// topOp is the actor's most frequent kind of request.
func topOp(s *auditStats) opKey {
	var (
		best  opKey
		count int
	)

	for op, stats := range s.Ops {
		if stats.Count > count || stats.Count == count && op.Resource < best.Resource {
			best, count = op, stats.Count
		}
	}

	return best
}

// describeActor turns a username and user agent into what the apps show.
func describeActor(user, agent, address string) auditActor {
	a := auditActor{User: user, Agent: agent, Kind: "user", Name: user}

	switch {
	case user == "" && address != "":
		// Refused before it was known: its address is all there is.
		a.Kind, a.Name = "unauthenticated", address
	case user == "":
		a.Kind, a.Name = "others", "…"
	case strings.HasPrefix(user, "system:serviceaccount:"):
		parts := strings.SplitN(strings.TrimPrefix(user, "system:serviceaccount:"), ":", 2)
		if len(parts) == 2 {
			a.Kind, a.Namespace, a.Name = "serviceAccount", parts[0], parts[1]
		}
	case strings.HasPrefix(user, "system:node:"):
		a.Kind, a.Name = "node", strings.TrimPrefix(user, "system:node:")
	case user == "system:anonymous":
		a.Kind = "anonymous"
	case strings.HasPrefix(user, "system:"):
		a.Kind, a.Name = "controlPlane", strings.TrimPrefix(user, "system:")
	}

	return a
}

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
