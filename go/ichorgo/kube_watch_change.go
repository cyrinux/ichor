package ichorgo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// A change signal for lists the apps cannot merge row by row (the workloads of three kinds,
// Argo CD's derived status): watches on each kind count what changed, and the app is told
// "something changed" to read its list again, at most every changeFloor. Only counts cross,
// never objects: the lists are Table rows kept in Go memory, fine up to a few thousand objects
// per kind.

// changeFloor is the least time between two change signals: a refresh they cause settles the
// list, which restarts the watch; a burst of changes (five deployments scaled) is one signal.
var changeFloor = 2 * time.Second

// kubeChange is one change signal: how many objects changed since the last one, and when.
type kubeChange struct {
	Changed int    `json:"changed"`
	At      string `json:"at"`
}

// StartKubeChangeWatch tells listener when one of kinds changes in namespace ("" for every
// namespace), instead of the app polling: {"changed": count, "at": RFC 3339} after each burst
// of changes, at most every 2 s, never for the lists read at the start (so a screen that just
// loaded does not load again). kinds is a comma list of GROUP/VERSION/RESOURCE (VERSION/RESOURCE
// for the core group), e.g. "apps/v1/deployments,apps/v1/statefulsets". An account that may
// list but not watch a kind gets it polled; nothing is sent in the demo inventory, where
// nothing changes. kubeServer: see KubePods.
func StartKubeChangeWatch(configYAML, contextName, kubeServer, namespace, kinds string, listener KubeLiveListener) *KubeLiveRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeLiveListener{listener}
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	refs, err := parseChangeKinds(kinds)
	if err == nil && namespace != "" {
		err = validateNamespace(namespace)
	}

	specs := make([]watchSpec, len(refs))
	for i, ref := range refs {
		specs[i] = watchSpec{path: ref.path(namespace), table: true}
	}

	demo := func() (string, error) { return "", nil }

	return startLive(kubeTarget{configYAML, contextName, kubeServer}, err, listener, demo, func(ctx context.Context, k *kubeClient, emit func(string)) error {
		return changeView(ctx, k, specs, emit)
	})
}

// parseChangeKinds reads "GROUP/VERSION/RESOURCE,…" (VERSION/RESOURCE for the core group).
func parseChangeKinds(kinds string) ([]resourceRef, error) {
	var refs []resourceRef

	for kind := range strings.SplitSeq(kinds, ",") {
		kind = strings.TrimSpace(kind)
		if kind == "" {
			continue
		}

		parts := strings.Split(kind, "/")
		if len(parts) == 2 {
			parts = append([]string{""}, parts...)
		}

		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid kind %q: GROUP/VERSION/RESOURCE expected", kind)
		}

		ref, err := newResourceRef(parts[0], parts[1], parts[2])
		if err != nil {
			return nil, err
		}

		refs = append(refs, ref)
	}

	if len(refs) == 0 {
		return nil, fmt.Errorf("no kind to watch")
	}

	return refs, nil
}

// changeCounter counts the objects that changed across the watched kinds. Each kind keeps
// the resourceVersion of every object seen, so a list read again (a watch that started over,
// a poll for a role that may not watch) counts only what differs from what was known.
type changeCounter struct {
	mu      sync.Mutex
	count   int
	changed chan struct{}
}

func newChangeCounter() *changeCounter { return &changeCounter{changed: make(chan struct{}, 1)} }

func (c *changeCounter) add(n int) {
	if n == 0 {
		return
	}

	c.mu.Lock()
	c.count += n
	c.mu.Unlock()

	select {
	case c.changed <- struct{}{}:
	default:
	}
}

// take is the count since the last take.
func (c *changeCounter) take() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := c.count
	c.count = 0

	return n
}

// kindVersions is what one watched kind knows: object key -> resourceVersion.
type kindVersions struct {
	known  map[string]string
	listed bool
}

// apply counts what ev changes in the kind; the first list counts nothing.
func (v *kindVersions) apply(ev watchEvent) int {
	rows := tableRows(ev.Page)

	if ev.Type == watchSync {
		next := make(map[string]string, len(rows))
		for _, r := range rows {
			next[rowKey(r.Object.Metadata)] = r.Object.Metadata.ResourceVersion
		}

		n := 0

		if v.listed {
			for key, rv := range next {
				if old, ok := v.known[key]; !ok || old != rv {
					n++
				}
			}

			for key := range v.known {
				if _, ok := next[key]; !ok {
					n++
				}
			}
		}

		v.known, v.listed = next, true

		return n
	}

	if len(rows) == 0 {
		return 1 // not a Table: the change itself is all there is to count
	}

	for _, r := range rows {
		key := rowKey(r.Object.Metadata)
		if ev.Type == watchDeleted {
			delete(v.known, key)
		} else {
			v.known[key] = r.Object.Metadata.ResourceVersion
		}
	}

	return len(rows)
}

func tableRows(p kubePage) []kubeTableRow {
	if p.table == nil {
		return nil
	}

	return p.table.Rows
}

func rowKey(m kubeRowMeta) string { return m.Namespace + "/" + m.Name }

// changeView watches specs and emits a kubeChange after each burst of changes, liveDebounce
// after its first change and at least changeFloor after the previous signal. A watch that
// ends (an API that is gone, a refusal) ends the view with its error.
func changeView(ctx context.Context, k *kubeClient, specs []watchSpec, emit func(string)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	counter := newChangeCounter()
	ended := make(chan error, len(specs))

	var wg sync.WaitGroup

	for _, spec := range specs {
		wg.Go(func() {
			versions := &kindVersions{known: map[string]string{}}
			ended <- watchList(ctx, k, spec, func(ev watchEvent) error {
				counter.add(versions.apply(ev))

				return nil
			})
		})
	}

	err := signalChanges(ctx, counter, ended, emit, time.Now)

	cancel()
	wg.Wait()

	return err
}

// signalChanges is changeView's loop: one signal per settled burst, spaced by changeFloor.
func signalChanges(ctx context.Context, counter *changeCounter, ended <-chan error, emit func(string), now func() time.Time) error {
	var last time.Time

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-ended:
			return err
		case <-counter.changed:
		}

		wait := liveDebounce
		if floor := changeFloor - now().Sub(last); floor > wait {
			wait = floor
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		// What arrived while waiting is part of this signal.
		select {
		case <-counter.changed:
		default:
		}

		if n := counter.take(); n > 0 {
			last = now()
			emitJSON(kubeChange{Changed: n, At: last.UTC().Format(time.RFC3339)}, emit)
		}
	}
}
