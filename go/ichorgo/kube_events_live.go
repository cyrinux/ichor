package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// The cluster's events as a live stream, `kubectl get events -A --watch` coalesced: the
// list once (newest rows first), then the API server's changes through a watch
// (kube_watch.go: bookmarks, the list again on 410 Gone, backoff), rows coalesced
// (kube_events_coalesce.go) and sent in batches, so a burst of events is one UI update.

// kubeEventsListMax caps the Event objects the first list reads (whole pages): on a cluster
// keeping more, the rest only shows once it changes, and the status says partial.
const kubeEventsListMax = 5000

// Variables for the tests.
var (
	// kubeEventsFlushEvery is how often the changes are sent, at most.
	kubeEventsFlushEvery = 250 * time.Millisecond
	// demoKubeEventsEvery is how often the demo inventory's stream moves.
	demoKubeEventsEvery = 3 * time.Second
)

// KubeEventsListener follows the cluster's events live (implemented in Kotlin/Swift).
type KubeEventsListener interface {
	// OnEvents gets a batch of rows as JSON (kubeEventsBatch), at most every 250 ms.
	OnEvents(batchJSON string)
	// OnStatus gets where the stream stands as JSON (kubeEventsStatus), when it changes.
	OnStatus(stateJSON string)
	// OnDone is called exactly once; errMessage is empty when cancelled.
	OnDone(errMessage string)
}

// KubeEventsRun is a handle on an event stream.
type KubeEventsRun struct {
	cancel context.CancelFunc
}

// Cancel stops the stream; OnDone follows.
func (r *KubeEventsRun) Cancel() { r.cancel() }

// kubeEventsStatus is one OnStatus call.
type kubeEventsStatus struct {
	// State is live, reconnecting (Reason the error), relisting (the version expired) or
	// polling (the account may list events but not watch them).
	State  string `json:"state"`
	Reason string `json:"reason"`
	// API is the events API read: events.k8s.io/v1, or v1 where it is not served.
	API string `json:"api"`
	// Tracked rows of at most Limit; Dropped rows let go (the seen longest ago) past it.
	Tracked int `json:"tracked"`
	Limit   int `json:"limit"`
	Dropped int `json:"dropped"`
	// Partial: the first list stopped at kubeEventsListMax Event objects.
	Partial bool `json:"partial"`
	// Note says in a sentence what Dropped and Partial mean, "" when neither applies.
	Note string `json:"note"`
}

// StartKubeEvents streams the cluster's events (os:admin), namespace "" for every namespace,
// warningsOnly for type Warning alone. OnEvents gets {"reset", "upserts": [kubeEventRow],
// "removed": [key]}: the first batch (reset) carries the newest 200 rows, then each batch the
// rows that changed (a new one, a count gone up) by their stable key, and the keys let go past
// the 2000 rows kept. OnStatus gets the stream's state (kubeEventsStatus). Rows come from
// events.k8s.io/v1, or core/v1 where it is not served. kubeServer: see KubePods.
func StartKubeEvents(configYAML, contextName, kubeServer, namespace string, warningsOnly bool, listener KubeEventsListener) *KubeEventsRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeEventsListener{listener}
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	err := validateNamespace(namespace)
	target := kubeTarget{configYAML, contextName, kubeServer}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		switch {
		case err != nil:
		case isDemoContext(target.config, target.context):
			s := newEventsStream(apiEventsV1, warningsOnly, demoResourceLookup)
			s.setState(watchLive, "")
			err = s.run(ctx, listener, demoKubeEventsFollow(namespace))
		default:
			_, err = withKubeContext(ctx, target, noResult(func(ctx context.Context, k *kubeClient) error {
				return followKubeEvents(ctx, k, namespace, warningsOnly, listener)
			}))
		}

		listener.OnDone(doneMessage(ctx, err))
	}()

	return &KubeEventsRun{cancel: cancel}
}

// The events APIs, as kubeEventsStatus.API names them.
const (
	apiEventsV1 = "events.k8s.io/v1"
	apiCoreV1   = "v1"
)

