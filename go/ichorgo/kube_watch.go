package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// A list kept live with a watch: the list once, then the API server's ADDED/MODIFIED/DELETED
// events from its resourceVersion, bookmarks kept so a new request starts where the last one
// ended, and the list again when the server has let the version go (410 Gone). A watch
// outlives the server's own timeout and a cut connection; a refusal (401, 403, 404, a bad
// selector) ends it.

const (
	// watchTimeoutSeconds asks the server to end each watch request after this long: the next
	// one starts from the last resourceVersion seen, no event lost.
	watchTimeoutSeconds = 300
	// watchStreamGrace is how much longer than its own timeout a watch request may last
	// before the client gives it up as stalled (a connection that died without a word).
	watchStreamGrace = 30 * time.Second
	// watchRetryMin and watchRetryMax bound the wait before a failed request is tried again.
	watchRetryMin = time.Second
	watchRetryMax = 30 * time.Second
)

// Variables for the tests.
var (
	// watchPollInterval is how often the list is read again when the role may list but not
	// watch (a 403 on the watch alone): the same events, later.
	watchPollInterval = 5 * time.Second
	// watchStallAfter is how long a watch request may last before the client gives it up as
	// stalled: the server's own timeout plus watchStreamGrace.
	watchStallAfter = watchTimeoutSeconds*time.Second + watchStreamGrace
)

// Event types, as the API server names them, and watchSync for the list itself.
const (
	watchSync     = "SYNC"
	watchAdded    = "ADDED"
	watchModified = "MODIFIED"
	watchDeleted  = "DELETED"
	watchBookmark = "BOOKMARK"
	watchError    = "ERROR"
)

// watchSpec is one list to keep live.
type watchSpec struct {
	path                         string // the collection: /api/v1/namespaces/NS/pods
	labelSelector, fieldSelector string
	// table asks for Table rows (any resource, 10-20 times smaller) rather than full objects.
	table bool
}

// watchEvent is one change: Type watchSync with the whole list (after the first list and
// each one started over), else one object (one item, or a Table of one row).
type watchEvent struct {
	Type string
	Page kubePage
}

// rawWatchEvent is one line of a watch answer.
type rawWatchEvent struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

// watcher is the state of one list kept live.
type watcher struct {
	k      *kubeClient
	spec   watchSpec
	handle func(watchEvent) error
	// resourceVersion is where the next watch request starts.
	resourceVersion string
	// columns are the Table's, from the list: a watch sends them with its first event only.
	columns []kubeTableColumn
	wait    time.Duration
}

// watchList keeps spec live until ctx ends, handing each event to handle; an error of handle
// ends it too. The error is ctx's when cancelled.
func watchList(ctx context.Context, k *kubeClient, spec watchSpec, handle func(watchEvent) error) error {
	w := &watcher{k: k, spec: spec, handle: handle, wait: watchRetryMin}
	relist := true

	for {
		if relist {
			if err := w.list(ctx); err != nil {
				if err := w.pause(ctx, err); err != nil {
					return err
				}

				continue
			}

			relist = false
		}

		err := w.stream(ctx)

		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case isKubeExpired(err):
			relist = true
		case isForbidden(err):
			// The list was allowed, the watch is not: the list again, every so often.
			return w.poll(ctx)
		case err != nil:
			if err := w.pause(ctx, err); err != nil {
				return err
			}
		default:
			// Ended on the server's timeout: again from the last version seen.
			w.wait = watchRetryMin
		}
	}
}

// list reads the whole list, hands it over as one watchSync and keeps where to watch from.
func (w *watcher) list(ctx context.Context) error {
	q := pageQuery{limit: kubePageLimit, labelSelector: w.spec.labelSelector, fieldSelector: w.spec.fieldSelector, table: w.spec.table}

	var merged kubePage

	err := w.k.listAll(ctx, w.spec.path, q, func() { merged = kubePage{} }, func(page kubePage) error {
		merged = mergePages(merged, page)

		return nil
	})
	if err != nil {
		return err
	}

	if merged.table != nil {
		w.columns = merged.table.Columns
	}

	w.resourceVersion = merged.resourceVersion
	w.wait = watchRetryMin

	return w.handle(watchEvent{Type: watchSync, Page: merged})
}

