package ichorgo

import (
	"net/url"
	"strings"
	"time"
)

// auditEvent is the part of a kube-apiserver audit event (audit.k8s.io/v1, one JSON object
// per line) the analysis reads. Talos logs every request at the Metadata level: who, what,
// the answer's code and when, never the bodies.
type auditEvent struct {
	Stage      string `json:"stage"`
	Verb       string `json:"verb"`
	UserAgent  string `json:"userAgent"`
	RequestURI string `json:"requestURI"`
	User       struct {
		Username string `json:"username"`
	} `json:"user"`
	ImpersonatedUser *struct {
		Username string `json:"username"`
	} `json:"impersonatedUser"`
	SourceIPs []string `json:"sourceIPs"`
	ObjectRef *struct {
		Resource    string `json:"resource"`
		Namespace   string `json:"namespace"`
		Name        string `json:"name"`
		Subresource string `json:"subresource"`
		APIGroup    string `json:"apiGroup"`
	} `json:"objectRef"`
	ResponseStatus *struct {
		Code int `json:"code"`
	} `json:"responseStatus"`
	Received time.Time `json:"requestReceivedTimestamp"`
	StageAt  time.Time `json:"stageTimestamp"`
}

// countable tells whether the event is the one counting its request: every request is
// logged at several stages. A watch counts when it starts: the API server always logs that
// stage for it, refused or not. A request refused for its credentials (401) is only logged
// at that stage.
func (e auditEvent) countable() bool {
	switch e.Stage {
	case "Panic":
		return true
	case "ResponseStarted":
		return e.Verb == "watch" || e.code() == 401
	case "ResponseComplete":
		return e.Verb != "watch"
	}

	return false
}

// watchLifetime is how long a watch that ran lasted, when the event is its end.
func (e auditEvent) watchLifetime() (time.Duration, bool) {
	if e.Stage != "ResponseComplete" || e.Verb != "watch" || e.code() >= 400 || e.Received.IsZero() || e.StageAt.IsZero() {
		return 0, false
	}

	return e.StageAt.Sub(e.Received), true
}

// streaming requests stay open as long as a session or a followed log: their duration says
// nothing about the API server's speed.
func (e auditEvent) streaming() bool {
	if e.Verb == "proxy" {
		return true
	}

	if e.ObjectRef == nil {
		return false
	}

	switch e.ObjectRef.Subresource {
	case "log", "exec", "attach", "portforward", "proxy":
		return true
	}

	return false
}

func (e auditEvent) code() int {
	if e.Stage == "Panic" {
		return 500
	}

	if e.ResponseStatus == nil {
		return 0
	}

	return e.ResponseStatus.Code
}

// opKey is one kind of request: a verb on a resource ("pods/log", "pods.metrics.k8s.io",
// or a path such as /healthz) in a namespace, "" for cluster-wide.
type opKey struct {
	Verb, Resource, Namespace string
}

// objKey is one object.
type objKey struct {
	Resource, Namespace, Name string
}

type opStats struct {
	Count     int
	Codes     map[int]int
	LatencyMs float64 // summed over Timed requests
	Timed     int
	Sources   map[string]bool // client addresses: the replicas of a DaemonSet share one actor
	// Watches that ran and ended, and those that ended within auditShortWatch (their
	// summed lifetime in seconds).
	WatchEnds, ShortWatches int
	ShortSeconds            float64
}

// auditStats is what one actor did over some minutes.
type auditStats struct {
	Requests  int
	Ops       map[opKey]*opStats
	Mutations map[objKey]int // updates and patches per object
	Missing   map[objKey]int // gets answered 404 per object
}

// Bounds, so a client touching every object of a large cluster, or a scan from many
// addresses, stays cheap.
const (
	auditMaxOps     = 2000
	auditMaxObjects = 2000
	auditMaxSources = 256
	auditMaxActors  = 500 // per minute; more are counted together as auditOthers
	auditOthers     = "\x00\x00"
	// auditShortWatch: client-go watches last 5 to 10 minutes, the API server's longer.
	auditShortWatch = 2 * time.Minute
)

func newAuditStats() *auditStats {
	return &auditStats{Ops: map[opKey]*opStats{}, Mutations: map[objKey]int{}, Missing: map[objKey]int{}}
}

