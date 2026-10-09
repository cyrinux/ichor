package ichorgo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// deploymentRow is a Table row of one Deployment at resourceVersion rv.
func deploymentRow(name, rv string) string {
	return `{"cells":["` + name + `"],"object":{"metadata":{"name":"` + name + `","namespace":"shop","resourceVersion":"` + rv + `"}}}`
}

func deploymentTable(rv string, rows ...string) string {
	return `{"kind":"Table","metadata":{"resourceVersion":"` + rv + `"},"columnDefinitions":[{"name":"Name","type":"string"}],"rows":[` + strings.Join(rows, ",") + `]}`
}

// noUpdate fails when rec gets an update within d.
func noUpdate(t *testing.T, rec *watchRecorder, d time.Duration) {
	t.Helper()

	select {
	case ev := <-rec.events:
		t.Fatalf("unexpected update %+v", ev)
	case msg := <-rec.done:
		t.Fatalf("ended early: %q", msg)
	case <-time.After(d):
	}
}

func TestStartKubeChangeWatchCoalescesABurstAfterTheLists(t *testing.T) {
	release := make(chan struct{})

	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		switch {
		case q.Get("includeObject") != "Metadata":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"kind":"Status","message":"not a Table request: `+r.URL.String()+`"}`)
		case r.URL.Path == "/apis/apps/v1/deployments" && q.Get("watch") == "":
			_, _ = io.WriteString(w, deploymentTable("1", deploymentRow("web", "1")))
		case r.URL.Path == "/apis/apps/v1/deployments":
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}

			streamEvents(w,
				`{"type":"MODIFIED","object":`+deploymentTable("2", deploymentRow("web", "2"))+`}`,
				`{"type":"MODIFIED","object":`+deploymentTable("3", deploymentRow("web", "3"))+`}`,
				`{"type":"ADDED","object":`+deploymentTable("4", deploymentRow("api", "4"))+`}`)
			holdOpen(w, r)
		case r.URL.Path == "/apis/apps/v1/statefulsets" && q.Get("watch") == "":
			_, _ = io.WriteString(w, deploymentTable("1"))
		case r.URL.Path == "/apis/apps/v1/statefulsets":
			holdOpen(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	rec := newWatchRecorder()
	run := StartKubeChangeWatch(kubeStoreFor(t, f), "admin@test", "", "", "apps/v1/deployments, apps/v1/statefulsets", rec)

	// The lists read at the start are not a change: a screen that just loaded stays put.
	noUpdate(t, rec, 2*liveDebounce)

	close(release)

	var change kubeChange
	if ev := rec.next(t); json.Unmarshal([]byte(ev.json), &change) != nil || change.Changed != 3 || change.At == "" {
		t.Fatalf("update %+v", ev)
	}

	noUpdate(t, rec, 2*liveDebounce)

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}
}

// A role that may list but not watch gets the list polled: only what differs from the last
// list is a change, so an idle cluster sends nothing.
func TestStartKubeChangeWatchPolledListCountsOnlyDifferences(t *testing.T) {
	old := watchPollInterval
	watchPollInterval = 20 * time.Millisecond

	t.Cleanup(func() { watchPollInterval = old })

	var scaled atomic.Bool

	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("watch") != "":
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"cannot watch"}`)
		case scaled.Load():
			_, _ = io.WriteString(w, deploymentTable("9", deploymentRow("web", "2"), deploymentRow("api", "1")))
		default:
			_, _ = io.WriteString(w, deploymentTable("5", deploymentRow("web", "1"), deploymentRow("api", "1")))
		}
	})

	rec := newWatchRecorder()
	run := StartKubeChangeWatch(kubeStoreFor(t, f), "admin@test", "", "shop", "apps/v1/deployments", rec)

	noUpdate(t, rec, 2*liveDebounce) // a dozen identical polls

	scaled.Store(true)

	var change kubeChange
	if ev := rec.next(t); json.Unmarshal([]byte(ev.json), &change) != nil || change.Changed != 1 {
		t.Fatalf("update %+v", ev)
	}

	run.Cancel()
	rec.ended(t)

	if r := f.recorded(); r[1].path != "/apis/apps/v1/namespaces/shop/deployments" {
		t.Errorf("namespaced path %s", r[1].path)
	}
}

func TestStartKubeChangeWatchRefusalsAndDemo(t *testing.T) {
	for _, tc := range []struct{ namespace, kinds, want string }{
		{"", "", "no kind"},
		{"", "deployments", "GROUP/VERSION/RESOURCE"},
		{"", "apps/v1/deployments/x", "GROUP/VERSION/RESOURCE"},
		{"", "apps/v1/Deploy ments", "invalid"},
		{"Not_A_Namespace", "apps/v1/deployments", "namespace"},
	} {
		rec := newWatchRecorder()
		StartKubeChangeWatch("", "", "", tc.namespace, tc.kinds, rec)

		if msg := rec.ended(t); !strings.Contains(msg, tc.want) {
			t.Errorf("%q %q ended with %q, want %q", tc.namespace, tc.kinds, msg, tc.want)
		}
	}

	// Nothing changes in the demo: no update, so no refresh loop.
	rec := newWatchRecorder()
	run := StartKubeChangeWatch(demoKubeconfigForTest(t), "", "", "", "apps/v1/deployments,v1/pods", rec)

	noUpdate(t, rec, 200*time.Millisecond)
	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("demo ended with %q", msg)
	}
}

// Two bursts close together are two signals at least changeFloor apart.
func TestSignalChangesKeepsTheFloor(t *testing.T) {
	old := changeFloor
	changeFloor = time.Second

	t.Cleanup(func() { changeFloor = old })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	counter := newChangeCounter()
	signals := make(chan time.Time, 4)

	go func() {
		_ = signalChanges(ctx, counter, make(chan error), func(string) { signals <- time.Now() }, time.Now)
	}()

	counter.add(2)
	first := <-signals

	counter.add(1)

	select {
	case second := <-signals:
		if gap := second.Sub(first); gap < changeFloor-50*time.Millisecond {
			t.Errorf("second signal %s after the first", gap)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no second signal")
	}
}