// mergePages adds page to the pages read so far (none: page itself). The resourceVersion is
// the first page's, which the continue tokens pin for the next ones.
func mergePages(into, page kubePage) kubePage {
	if into.table == nil && into.items == nil {
		page.continueToken, page.remaining = "", 0

		return page
	}

	switch {
	case into.table != nil && page.table != nil:
		into.table.Rows = append(into.table.Rows, page.table.Rows...)
	case into.table == nil && page.table == nil:
		into.items = append(into.items, page.items...)
	}

	return into
}

// poll reads the list again every watchPollInterval, for a role that may not watch, until
// ctx ends; a list that fails is tried again as a watch would be.
func (w *watcher) poll(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(watchPollInterval):
		}

		if err := w.list(ctx); err != nil {
			if err := w.pause(ctx, err); err != nil {
				return err
			}
		}
	}
}

// stream follows one watch request until the server ends it (nil), the version expired
// (a 410 kubeAPIError), or it failed. A request outliving the server's timeout by
// watchStreamGrace is stalled: given up like one the server ended.
func (w *watcher) stream(ctx context.Context) error {
	streamCtx, cancel := context.WithTimeout(ctx, watchStallAfter)
	defer cancel()

	err := w.streamOnce(streamCtx)
	if ctx.Err() == nil && errors.Is(streamCtx.Err(), context.DeadlineExceeded) {
		return nil
	}

	return err
}

// streamOnce is stream's request, read until ctx ends or the server does.
func (w *watcher) streamOnce(ctx context.Context) error {
	query := url.Values{}
	query.Set("watch", "1")
	query.Set("resourceVersion", w.resourceVersion)
	query.Set("allowWatchBookmarks", "true")
	query.Set("timeoutSeconds", strconv.Itoa(watchTimeoutSeconds))

	if w.spec.labelSelector != "" {
		query.Set("labelSelector", w.spec.labelSelector)
	}

	if w.spec.fieldSelector != "" {
		query.Set("fieldSelector", w.spec.fieldSelector)
	}

	accept := "application/json"
	if w.spec.table {
		accept = kubeTableAccept

		query.Set("includeObject", "Metadata")
	}

	return w.k.stream(ctx, w.spec.path+"?"+query.Encode(), accept, func(r io.Reader) error {
		dec := json.NewDecoder(r)

		for {
			var ev rawWatchEvent
			if err := dec.Decode(&ev); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}

				return err
			}

			if err := w.apply(ev); err != nil {
				return err
			}
		}
	})
}

// apply keeps the event's resourceVersion and hands a change over; a bookmark only moves
// the version on, an ERROR ends the request with the status it carries.
func (w *watcher) apply(ev rawWatchEvent) error {
	var obj struct {
		Kind     string `json:"kind"`
		Code     int    `json:"code"`
		Reason   string `json:"reason"`
		Message  string `json:"message"`
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}

	_ = json.Unmarshal(ev.Object, &obj) //nolint:errcheck // an undecodable object is handed over as is

	switch ev.Type {
	case watchBookmark:
		w.resourceVersion = obj.Metadata.ResourceVersion

		return nil
	case watchError:
		if obj.Code == 0 {
			obj.Code = http.StatusInternalServerError
		}

		return kubeStatusError(obj.Code, ev.Object)
	case watchAdded, watchModified, watchDeleted:
	default:
		return nil
	}

	if obj.Metadata.ResourceVersion != "" {
		w.resourceVersion = obj.Metadata.ResourceVersion
	}

	page := kubePage{remaining: -1}

	// A server that ignores the Table ask sends the object itself, as decodePage allows.
	if w.spec.table && obj.Kind == "Table" {
		var t kubeTable
		if err := json.Unmarshal(ev.Object, &t); err != nil {
			return err
		}

		if len(t.Columns) == 0 {
			t.Columns = w.columns
		}

		page.table = &t
	} else {
		page.items = []json.RawMessage{ev.Object}
	}

	return w.handle(watchEvent{Type: ev.Type, Page: page})
}

// pause waits before the next try after err, longer each time, or returns err when the
// server refused (a 4xx but 410): no retry will change its mind.
func (w *watcher) pause(ctx context.Context, err error) error {
	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) && apiErr.Code >= 400 && apiErr.Code < 500 && apiErr.Code != http.StatusGone {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(w.wait):
	}

	w.wait = min(w.wait*2, watchRetryMax)

	return nil
}
