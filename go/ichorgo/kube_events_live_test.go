package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// eventsRecorder is a KubeEventsListener for tests.
type eventsRecorder struct {
	batches  chan kubeEventsBatch
	statuses chan kubeEventsStatus
	done     chan string
}

func newEventsRecorder() *eventsRecorder {
	return &eventsRecorder{batches: make(chan kubeEventsBatch, 64), statuses: make(chan kubeEventsStatus, 64), done: make(chan string, 1)}
}

func (r *eventsRecorder) OnEvents(batchJSON string) {
	var b kubeEventsBatch
	if err := json.Unmarshal([]byte(batchJSON), &b); err != nil {
		panic(err)
	}

	r.batches <- b
}

func (r *eventsRecorder) OnStatus(stateJSON string) {
	var s kubeEventsStatus
	if err := json.Unmarshal([]byte(stateJSON), &s); err != nil {
		panic(err)
	}

	r.statuses <- s
}

func (r *eventsRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *eventsRecorder) batch(t *testing.T) kubeEventsBatch {
	t.Helper()

	select {
	case b := <-r.batches:
		return b
	case msg := <-r.done:
		t.Fatalf("ended early: %q", msg)
	case <-time.After(10 * time.Second):
		t.Fatal("no batch")
	}

	return kubeEventsBatch{}
}

// status waits for the status in state.
func (r *eventsRecorder) status(t *testing.T, state string) kubeEventsStatus {
	t.Helper()

	for {
		select {
		case s := <-r.statuses:
			if s.State == state {
				return s
			}
		case msg := <-r.done:
			t.Fatalf("ended early waiting for %s: %q", state, msg)
		case <-time.After(10 * time.Second):
			t.Fatalf("never %s", state)
		}
	}
}

func (r *eventsRecorder) ended(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("never ended")
	}

	return ""
}

// v1Event is an events.k8s.io/v1 Event about the pod name of shop.
func v1Event(uid, pod, eventType, reason string, count int, last, rv string) string {
	return `{"metadata":{"uid":"` + uid + `","namespace":"shop","name":"` + pod + `.` + uid + `","resourceVersion":"` + rv + `","creationTimestamp":"2026-10-10T10:00:00Z"},` +
		`"regarding":{"kind":"Pod","apiVersion":"v1","namespace":"shop","name":"` + pod + `"},"type":"` + eventType + `","reason":"` + reason + `",` +
		`"note":"` + reason + ` at ` + last + `","eventTime":"2026-10-10T10:00:00.000000Z","series":{"count":` + fmt.Sprint(count) + `,"lastObservedTime":"` + last + `"},"reportingController":"kubelet"}`
}

// coreEvent is a core/v1 Event about the pod name of shop.
func coreEvent(uid, pod, eventType, reason string, count int, last string) eventObject {
	var o eventObject

	raw := `{"metadata":{"uid":"` + uid + `","namespace":"shop","name":"` + pod + `.` + uid + `","creationTimestamp":"2026-10-10T10:00:00Z"},` +
		`"involvedObject":{"kind":"Pod","apiVersion":"v1","namespace":"shop","name":"` + pod + `"},"type":"` + eventType + `","reason":"` + reason + `",` +
		`"message":"` + reason + ` at ` + last + `","count":` + fmt.Sprint(count) + `,"firstTimestamp":"2026-10-10T10:00:00Z","lastTimestamp":"` + last + `","source":{"component":"kubelet"}}`

	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		panic(err)
	}

	return o
}

func podRegarding(o eventObject) kubeEventRegarding {
	return regardingOf(o, demoResourceLookup)
}

