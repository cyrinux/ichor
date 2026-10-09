package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAuditEventBodyAndMessage(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	e := auditEntry{Namespace: "web", Object: "Deployment/web", Action: "scale", Params: "replicas=3", Outcome: auditOK}

	body := auditEventBody(e, now)

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"reason":"IchorAction"`, `"type":"Normal"`, `"kind":"Deployment"`, `"name":"web"`, `"namespace":"web"`,
		`"generateName":"ichor-"`, `"reportingComponent":"ichor"`, `"action":"scale"`, `"firstTimestamp":"2026-10-09T12:00:00Z"`, `"message":"Ichor: scale replicas=3 (ok)"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("event lacks %s:\n%s", want, raw)
		}
	}

	failed := auditEntry{Action: "delete-pod", Outcome: auditFailed, Error: "pods \"web-1\" is forbidden"}
	if got := auditEventMessage(failed); got != `Ichor: delete-pod failed: pods "web-1" is forbidden` {
		t.Fatalf("failed message %q", got)
	}

	long := auditEntry{Action: "edit", Params: strings.Repeat("x", 2000), Outcome: auditOK}
	if got := auditEventMessage(long); len(got) > 1024 {
		t.Fatalf("message not clipped: %d bytes", len(got))
	}
}

func TestAuditEventWanted(t *testing.T) {
	SetAuditEvents(true)
	t.Cleanup(func() { SetAuditEvents(false) })

	cases := []struct {
		e    auditEntry
		want bool
	}{
		{auditEntry{Namespace: "web", Object: "Deployment/web"}, true},
		{auditEntry{Namespace: "web", Object: "web"}, false},           // no kind (a Longhorn volume name)
		{auditEntry{Node: "node-1", Object: "Service/kubelet"}, false}, // a Talos action: no namespace
		{auditEntry{Node: "node-1"}, false},                            // a reboot
		{auditEntry{Namespace: "web", Object: "Deployment/web", Demo: true}, false},
	}

	for i, c := range cases {
		if got := auditEventWanted(c.e); got != c.want {
			t.Errorf("case %d: wanted %v, got %v", i, c.want, got)
		}
	}

	SetAuditEvents(false)

	if auditEventWanted(auditEntry{Namespace: "web", Object: "Deployment/web"}) {
		t.Fatal("events posted while the setting is off")
	}
}

// TestRecordOutcomePostsEvent: with the setting on, a recorded Kubernetes action posts one
// Event on its object, on the cluster the action ran on; a failure to post is swallowed.
func TestRecordOutcomePostsEvent(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"POST /api/v1/namespaces/web/events": `{"kind":"Event","metadata":{"name":"ichor-abc12"}}`,
	})

	SetAuditEvents(true)
	t.Cleanup(func() { SetAuditEvents(false); auditEventPosted = nil })

	posted := make(chan struct{}, 4)
	auditEventPosted = func() { posted <- struct{}{} }

	cfg := f.kubeconfigFor(f.URL)

	recordOutcome(cfg, "admin@test", auditAction{Action: "scale", Namespace: "web", Object: "Deployment/web", Params: "replicas=2"}, nil)
	waitPosted(t, posted)

	// A refused Event (a namespace that is gone) changes nothing for the caller.
	recordOutcome(cfg, "admin@test", auditAction{Action: "delete-pod", Namespace: "gone", Object: "Pod/web-1"}, errors.New("boom"))
	waitPosted(t, posted)

	// A node action has no object: nothing is posted.
	recordOutcome(cfg, "admin@test", auditAction{Action: "reboot", Node: "node-1"}, nil)

	var events []fakeKubeRequest
	for _, r := range f.recorded() {
		if r.method == "POST" {
			events = append(events, r)
		}
	}

	if len(events) != 2 || events[0].path != "/api/v1/namespaces/web/events" || events[1].path != "/api/v1/namespaces/gone/events" {
		t.Fatalf("posted %+v", events)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(events[0].body), &body); err != nil {
		t.Fatal(err)
	}

	if body["message"] != "Ichor: scale replicas=2 (ok)" || body["reason"] != auditEventReason {
		t.Fatalf("body %v", body)
	}

	if msg, _ := json.Marshal(body); strings.Contains(string(msg), "admin@test") {
		t.Fatalf("the Event names the cluster:\n%s", msg)
	}
}

func waitPosted(t *testing.T, posted <-chan struct{}) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	select {
	case <-posted:
	case <-ctx.Done():
		t.Fatal("no Event posted in time")
	}
}