func newOpStats() *opStats { return &opStats{Codes: map[int]int{}, Sources: map[string]bool{}} }

// op is the stats of the event's kind of request, nil past auditMaxOps.
func (s *auditStats) op(e auditEvent) (*opStats, objKey) {
	op, obj := e.keys()
	stats := s.Ops[op]

	if stats == nil && len(s.Ops) < auditMaxOps {
		stats = newOpStats()
		s.Ops[op] = stats
	}

	return stats, obj
}

// add counts one request.
func (s *auditStats) add(e auditEvent) {
	s.Requests++

	stats, obj := s.op(e)
	if stats == nil {
		return
	}

	stats.Count++
	stats.Codes[e.code()]++

	if len(e.SourceIPs) > 0 && len(stats.Sources) < auditMaxSources {
		stats.Sources[e.SourceIPs[0]] = true
	}

	if e.Verb != "watch" && !e.streaming() && e.Stage == "ResponseComplete" && !e.StageAt.IsZero() && !e.Received.IsZero() {
		stats.LatencyMs += float64(e.StageAt.Sub(e.Received).Microseconds()) / 1000
		stats.Timed++
	}

	base, _, _ := strings.Cut(obj.Resource, "/")

	switch {
	case obj.Name == "" || e.code() >= 400 && e.code() != 404:
	case (e.Verb == "update" || e.Verb == "patch") && base != "events" && base != "events.events.k8s.io":
		// Events are patched to count repeats: that is eventSpam's business.
		addCapped(s.Mutations, obj, 1)
	case e.Verb == "get" && e.code() == 404:
		addCapped(s.Missing, obj, 1)
	}
}

// watchEnded records a watch that ran and ended after lifetime.
func (s *auditStats) watchEnded(e auditEvent, lifetime time.Duration) {
	stats, _ := s.op(e)
	if stats == nil {
		return
	}

	stats.WatchEnds++

	if lifetime < auditShortWatch {
		stats.ShortWatches++
		stats.ShortSeconds += lifetime.Seconds()
	}
}

func addCapped(m map[objKey]int, k objKey, n int) {
	if _, ok := m[k]; ok || len(m) < auditMaxObjects {
		m[k] += n
	}
}

// keys names the event's kind of request and object. The API group sets apart resources
// of the same name ("pods.metrics.k8s.io"); a request for no resource is named by its path.
func (e auditEvent) keys() (opKey, objKey) {
	if e.ObjectRef == nil {
		path := e.RequestURI
		if u, err := url.Parse(e.RequestURI); err == nil {
			path = u.Path
		}

		return opKey{Verb: e.Verb, Resource: path}, objKey{Resource: path}
	}

	resource := e.ObjectRef.Resource
	if e.ObjectRef.APIGroup != "" {
		resource += "." + e.ObjectRef.APIGroup
	}

	if e.ObjectRef.Subresource != "" {
		resource += "/" + e.ObjectRef.Subresource
	}

	return opKey{e.Verb, resource, e.ObjectRef.Namespace}, objKey{resource, e.ObjectRef.Namespace, e.ObjectRef.Name}
}

// merge adds o into s.
func (s *auditStats) merge(o *auditStats) {
	s.Requests += o.Requests

	for k, v := range o.Ops {
		stats := s.Ops[k]
		if stats == nil {
			stats = newOpStats()
			s.Ops[k] = stats
		}

		stats.Count += v.Count
		stats.LatencyMs += v.LatencyMs
		stats.Timed += v.Timed
		stats.WatchEnds += v.WatchEnds
		stats.ShortWatches += v.ShortWatches
		stats.ShortSeconds += v.ShortSeconds

		for code, n := range v.Codes {
			stats.Codes[code] += n
		}

		for ip := range v.Sources {
			if len(stats.Sources) < auditMaxSources {
				stats.Sources[ip] = true
			}
		}
	}

	// Summed whole: a cap here would drop, at random, the object a client starts rewriting
	// late in the window. Each minute is capped already.
	for k, n := range o.Mutations {
		s.Mutations[k] += n
	}

	for k, n := range o.Missing {
		s.Missing[k] += n
	}
}