func TestEventCoalescerAddsUpObjectsOfOneRow(t *testing.T) {
	c := newEventCoalescer(10)

	add := func(o eventObject) { c.add(o, podRegarding(o)) }

	add(coreEvent("u1", "web-1", "Warning", "BackOff", 3, "2026-10-10T10:05:00Z"))
	add(coreEvent("u2", "web-1", "Warning", "BackOff", 2, "2026-10-10T10:09:00Z"))
	add(coreEvent("u3", "web-1", "Normal", "Pulled", 1, "2026-10-10T10:01:00Z"))
	// u1 seen again with its count up, but an earlier last time than u2's.
	add(coreEvent("u1", "web-1", "Warning", "BackOff", 4, "2026-10-10T10:06:00Z"))

	batch, ok := c.take(0)
	if !ok || len(batch.Upserts) != 2 {
		t.Fatalf("batch %+v", batch)
	}

	backOff := batch.Upserts[0]
	if backOff.Reason != "BackOff" || backOff.Count != 6 || backOff.Note != "BackOff at 2026-10-10T10:09:00Z" || backOff.Source != "kubelet" {
		t.Errorf("row %+v", backOff)
	}

	first, _ := time.Parse(time.RFC3339, "2026-10-10T10:00:00Z")
	last, _ := time.Parse(time.RFC3339, "2026-10-10T10:09:00Z")

	if backOff.FirstSeen != first.UnixMilli() || backOff.LastSeen != last.UnixMilli() {
		t.Errorf("first %d last %d", backOff.FirstSeen, backOff.LastSeen)
	}

	if r := backOff.Regarding; r.Kind != "Pod" || r.Group != "" || r.Version != "v1" || r.Resource != "pods" || !r.Namespaced || r.Namespace != "shop" || r.Name != "web-1" {
		t.Errorf("regarding %+v", r)
	}

	if backOff.Key == batch.Upserts[1].Key || len(backOff.Key) != 16 {
		t.Errorf("keys %q %q", backOff.Key, batch.Upserts[1].Key)
	}

	// The same again changes nothing: no batch.
	add(coreEvent("u2", "web-1", "Warning", "BackOff", 2, "2026-10-10T10:09:00Z"))

	if batch, ok := c.take(0); ok {
		t.Errorf("unchanged rows sent: %+v", batch)
	}

	// The key stays the row's.
	add(coreEvent("u2", "web-1", "Warning", "BackOff", 5, "2026-10-10T10:10:00Z"))

	if batch, _ := c.take(0); len(batch.Upserts) != 1 || batch.Upserts[0].Key != backOff.Key || batch.Upserts[0].Count != 9 {
		t.Errorf("update %+v", batch)
	}
}

func TestEventCoalescerFoldsOldObjects(t *testing.T) {
	c := newEventCoalescer(10)

	for i := range eventGroupObjects + 6 {
		o := coreEvent(fmt.Sprint("u", i), "web-1", "Warning", "BackOff", 1, "2026-10-10T10:05:00Z")
		c.add(o, podRegarding(o))
	}

	batch, _ := c.take(0)
	if len(batch.Upserts) != 1 || batch.Upserts[0].Count != eventGroupObjects+6 {
		t.Fatalf("batch %+v", batch)
	}

	for _, g := range c.groups {
		if len(g.counts) != eventGroupObjects || g.folded != 6 {
			t.Errorf("%d counts, %d folded", len(g.counts), g.folded)
		}
	}
}

func TestEventCoalescerCapLetsTheOldestGo(t *testing.T) {
	c := newEventCoalescer(2)
	add := func(uid, pod, last string) {
		o := coreEvent(uid, pod, "Warning", "BackOff", 1, last)
		c.add(o, podRegarding(o))
	}

	add("a", "web-a", "2026-10-10T10:01:00Z")
	add("b", "web-b", "2026-10-10T10:02:00Z")

	shown, _ := c.take(0)
	oldKey := shown.Upserts[1].Key

	add("c", "web-c", "2026-10-10T10:03:00Z")
	// Older than any row kept: not kept.
	add("d", "web-d", "2026-10-10T09:00:00Z")

	batch, _ := c.take(0)
	if len(batch.Upserts) != 1 || batch.Upserts[0].Regarding.Name != "web-c" || len(batch.Removed) != 1 || batch.Removed[0] != oldKey {
		t.Fatalf("batch %+v", batch)
	}

	if len(c.groups) != 2 || c.dropped != 2 {
		t.Errorf("%d rows, %d dropped", len(c.groups), c.dropped)
	}

	// A row let go before it was ever sent is not removed: the app never had it.
	c = newEventCoalescer(1)
	add("e", "web-e", "2026-10-10T10:04:00Z")
	add("f", "web-f", "2026-10-10T10:05:00Z")

	if batch, _ := c.take(0); len(batch.Removed) != 0 || len(batch.Upserts) != 1 || batch.Upserts[0].Regarding.Name != "web-f" {
		t.Errorf("batch %+v", batch)
	}
}