// followKubeEvents is StartKubeEvents against a cluster.
func followKubeEvents(ctx context.Context, k *kubeClient, namespace string, warningsOnly bool, listener KubeEventsListener) error {
	api, prefix := apiCoreV1, "/api/v1"
	if servesEventsV1(ctx, k) {
		api, prefix = apiEventsV1, "/apis/events.k8s.io/v1"
	}

	resolver := newKindResolver(k)
	lookup := func(apiVersion, kind string) (kubeBrowserResource, bool) {
		return resolver.byAPIVersion(ctx, apiVersion, kind)
	}

	s := newEventsStream(api, warningsOnly, lookup)
	spec := watchSpec{path: scopedPath(prefix, namespace, "events"), maxItems: kubeEventsListMax, onState: s.setState}

	if warningsOnly {
		spec.fieldSelector = "type=Warning"
	}

	return s.run(ctx, listener, func(ctx context.Context, handle func(watchEvent) error) error {
		return watchList(ctx, k, spec, handle)
	})
}

// servesEventsV1 tells whether the API server serves events.k8s.io/v1 (1.19 and later). A
// discovery that fails otherwise than by a 404 says yes: the watch tells the rest.
func servesEventsV1(ctx context.Context, k *kubeClient) bool {
	var list discoveryResourceList
	if err := k.get(ctx, "/apis/events.k8s.io/v1", &list); err != nil {
		return kubeCode(err) != http.StatusNotFound
	}

	return slices.ContainsFunc(list.Resources, func(r discoveryResource) bool { return r.Name == "events" })
}

// resourceLookup finds the resource of kind in apiVersion, false when unknown.
type resourceLookup func(apiVersion, kind string) (kubeBrowserResource, bool)

// demoResourceLookup is resourceLookup over the demo inventory's resources.
func demoResourceLookup(apiVersion, kind string) (kubeBrowserResource, bool) {
	group, _ := splitAPIVersion(apiVersion)
	resources := demoAPIResources().Resources

	i := slices.IndexFunc(resources, func(r kubeBrowserResource) bool { return r.Kind == kind && r.Group == group })
	if i < 0 {
		return kubeBrowserResource{}, false
	}

	return resources[i], true
}

// regardingOf is the object obj is about, with its resource when lookup knows the kind.
func regardingOf(obj eventObject, lookup resourceLookup) kubeEventRegarding {
	o := obj.InvolvedObject
	r := kubeEventRegarding{Kind: o.Kind, APIVersion: o.APIVersion, Namespace: o.Namespace, Name: o.Name, Namespaced: o.Namespace != ""}
	r.Group, r.Version = splitAPIVersion(o.APIVersion)

	if o.APIVersion == "" || o.Kind == "" {
		return r
	}

	if res, ok := lookup(o.APIVersion, o.Kind); ok {
		r.Group, r.Version, r.Resource, r.Namespaced = res.Group, res.Version, res.Resource, res.Namespaced
	}

	return r
}

// eventsStream is the state of one stream: the rows, where the watch stands, what was sent.
type eventsStream struct {
	warningsOnly bool
	eventsV1     bool
	lookup       resourceLookup

	mu        sync.Mutex
	rows      *eventCoalescer
	status    kubeEventsStatus
	states    []kubeEventsStatus // the states passed through since the last pending
	listed    bool               // a list was read: batches may go
	sentFirst bool
}

func newEventsStream(api string, warningsOnly bool, lookup resourceLookup) *eventsStream {
	return &eventsStream{
		warningsOnly: warningsOnly, eventsV1: api == apiEventsV1, lookup: lookup,
		rows:   newEventCoalescer(kubeEventsKeep),
		status: kubeEventsStatus{API: api, Limit: kubeEventsKeep},
	}
}

// setState is the watch's watchSpec.onState.
func (s *eventsStream) setState(state, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status.State, s.status.Reason = state, reason
	// Each one is sent, however short: a relist may last less than a flush.
	s.states = append(s.states, s.status)
}

