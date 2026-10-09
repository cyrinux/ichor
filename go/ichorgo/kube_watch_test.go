package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newWatchKubeAPI is a fake API server whose handler may stream: it answers watch requests
// event by event and holds them open until the client gives up. /version is answered for it.
func newWatchKubeAPI(t *testing.T, handle http.HandlerFunc) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{})
	f.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)

			return
		}

		f.mu.Lock()
		f.requests = append(f.requests, fakeKubeRequest{r.Method, r.URL.Path, "", r.Header.Get("Authorization"), "", r.URL.RawQuery})
		f.mu.Unlock()

		handle(w, r)
	})

	return f
}

// streamEvents writes watch events one per line, each flushed to the client at once.
func streamEvents(w http.ResponseWriter, events ...string) {
	for _, ev := range events {
		_, _ = io.WriteString(w, ev+"\n")

		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}
}

// holdOpen keeps a watch answer open, headers sent, until the client goes away.
func holdOpen(w http.ResponseWriter, r *http.Request) {
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}

	<-r.Context().Done()
}

func podWithVersion(name, rv string) string {
	return `{"metadata":{"name":"` + name + `","namespace":"shop","resourceVersion":"` + rv + `"},"spec":{"containers":[{"name":"c","image":"nginx"}]},"status":{"phase":"Running"}}`
}

func statusJSON(code int, message string) string {
	return `{"kind":"Status","code":` + strconv.Itoa(code) + `,"reason":"Expired","message":"` + message + `"}`
}

func watchClient(t *testing.T, f *fakeKubeAPI) *kubeClient {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k
}

// The list, its events with their versions, a bookmark moving the version on, the server's
// own timeout followed from the last version, and a 410 answered with the list again.
func TestWatchListFollowsEventsAndRelistsOnGone(t *testing.T) {
	var (
		mu       sync.Mutex
		lists    int
		versions []string
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/api/v1/namespaces/shop/pods" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		mu.Lock()
		defer mu.Unlock()

		if q.Get("watch") == "" {
			lists++
			if lists == 1 {
				_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"10"},"items":[`+podWithVersion("a", "9")+`]}`)
			} else {
				_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"20"},"items":[`+podWithVersion("a", "9")+`,`+podWithVersion("b", "19")+`]}`)
			}

			return
		}

		versions = append(versions, q.Get("resourceVersion"))

		switch len(versions) {
		case 1:
			if q.Get("allowWatchBookmarks") != "true" || q.Get("timeoutSeconds") == "" {
				t.Errorf("watch query %s", r.URL.RawQuery)
			}

			streamEvents(w,
				`{"type":"ADDED","object":`+podWithVersion("b", "11")+`}`,
				`{"type":"BOOKMARK","object":{"metadata":{"resourceVersion":"12"}}}`,
				`{"type":"MODIFIED","object":`+podWithVersion("b", "13")+`}`)
		case 2:
			w.WriteHeader(http.StatusGone)
			_, _ = io.WriteString(w, statusJSON(410, "too old resource version"))
		default:
			// The list came again, from which this watch starts: seen enough.
			cancel()
			mu.Unlock()
			holdOpen(w, r)
			mu.Lock()
		}
	})

	var got []string

	err := watchList(ctx, watchClient(t, f), watchSpec{path: "/api/v1/namespaces/shop/pods"}, func(ev watchEvent) error {
		got = append(got, ev.Type+":"+strconv.Itoa(len(ev.Page.items)))

		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ended with %v", err)
	}

	if want := "SYNC:1 ADDED:1 MODIFIED:1 SYNC:2"; strings.Join(got, " ") != want {
		t.Errorf("events %q, want %q", strings.Join(got, " "), want)
	}

	mu.Lock()
	defer mu.Unlock()

	if want := "10 13 20"; strings.Join(versions, " ") != want || lists != 2 {
		t.Errorf("watched from %q (%d lists), want %q", strings.Join(versions, " "), lists, want)
	}
}

// An ERROR event carrying a 410 starts the list over like the status would.
func TestWatchListStartsOverOnErrorEvent(t *testing.T) {
	var (
		mu    sync.Mutex
		lists int
	)

	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "" {
			mu.Lock()
			lists++
			mu.Unlock()
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"1"},"items":[]}`)

			return
		}

		mu.Lock()
		first := lists == 1
		mu.Unlock()

		if first {
			streamEvents(w, `{"type":"ERROR","object":`+statusJSON(410, "expired")+`}`)

			return
		}

		holdOpen(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncs := 0

	err := watchList(ctx, watchClient(t, f), watchSpec{path: "/api/v1/pods"}, func(ev watchEvent) error {
		if ev.Type == watchSync {
			syncs++
		}

		if syncs == 2 {
			cancel()
		}

		return nil
	})

	if !errors.Is(err, context.Canceled) || syncs != 2 {
		t.Fatalf("ended with %v after %d lists", err, syncs)
	}
}

// A list refused ends the watch at once, with the server's reason.
func TestWatchListEndsOnRefusal(t *testing.T) {
	f := newWatchKubeAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"pods is forbidden"}`)
	})

	start := time.Now()

	err := watchList(context.Background(), watchClient(t, f), watchSpec{path: "/api/v1/pods"}, func(watchEvent) error { return nil })
	if kubeCode(err) != http.StatusForbidden || !strings.Contains(err.Error(), "pods is forbidden") {
		t.Fatalf("ended with %v", err)
	}

	if time.Since(start) > watchRetryMin {
		t.Error("a refusal waited before ending")
	}
}