func TestDecodeEventV1(t *testing.T) {
	obj, err := decodeEvent(json.RawMessage(v1Event("u1", "web-1", "Warning", "BackOff", 7, "2026-10-10T10:07:00.000000Z", "3")), true)
	if err != nil {
		t.Fatal(err)
	}

	e := mapEvent(obj)
	if e.Kind != "Pod" || e.Name != "web-1" || e.Count != 7 || e.Source != "kubelet" || e.Message != "BackOff at 2026-10-10T10:07:00.000000Z" || obj.Metadata.UID != "u1" {
		t.Errorf("event %+v", e)
	}

	// Deprecated fields as the core API fills them in, the epoch as unset.
	raw := `{"metadata":{"uid":"u2"},"regarding":{"kind":"Node","apiVersion":"v1","name":"worker-1"},"type":"Normal","reason":"Starting",` +
		`"deprecatedCount":4,"deprecatedFirstTimestamp":"2026-10-10T09:00:00Z","deprecatedLastTimestamp":"2026-10-10T09:30:00Z",` +
		`"eventTime":"1970-01-01T00:00:00Z","deprecatedSource":{"component":"kubelet"}}`

	if obj, err = decodeEvent(json.RawMessage(raw), true); err != nil {
		t.Fatal(err)
	}

	e = mapEvent(obj)
	if e.Count != 4 || e.Source != "kubelet" || e.Last-e.First != (30*time.Minute).Milliseconds() {
		t.Errorf("event %+v", e)
	}

	if r := regardingOf(obj, demoResourceLookup); r.Resource != "nodes" || r.Namespaced {
		t.Errorf("regarding %+v", r)
	}

	if _, err := decodeEvent(json.RawMessage(`[`), true); err == nil {
		t.Error("broken JSON decoded")
	}
}

// The first batch carries the newest kubeEventsFirst rows; later ones only what changed.
func TestEventsStreamBatches(t *testing.T) {
	s := newEventsStream(apiCoreV1, false, demoResourceLookup)

	if _, ok, _ := s.pending(); ok {
		t.Fatal("a batch before the list")
	}

	items := make([]json.RawMessage, 0, 300)
	for i := range 300 {
		o := coreEvent(fmt.Sprint("u", i), fmt.Sprint("web-", i), "Normal", "Pulled", 1, time.Date(2026, 10, 10, 10, 0, i%60, 0, time.UTC).Add(time.Duration(i)*time.Minute).Format(time.RFC3339))
		raw, _ := json.Marshal(o)
		items = append(items, raw)
	}

	items = append(items, json.RawMessage(`{"broken`))

	if err := s.apply(watchEvent{Type: watchSync, Page: kubePage{items: items}, Partial: true}); err != nil {
		t.Fatal(err)
	}

	batch, ok, statuses := s.pending()
	status := statuses[len(statuses)-1]
	if !ok || !batch.Reset || len(batch.Upserts) != kubeEventsFirst || batch.Upserts[0].Regarding.Name != "web-299" {
		t.Fatalf("first batch: %v reset %v, %d rows", ok, batch.Reset, len(batch.Upserts))
	}

	if status.Tracked != 300 || !status.Partial || status.Limit != kubeEventsKeep || !strings.Contains(status.Note, "5000") {
		t.Errorf("status %+v", status)
	}

	if _, ok, _ := s.pending(); ok {
		t.Error("a batch with nothing changed")
	}

	o := coreEvent("u5", "web-5", "Normal", "Pulled", 2, "2026-10-11T00:00:00Z")
	raw, _ := json.Marshal(o)
	_ = s.apply(watchEvent{Type: watchModified, Page: kubePage{items: []json.RawMessage{raw}}})
	_ = s.apply(watchEvent{Type: watchDeleted, Page: kubePage{items: []json.RawMessage{raw}}})

	if batch, ok, _ := s.pending(); !ok || batch.Reset || len(batch.Upserts) != 1 || batch.Upserts[0].Count != 2 {
		t.Errorf("update %+v", batch)
	}
}

func TestEventsStreamWarningsOnly(t *testing.T) {
	s := newEventsStream(apiEventsV1, true, demoResourceLookup)
	items := []json.RawMessage{
		json.RawMessage(v1Event("u1", "web-1", "Warning", "BackOff", 2, "2026-10-10T10:05:00Z", "1")),
		json.RawMessage(v1Event("u2", "web-1", "Normal", "Pulled", 1, "2026-10-10T10:05:00Z", "1")),
	}

	_ = s.apply(watchEvent{Type: watchSync, Page: kubePage{items: items}})

	if batch, _, _ := s.pending(); len(batch.Upserts) != 1 || batch.Upserts[0].Type != "Warning" {
		t.Errorf("batch %+v", batch)
	}
}

