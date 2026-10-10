package ichorgo

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// The cluster's events, coalesced for a live stream (StartKubeEvents): one row per object,
// reason and type, however many Event objects the API server keeps for it (kubelet starts a
// new one when a series ends), with their counts added up, the first and last time seen and
// the latest note. Rows carry a stable key, so the apps update a row rather than append one.

const (
	// kubeEventsKeep caps the rows kept in memory: past it the row seen longest ago goes.
	kubeEventsKeep = 2000
	// kubeEventsFirst is how many rows, newest first, the first batch carries.
	kubeEventsFirst = 200
	// eventGroupObjects caps the Event objects one row remembers the count of: past it the
	// oldest one's count is folded into the row's for good.
	eventGroupObjects = 64
)

// kubeEventRow is one coalesced row as the apps show it.
type kubeEventRow struct {
	// Key is stable for the row's life: a hash of the object, reason and type.
	Key       string             `json:"key"`
	Type      string             `json:"type"` // Normal or Warning
	Reason    string             `json:"reason"`
	Note      string             `json:"note"`
	Regarding kubeEventRegarding `json:"regarding"`
	Count     int                `json:"count"`
	FirstSeen int64              `json:"firstSeen"` // unix milliseconds
	LastSeen  int64              `json:"lastSeen"`
	// Source is the reporting controller (kubelet, deployment-controller…), "" when none.
	Source string `json:"source"`
}

// kubeEventRegarding is the object an event is about. Group, Version and Resource name it as
// KubeObjectSummary takes it; Resource is "" when discovery does not know the kind.
type kubeEventRegarding struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Namespaced bool   `json:"namespaced"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
}

// kubeEventsBatch is one OnEvents call: the app drops Removed, then upserts Upserts by key.
// Reset (the first batch) replaces whatever the app showed.
type kubeEventsBatch struct {
	Reset   bool           `json:"reset"`
	Upserts []kubeEventRow `json:"upserts"`
	Removed []string       `json:"removed"`
}

// eventsV1Object is an events.k8s.io/v1 Event, the newer shape of the same objects.
type eventsV1Object struct {
	Metadata struct {
		UID               string    `json:"uid"`
		Namespace         string    `json:"namespace"`
		Name              string    `json:"name"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Regarding struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
		Namespace  string `json:"namespace"`
		Name       string `json:"name"`
	} `json:"regarding"`
	Type      string     `json:"type"`
	Reason    string     `json:"reason"`
	Note      string     `json:"note"`
	EventTime *time.Time `json:"eventTime"`
	Series    *struct {
		Count            int        `json:"count"`
		LastObservedTime *time.Time `json:"lastObservedTime"`
	} `json:"series"`
	DeprecatedCount          int        `json:"deprecatedCount"`
	DeprecatedFirstTimestamp *time.Time `json:"deprecatedFirstTimestamp"`
	DeprecatedLastTimestamp  *time.Time `json:"deprecatedLastTimestamp"`
	DeprecatedSource         struct {
		Component string `json:"component"`
	} `json:"deprecatedSource"`
	ReportingController string `json:"reportingController"`
}

// coreEvent is e in the core/v1 shape mapEvent reads.
func (e eventsV1Object) coreEvent() eventObject {
	var o eventObject

	o.Metadata = e.Metadata
	o.InvolvedObject = e.Regarding
	o.Type, o.Reason, o.Message = e.Type, e.Reason, e.Note
	o.Count, o.EventTime, o.Series = e.DeprecatedCount, setTime(e.EventTime), e.Series
	o.FirstTimestamp, o.LastTimestamp = setTime(e.DeprecatedFirstTimestamp), setTime(e.DeprecatedLastTimestamp)
	o.Source.Component, o.ReportingComponent = e.DeprecatedSource.Component, e.ReportingController

	return o
}

// setTime is t, nil when unset (null, zero, or the epoch some clients write).
func setTime(t *time.Time) *time.Time {
	if t == nil || t.Unix() <= 0 {
		return nil
	}

	return t
}

// decodeEvent reads one Event of either API as the core/v1 shape.
func decodeEvent(raw json.RawMessage, eventsV1 bool) (eventObject, error) {
	if !eventsV1 {
		var o eventObject

		return o, json.Unmarshal(raw, &o)
	}

	var e eventsV1Object
	if err := json.Unmarshal(raw, &e); err != nil {
		return eventObject{}, err
	}

	return e.coreEvent(), nil
}