// A role that may list but not watch gets the list again every watchPollInterval instead.
func TestWatchListPollsWhenTheWatchIsRefused(t *testing.T) {
	watchPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { watchPollInterval = 5 * time.Second })

	var watches atomic.Int32

	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "" {
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"1"},"items":[`+jsonPod("shop", "a", "nginx")+`]}`)

			return
		}

		watches.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"cannot watch pods"}`)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	syncs := 0

	err := watchList(ctx, watchClient(t, f), watchSpec{path: "/api/v1/pods"}, func(ev watchEvent) error {
		if ev.Type == watchSync {
			syncs++
		}

		if syncs == 3 {
			cancel()
		}

		return nil
	})

	if !errors.Is(err, context.Canceled) || syncs != 3 || watches.Load() != 1 {
		t.Fatalf("ended with %v after %d lists and %d watch requests", err, syncs, watches.Load())
	}
}

// A stream the server stopped answering is given up once its timeout has passed, like one
// the server ended: followed again from the last version seen.
func TestWatchListGivesUpAStalledStream(t *testing.T) {
	watchStallAfter = 50 * time.Millisecond
	t.Cleanup(func() { watchStallAfter = watchTimeoutSeconds*time.Second + watchStreamGrace })

	var watches atomic.Int32

	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "" {
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"1"},"items":[]}`)

			return
		}

		watches.Add(1)
		holdOpen(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := &watcher{k: watchClient(t, f), spec: watchSpec{path: "/api/v1/pods"}, handle: func(watchEvent) error { return nil }, wait: watchRetryMin}
	if err := w.list(ctx); err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	if err := w.stream(ctx); err != nil || watches.Load() != 1 {
		t.Fatalf("a stalled stream ended with %v after %d requests", err, watches.Load())
	}

	if time.Since(start) > time.Second {
		t.Error("the stalled stream was not given up at watchStallAfter")
	}
}

// A Table watch on a server that sends the objects themselves still carries each change.
func TestWatchListTableFallsBackToObjects(t *testing.T) {
	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "" {
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"1"},"items":[{"metadata":{"name":"web","namespace":"shop"}}]}`)

			return
		}

		streamEvents(w, `{"type":"MODIFIED","object":{"kind":"Deployment","metadata":{"name":"web","namespace":"shop","resourceVersion":"2"}}}`)
		holdOpen(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var rows []string

	spec := watchSpec{path: "/apis/apps/v1/namespaces/shop/deployments", table: true}

	err := watchList(ctx, watchClient(t, f), spec, func(ev watchEvent) error {
		js, err := rowWatchItems(ev)
		if err != nil {
			return err
		}

		rows = append(rows, ev.Type+" "+js)
		if ev.Type == watchModified {
			cancel()
		}

		return nil
	})
	if !errors.Is(err, context.Canceled) || len(rows) != 2 || !strings.Contains(rows[1], `"name":"web"`) {
		t.Fatalf("ended with %v after %q", err, rows)
	}
}

// Table rows: the columns come with the list, the events carry the row alone.
func TestWatchListTableRowsKeepTheColumns(t *testing.T) {
	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("fieldSelector") != "metadata.name=web" || q.Get("includeObject") != "Metadata" {
			t.Errorf("query %s", r.URL.RawQuery)
		}

		if q.Get("watch") == "" {
			_, _ = io.WriteString(w, `{"kind":"Table","metadata":{"resourceVersion":"3"},"columnDefinitions":[{"name":"Name","type":"string"},{"name":"Ready","type":"string"}],
				"rows":[{"cells":["web","1/2"],"object":{"metadata":{"name":"web","namespace":"shop"}}}]}`)

			return
		}

		streamEvents(w, `{"type":"MODIFIED","object":{"kind":"Table","metadata":{"resourceVersion":"4"},"rows":[{"cells":["web","2/2"],"object":{"metadata":{"name":"web","namespace":"shop"}}}]}}`)
		holdOpen(w, r)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var got []string

	spec := watchSpec{path: "/apis/apps/v1/namespaces/shop/deployments", fieldSelector: "metadata.name=web", table: true}

	err := watchList(ctx, watchClient(t, f), spec, func(ev watchEvent) error {
		js, err := rowWatchItems(ev)
		if err != nil {
			return err
		}

		got = append(got, ev.Type+" "+js)
		if ev.Type == watchModified {
			cancel()
		}

		return nil
	})
	if !errors.Is(err, context.Canceled) || len(got) != 2 {
		t.Fatalf("ended with %v after %q", err, got)
	}

	var page kubeResourcePage
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[0], "SYNC ")), &page); err != nil || len(page.Columns) != 2 || page.Rows[0].Cells[1] != "1/2" || page.Continue != "" {
		t.Errorf("sync %s (%v)", got[0], err)
	}

	var row kubeResourceRow
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[1], "MODIFIED ")), &row); err != nil || row.Name != "web" || row.Namespace != "shop" || strings.Join(row.Cells, ",") != "web,2/2" {
		t.Errorf("row %s (%v)", got[1], err)
	}
}

func TestMergePages(t *testing.T) {
	first := kubePage{items: []json.RawMessage{json.RawMessage(`1`)}, continueToken: "t", remaining: 1, resourceVersion: "5"}
	second := kubePage{items: []json.RawMessage{json.RawMessage(`2`)}, resourceVersion: "5"}

	merged := mergePages(mergePages(kubePage{}, first), second)
	if len(merged.items) != 2 || merged.resourceVersion != "5" || merged.continueToken != "" || merged.remaining != 0 {
		t.Errorf("merged %+v", merged)
	}
}