func TestStatusNote(t *testing.T) {
	if n := statusNote(kubeEventsStatus{Dropped: 3, Limit: 2000}); !strings.Contains(n, "2000") {
		t.Errorf("note %q", n)
	}

	if n := statusNote(kubeEventsStatus{}); n != "" {
		t.Errorf("note %q", n)
	}
}

// eventsAPI is a fake API server serving events.k8s.io/v1 (or not) and core/v1 discovery.
func eventsAPI(t *testing.T, eventsV1 bool, events http.HandlerFunc) *fakeKubeAPI {
	t.Helper()

	return newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/events.k8s.io/v1":
			if !eventsV1 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"kind":"Status","code":404,"message":"the server could not find the requested resource"}`)

				return
			}

			_, _ = io.WriteString(w, `{"groupVersion":"events.k8s.io/v1","resources":[{"name":"events","kind":"Event","namespaced":true,"verbs":["list","watch"]}]}`)
		case "/api/v1":
			_, _ = io.WriteString(w, `{"groupVersion":"v1","resources":[{"name":"pods","kind":"Pod","namespaced":true,"verbs":["get","list"]}]}`)
		default:
			events(w, r)
		}
	})
}

// The list, a count going up through the watch, a retry, a 410 answered with the list again,
// each with its status; the warning filter goes to the server.
func TestStartKubeEventsFollowsAndRelists(t *testing.T) {
	var (
		mu      sync.Mutex
		lists   int
		watches int
		shown   = make(chan struct{})
	)

	f := eventsAPI(t, true, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/apis/events.k8s.io/v1/namespaces/shop/events" || q.Get("fieldSelector") != "type=Warning" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"kind":"Status","message":"unexpected `+r.URL.String()+`"}`)

			return
		}

		mu.Lock()

		if q.Get("watch") == "" {
			lists++
			mu.Unlock()
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"10"},"items":[`+v1Event("u1", "web-1", "Warning", "BackOff", 3, "2026-10-10T10:05:00Z", "9")+`]}`)

			return
		}

		watches++
		n := watches
		mu.Unlock()

		switch n {
		case 1:
			// Once the first batch is out.
			select {
			case <-shown:
			case <-r.Context().Done():
				return
			}

			streamEvents(w, `{"type":"MODIFIED","object":`+v1Event("u1", "web-1", "Warning", "BackOff", 4, "2026-10-10T10:06:00Z", "11")+`}`)
		case 2:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"kind":"Status","code":500,"message":"etcd is slow"}`)
		case 3:
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, statusJSON(410, "too old resource version"))
		default:
			holdOpen(w, r)
		}
	})

	rec := newEventsRecorder()
	run := StartKubeEvents(kubeStoreFor(t, f), "admin@test", "", "shop", true, rec)

	if s := rec.status(t, watchLive); s.API != apiEventsV1 {
		t.Errorf("status %+v", s)
	}

	first := rec.batch(t)
	if !first.Reset || len(first.Upserts) != 1 || first.Upserts[0].Count != 3 || first.Upserts[0].Regarding.Resource != "pods" {
		t.Fatalf("first batch %+v", first)
	}

	close(shown)

	if b := rec.batch(t); len(b.Upserts) != 1 || b.Upserts[0].Count != 4 || b.Upserts[0].Key != first.Upserts[0].Key {
		t.Errorf("second batch %+v", b)
	}

	if s := rec.status(t, watchReconnecting); !strings.Contains(s.Reason, "etcd is slow") {
		t.Errorf("status %+v", s)
	}

	rec.status(t, watchRelisting)
	rec.status(t, watchLive)

	mu.Lock()
	if lists != 2 {
		t.Errorf("%d lists", lists)
	}
	mu.Unlock()

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}
}