// eventGroup is one row and the Event objects it adds up.
type eventGroup struct {
	row kubeEventRow
	// counts is each Event object's own count, by uid; objects the uids it was first seen in.
	counts  map[string]int
	objects []string
	folded  int
	// shown: the app has the row (a batch carried it).
	shown bool
}

// eventCoalescer keeps the rows of a stream, at most limit, and what changed since the last
// batch. Not safe for concurrent use.
type eventCoalescer struct {
	groups  map[string]*eventGroup
	limit   int
	dirty   map[string]bool
	removed []string
	// dropped counts the rows let go (or never kept) to stay under limit.
	dropped int
}

func newEventCoalescer(limit int) *eventCoalescer {
	return &eventCoalescer{groups: map[string]*eventGroup{}, limit: limit, dirty: map[string]bool{}}
}

// eventKey is the row key of an event: a hash, so the privacy mask leaves it alone.
func eventKey(group, kind, namespace, name, reason, eventType string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{group, kind, namespace, name, reason, eventType}, "\x00")))

	return hex.EncodeToString(sum[:8])
}

// add counts the Event object obj (uid) into its row; regarding names the object.
func (c *eventCoalescer) add(obj eventObject, regarding kubeEventRegarding) {
	e := mapEvent(obj)
	key := eventKey(regarding.Group, e.Kind, e.Namespace, e.Name, e.Reason, e.Type)
	uid := cmp.Or(obj.Metadata.UID, obj.Metadata.Namespace+"/"+obj.Metadata.Name)

	g := c.groups[key]
	if g == nil {
		if !c.makeRoom(e.Last) {
			return
		}

		g = &eventGroup{counts: map[string]int{}, row: kubeEventRow{
			Key: key, Type: e.Type, Reason: e.Reason, Regarding: regarding, FirstSeen: e.First,
		}}
		c.groups[key] = g
	}

	before := g.row

	if _, seen := g.counts[uid]; !seen {
		g.objects = append(g.objects, uid)
	}

	g.counts[uid] = max(g.counts[uid], e.Count) // a count never goes down; a stale relist is not news
	g.foldOldest()
	g.row.Count = g.total()
	g.row.FirstSeen = min(g.row.FirstSeen, e.First)

	if e.Last >= g.row.LastSeen {
		g.row.LastSeen, g.row.Note, g.row.Source = e.Last, e.Message, e.Source
	}

	if g.row != before {
		c.dirty[key] = true
	}
}

// foldOldest keeps the count of at most eventGroupObjects objects.
func (g *eventGroup) foldOldest() {
	for len(g.objects) > eventGroupObjects {
		oldest := g.objects[0]
		g.folded += g.counts[oldest]
		delete(g.counts, oldest)
		g.objects = g.objects[1:]
	}
}

func (g *eventGroup) total() int {
	n := g.folded
	for _, count := range g.counts {
		n += count
	}

	return n
}

// makeRoom lets the row seen longest ago go when the rows are at limit, false when a row last
// seen at last would itself be that one (it is not kept).
func (c *eventCoalescer) makeRoom(last int64) bool {
	if len(c.groups) < c.limit {
		return true
	}

	var oldest *eventGroup

	for _, g := range c.groups {
		if oldest == nil || g.row.LastSeen < oldest.row.LastSeen {
			oldest = g
		}
	}

	c.dropped++

	if oldest == nil || last < oldest.row.LastSeen {
		return false
	}

	key := oldest.row.Key
	delete(c.groups, key)
	delete(c.dirty, key)

	if oldest.shown {
		c.removed = append(c.removed, key)
	}

	return true
}

// take is what changed since the last take, newest first, at most first rows when first > 0
// (the others stay unshown until they change again); ok false when nothing did.
func (c *eventCoalescer) take(first int) (kubeEventsBatch, bool) {
	if len(c.dirty) == 0 && len(c.removed) == 0 {
		return kubeEventsBatch{}, false
	}

	rows := make([]kubeEventRow, 0, len(c.dirty))
	for key := range c.dirty {
		rows = append(rows, c.groups[key].row)
	}

	slices.SortFunc(rows, func(a, b kubeEventRow) int {
		return cmp.Or(cmp.Compare(b.LastSeen, a.LastSeen), cmp.Compare(a.Key, b.Key))
	})

	if first > 0 && len(rows) > first {
		rows = rows[:first]
	}

	for _, r := range rows {
		c.groups[r.Key].shown = true
	}

	batch := kubeEventsBatch{Upserts: rows, Removed: nonNil(c.removed)}
	c.dirty, c.removed = map[string]bool{}, nil

	return batch, true
}