// actorKey names who sent a request: the user (the impersonated one, when a proxy acts for
// it), and the program (the user agent's first word) since one service account can run
// several. A request refused for its credentials has no user: its address names it.
func actorKey(e auditEvent) string {
	user := e.User.Username
	if e.ImpersonatedUser != nil && e.ImpersonatedUser.Username != "" {
		user = e.ImpersonatedUser.Username
	}

	address := ""
	if user == "" && len(e.SourceIPs) > 0 {
		address = e.SourceIPs[0]
	}

	return user + "\x00" + agentName(e.UserAgent) + "\x00" + address
}

// agentName is "kubectl" for "kubectl/v1.34.1 (linux/amd64) kubernetes/abc".
func agentName(ua string) string {
	name, _, _ := strings.Cut(ua, "/")
	name, _, _ = strings.Cut(name, " ")

	return name
}

// auditWindow keeps the last minutes of an audit log read from its start: one bucket per
// minute, the oldest dropped as newer ones arrive, so memory stays bounded whatever the
// file's size.
type auditWindow struct {
	minutes int64
	latest  int64 // newest minute seen
	buckets map[int64]map[string]*auditStats
	first   map[int64]time.Time
	last    time.Time
}

func newAuditWindow(minutes int) *auditWindow {
	return &auditWindow{minutes: int64(minutes), buckets: map[int64]map[string]*auditStats{}, first: map[int64]time.Time{}}
}

func (w *auditWindow) add(e auditEvent) {
	lifetime, ended := e.watchLifetime()

	switch {
	case e.countable() && !e.Received.IsZero():
		if stats := w.statsAt(e, e.Received); stats != nil {
			stats.add(e)
		}
	case ended:
		// Counted when it ended: a watch opened before the window may end inside it.
		if stats := w.statsAt(e, e.StageAt); stats != nil {
			stats.watchEnded(e, lifetime)
		}
	}
}

// statsAt is the stats of the event's actor in the minute of at, nil when that minute is
// out of the window. A minute holds auditMaxActors at most, the others counted together.
func (w *auditWindow) statsAt(e auditEvent, at time.Time) *auditStats {
	minute := at.Unix() / 60
	if minute > w.latest {
		w.latest = minute
		w.alignTo(minute)
	}

	if minute <= w.latest-w.minutes {
		return nil
	}

	bucket := w.buckets[minute]
	if bucket == nil {
		bucket = map[string]*auditStats{}
		w.buckets[minute] = bucket
	}

	key := actorKey(e)
	if bucket[key] == nil {
		if len(bucket) >= auditMaxActors {
			key = auditOthers
		}

		if bucket[key] == nil {
			bucket[key] = newAuditStats()
		}
	}

	if t, ok := w.first[minute]; !ok || at.Before(t) {
		w.first[minute] = at
	}

	if at.After(w.last) {
		w.last = at
	}

	return bucket[key]
}

// covered tells whether the kept minutes reach back to the window's first: an older log
// would add nothing.
func (w *auditWindow) covered() bool {
	earliest := int64(0)

	for minute := range w.first {
		if earliest == 0 || minute < earliest {
			earliest = minute
		}
	}

	return earliest != 0 && earliest <= w.latest-w.minutes+1
}

// alignTo drops the minutes older than the window ending at latest: the nodes' windows
// must cover the same minutes, though one node's log may stop earlier than the others.
func (w *auditWindow) alignTo(latest int64) {
	for m := range w.buckets {
		if m <= latest-w.minutes {
			delete(w.buckets, m)
			delete(w.first, m)
		}
	}
}

// actors sums the kept minutes per actor, with the span they cover.
func (w *auditWindow) actors() (map[string]*auditStats, time.Time, time.Time) {
	out := map[string]*auditStats{}

	var from, to time.Time

	if len(w.buckets) > 0 {
		to = w.last // after alignTo, a node whose log stopped long ago has no minute left
	}

	for minute, bucket := range w.buckets {
		if t := w.first[minute]; from.IsZero() || t.Before(from) {
			from = t
		}

		for key, stats := range bucket {
			if out[key] == nil {
				out[key] = newAuditStats()
			}

			out[key].merge(stats)
		}
	}

	return out, from, to
}