// Without events.k8s.io the core API's events of every namespace; a burst is one batch.
func TestStartKubeEventsFallsBackToCoreAndBatches(t *testing.T) {
	burst := make([]string, 0, 50)

	for i := range 50 {
		o := coreEvent(fmt.Sprint("u", i), fmt.Sprint("web-", i), "Normal", "Pulled", 1, "2026-10-10T10:05:00Z")
		raw, _ := json.Marshal(o)
		burst = append(burst, `{"type":"ADDED","object":`+string(raw)+`}`)
	}

	listed := make(chan struct{})

	f := eventsAPI(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" || r.URL.Query().Get("fieldSelector") != "" {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		if r.URL.Query().Get("watch") == "" {
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"10"},"items":[]}`)

			return
		}

		// After the first (empty) batch, the whole burst at once.
		select {
		case <-listed:
		case <-r.Context().Done():
			return
		}

		streamEvents(w, burst...)
		holdOpen(w, r)
	})

	rec := newEventsRecorder()
	run := StartKubeEvents(kubeStoreFor(t, f), "admin@test", "", "", false, rec)

	if s := rec.status(t, watchLive); s.API != apiCoreV1 {
		t.Errorf("status %+v", s)
	}

	if b := rec.batch(t); !b.Reset || len(b.Upserts) != 0 {
		t.Fatalf("first batch %+v", b)
	}

	close(listed)

	rows, batches := 0, 0
	for rows < 50 {
		rows += len(rec.batch(t).Upserts)
		batches++
	}

	if batches > 2 {
		t.Errorf("a burst of 50 came in %d batches", batches)
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}
}

func TestStartKubeEventsRefusals(t *testing.T) {
	rec := newEventsRecorder()
	StartKubeEvents("", "", "", "Not_A_Namespace", false, rec)

	if msg := rec.ended(t); !strings.Contains(msg, "namespace") {
		t.Errorf("ended with %q", msg)
	}

	f := eventsAPI(t, true, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","code":403,"message":"events is forbidden"}`)
	})

	rec = newEventsRecorder()
	StartKubeEvents(kubeStoreFor(t, f), "admin@test", "", "", false, rec)

	if msg := rec.ended(t); !strings.Contains(msg, "events is forbidden") {
		t.Errorf("ended with %q", msg)
	}
}

func TestStartKubeEventsDemo(t *testing.T) {
	noKubeDial(t)

	// The first batch before the first tick.
	demoKubeEventsEvery, kubeEventsFlushEvery = 150*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { demoKubeEventsEvery, kubeEventsFlushEvery = 3*time.Second, 250*time.Millisecond })

	rec := newEventsRecorder()
	run := StartKubeEvents(demoKubeconfigForTest(t), "", "", "", false, rec)

	first := rec.batch(t)
	if !first.Reset || len(first.Upserts) != 5 || first.Upserts[0].Reason != "BackOff" || first.Upserts[0].Regarding.Resource != "pods" {
		t.Fatalf("first batch %+v", first)
	}

	backOff, count, fresh := first.Upserts[0].Key, first.Upserts[0].Count, false

	for !fresh {
		for _, row := range rec.batch(t).Upserts {
			switch {
			case row.Key == backOff && row.Count <= count:
				t.Fatalf("BackOff count %d after %d", row.Count, count)
			case row.Key == backOff:
				count = row.Count
			default:
				fresh = true
			}
		}
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}

	// Warnings only; another namespace has none.
	for _, tc := range []struct {
		namespace    string
		warningsOnly bool
		want         int
	}{{"demo", true, 2}, {"other", false, 0}} {
		rec := newEventsRecorder()
		run := StartKubeEvents(demoKubeconfigForTest(t), "", "", tc.namespace, tc.warningsOnly, rec)

		b := rec.batch(t)
		run.Cancel()
		rec.ended(t)

		if len(b.Upserts) != tc.want {
			t.Errorf("%s: %d rows", tc.namespace, len(b.Upserts))
		}

		for _, row := range b.Upserts {
			if row.Type != "Warning" {
				t.Errorf("%s: %+v", tc.namespace, row)
			}
		}
	}
}

// A list stops at maxItems (whole pages) and says so; the watch starts from its version.
func TestWatchListStopsAtMaxItems(t *testing.T) {
	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		switch {
		case q.Get("watch") != "":
			holdOpen(w, r)
		case q.Get("continue") != "":
			t.Error("read past maxItems")
			w.WriteHeader(http.StatusBadRequest)
		default:
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"10","continue":"next"},"items":[`+podWithVersion("a", "9")+`,`+podWithVersion("b", "9")+`]}`)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var states []string

	spec := watchSpec{path: "/api/v1/pods", maxItems: 2, onState: func(state, _ string) { states = append(states, state) }}

	err := watchList(ctx, watchClient(t, f), spec, func(ev watchEvent) error {
		if ev.Type != watchSync || !ev.Partial || len(ev.Page.items) != 2 {
			t.Errorf("event %+v", ev)
		}

		cancel()

		return nil
	})

	if !errors.Is(err, context.Canceled) || strings.Join(states, ",") != watchLive {
		t.Errorf("ended with %v, states %v", err, states)
	}
}