// apply counts ev's Event objects into the rows; one that cannot be decoded is skipped, a
// deleted one (its time to live over) stays in the stream.
func (s *eventsStream) apply(ev watchEvent) error {
	if ev.Type == watchDeleted {
		return nil
	}

	type decoded struct {
		obj       eventObject
		regarding kubeEventRegarding
	}

	objs := make([]decoded, 0, len(ev.Page.items))

	for _, raw := range ev.Page.items {
		obj, err := decodeEvent(raw, s.eventsV1)
		if err != nil || (s.warningsOnly && obj.Type != "Warning") {
			continue
		}

		// Outside the lock: the lookup may read discovery.
		objs = append(objs, decoded{obj, regardingOf(obj, s.lookup)})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range objs {
		s.rows.add(d.obj, d.regarding)
	}

	if ev.Type == watchSync {
		if !s.listed {
			s.status.Partial = ev.Partial
		}

		s.listed = true
	}

	return nil
}

// pending is the batch to send (ok false when none) and the statuses passed through since the
// last call, the current one last, with the rows as they stand.
func (s *eventsStream) pending() (kubeEventsBatch, bool, []kubeEventsStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		batch kubeEventsBatch
		ok    bool
	)

	switch {
	case !s.listed:
	case !s.sentFirst:
		batch, _ = s.rows.take(kubeEventsFirst)
		batch.Reset, batch.Removed, ok = true, []string{}, true
		s.sentFirst = true
	default:
		batch, ok = s.rows.take(0)
	}

	if batch.Upserts == nil {
		batch.Upserts = []kubeEventRow{}
	}

	s.status.Tracked, s.status.Dropped = len(s.rows.groups), s.rows.dropped
	s.status.Note = statusNote(s.status)

	statuses := append(s.states, s.status)
	for i := range statuses {
		statuses[i].Tracked, statuses[i].Dropped = s.status.Tracked, s.status.Dropped
		statuses[i].Partial, statuses[i].Note = s.status.Partial, s.status.Note
	}

	s.states = nil

	return batch, ok, statuses
}

// statusNote says what Dropped and Partial mean.
func statusNote(st kubeEventsStatus) string {
	var notes []string

	if st.Partial {
		notes = append(notes, fmt.Sprintf("the first list stopped at %d events", kubeEventsListMax))
	}

	if st.Dropped > 0 {
		notes = append(notes, fmt.Sprintf("older rows let go to keep the newest %d", st.Limit))
	}

	return strings.Join(notes, "; ")
}

// run follows the stream with follow, sending what changed every kubeEventsFlushEvery from
// one goroutine (the listener's callbacks never overlap), until follow ends.
func (s *eventsStream) run(ctx context.Context, listener KubeEventsListener, follow func(context.Context, func(watchEvent) error) error) error {
	flushCtx, stop := context.WithCancel(ctx)

	var (
		wg   sync.WaitGroup
		sent kubeEventsStatus
	)

	flush := func() {
		batch, ok, statuses := s.pending()
		for _, status := range statuses {
			if status != sent && status.State != "" {
				sent = status
				emitJSON(status, listener.OnStatus)
			}
		}

		if ok {
			emitJSON(batch, listener.OnEvents)
		}
	}

	wg.Go(func() {
		ticker := time.NewTicker(kubeEventsFlushEvery)
		defer ticker.Stop()

		for {
			select {
			case <-flushCtx.Done():
				return
			case <-ticker.C:
				flush()
			}
		}
	})

	err := follow(ctx, s.apply)

	stop()
	wg.Wait()

	// What came last, before OnDone tells why it ended.
	if ctx.Err() == nil {
		flush()
	}

	return err
}

// demoKubeEventsFollow is the demo inventory's stream: a few events listed, then every
// demoKubeEventsEvery the crash-looping pod's BackOff once more and, every other time, a new
// event. namespace "" for every namespace (all of them are in "demo").
func demoKubeEventsFollow(namespace string) func(context.Context, func(watchEvent) error) error {
	return func(ctx context.Context, handle func(watchEvent) error) error {
		visible := namespace == "" || namespace == "demo"
		send := func(eventType string, items ...json.RawMessage) error {
			if !visible {
				items = nil
			}

			return handle(watchEvent{Type: eventType, Page: kubePage{items: items, remaining: -1}})
		}

		now := time.Now()
		if err := send(watchSync, demoLiveEvents(now)...); err != nil {
			return err
		}

		ticker := time.NewTicker(demoKubeEventsEvery)
		defer ticker.Stop()

		for i := 1; ; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case at := <-ticker.C:
				items := []json.RawMessage{demoBackOff(now, at, i)}
				if i%2 == 0 {
					items = append(items, demoNewEvent(at, i/2))
				}

				if err := send(watchModified, items...); err != nil {
					return err
				}
			}
		}
	}
}
