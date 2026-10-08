package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// eventsAPI serves /api/v1/events (any query) with three events out of order, and records the
// queries asked; forbidden answers 403 instead.
type eventsAPI struct {
	mu        sync.Mutex
	queries   []string
	forbidden bool
}

const eventsJSON = `{"items":[
 {"metadata":{"name":"e1","namespace":"web","creationTimestamp":"2026-10-08T10:00:00Z"},"involvedObject":{"kind":"Pod","namespace":"web","name":"web-1"},
  "type":"Warning","reason":"BackOff","message":"back-off","count":3,"lastTimestamp":"2026-10-08T10:30:00Z"},
 {"metadata":{"name":"e2","namespace":"default","creationTimestamp":"2026-10-08T09:00:00Z"},"involvedObject":{"kind":"Node","name":"w1"},
  "type":"Normal","reason":"NodeReady","message":"ready","lastTimestamp":"2026-10-08T09:00:00Z"},
 {"metadata":{"name":"e3","namespace":"db","creationTimestamp":"2026-10-08T11:00:00Z"},"involvedObject":{"kind":"Pod","namespace":"db","name":"pg-1"},
  "type":"Warning","reason":"Unhealthy","message":"probe failed","lastTimestamp":"2026-10-08T11:00:00Z"}]}`

func (a *eventsAPI) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/version" {
		_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)

		return
	}

	if r.URL.Path != "/api/v1/events" {
		http.NotFound(w, r)

		return
	}

	a.mu.Lock()
	a.queries = append(a.queries, r.URL.RawQuery)
	forbidden := a.forbidden
	a.mu.Unlock()

	if forbidden {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"events is forbidden"}`)

		return
	}

	_, _ = io.WriteString(w, eventsJSON)
}

func useEventsAPI(t *testing.T, forbidden bool) *eventsAPI {
	t.Helper()

	a := &eventsAPI{forbidden: forbidden}
	f := newFakeKubeAPI(t, nil)
	f.Config.Handler = http.HandlerFunc(a.handler)
	useFakeKube(t, f)

	return a
}

func (a *eventsAPI) lastQuery() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.queries[len(a.queries)-1]
}

func decodeEvents(t *testing.T, out string) kubeEventList {
	t.Helper()

	var list kubeEventList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("%v in %s", err, out)
	}

	return list
}

func TestKubeClusterEvents(t *testing.T) {
	a := useEventsAPI(t, false)

	out, err := KubeClusterEvents("cfg", "ctx", "", true, 0)
	if err != nil {
		t.Fatal(err)
	}

	if q := a.lastQuery(); !strings.Contains(q, "type%21%3DNormal") {
		t.Errorf("warnings only asked with %q", q)
	}

	list := decodeEvents(t, out)
	if len(list.Events) != 3 || list.Events[0].Name != "pg-1" || list.Events[1].Name != "web-1" || list.Events[2].Kind != "Node" {
		t.Fatalf("not newest first: %+v", list.Events)
	}

	out, err = KubeClusterEvents("cfg", "ctx", "", false, 1)
	if err != nil {
		t.Fatal(err)
	}

	if q := a.lastQuery(); strings.Contains(q, "type") {
		t.Errorf("every type asked with %q", q)
	}

	if list = decodeEvents(t, out); len(list.Events) != 1 || list.Events[0].Reason != "Unhealthy" {
		t.Fatalf("limit ignored: %+v", list.Events)
	}

	// A cluster-scoped object: its events live outside any namespace path.
	if _, err := KubeEvents("cfg", "ctx", "", "", "Node", "w1"); err != nil {
		t.Fatal(err)
	}

	if q := a.lastQuery(); !strings.Contains(q, "involvedObject.kind%3DNode") || !strings.Contains(q, "involvedObject.name%3Dw1") {
		t.Errorf("node events asked with %q", q)
	}
}

func TestKubeClusterEventsForbidden(t *testing.T) {
	useEventsAPI(t, true)

	out, err := KubeClusterEvents("cfg", "ctx", "", true, 0)
	if err != nil {
		t.Fatal(err)
	}

	if list := decodeEvents(t, out); !list.Forbidden || len(list.Events) != 0 {
		t.Fatalf("%s", out)
	}

	// A namespace's events still fail: the caller named what it may not read.
	if _, err := KubeEvents("cfg", "ctx", "", "web", "", ""); err == nil {
		t.Fatal("a forbidden namespace listed")
	}
}

func TestKubeEventsNodeScopeValidationAndDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	for _, bad := range [][3]string{{"", "", "x"}, {"", "Node", ""}, {"", "", ""}} {
		if _, err := KubeEvents(yaml, "", "", bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}

	out, err := KubeEvents(yaml, "", "", "", "Node", "demo-worker-2")
	if err != nil {
		t.Fatal(err)
	}

	if list := decodeEvents(t, out); len(list.Events) != 1 || list.Events[0].Reason != "NodeNotSchedulable" {
		t.Fatalf("%s", out)
	}

	out, err = KubeClusterEvents(yaml, "", "", true, 0)
	if err != nil {
		t.Fatal(err)
	}

	if list := decodeEvents(t, out); len(list.Events) != 2 || list.Events[0].Type != "Warning" || list.Events[1].Type != "Warning" || list.Events[0].Last < list.Events[1].Last {
		t.Fatalf("%s", out)
	}

	out, err = KubeClusterEvents(yaml, "", "", false, 3)
	if err != nil {
		t.Fatal(err)
	}

	if list := decodeEvents(t, out); len(list.Events) != 3 {
		t.Fatalf("%s", out)
	}
}
