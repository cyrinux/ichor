package ichorgo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
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

	partial := auditEntry{Action: "sanitize-pods", Params: "pods=2", Outcome: auditPartial, Error: "1 of 2 pods not deleted"}
	if got := auditEventMessage(partial); got != "Ichor: sanitize-pods pods=2 partial: 1 of 2 pods not deleted" {
		t.Fatalf("partial message %q", got)
	}

	long := auditEntry{Action: "edit", Params: strings.Repeat("x", 2000), Outcome: auditOK}
	if got := auditEventMessage(long); len(got) > 1024 {
		t.Fatalf("message not clipped: %d bytes", len(got))
	}
}

// recordActionsOn makes exactly these clusters record actions for the test.
func recordActionsOn(t *testing.T, fingerprints ...string) {
	t.Helper()

	SetAuditEventClusters(strings.Join(fingerprints, "\n"))
	t.Cleanup(func() { SetAuditEventClusters("") })
}

func TestAuditEventWanted(t *testing.T) {
	recordActionsOn(t, "aaaaaaaaaaaaaaaa", " bbbbbbbbbbbbbbbb ", "")

	object := auditEntry{Namespace: "web", Object: "Deployment/web"}
	cases := []struct {
		name        string
		e           auditEntry
		fingerprint string
		want        bool
	}{
		{"a cluster that records", object, "aaaaaaaaaaaaaaaa", true},
		{"spaces trimmed", object, "bbbbbbbbbbbbbbbb", true},
		{"another cluster", object, "cccccccccccccccc", false},
		{"unknown context", object, "", false},
		{"no kind (a Longhorn volume name)", auditEntry{Namespace: "web", Object: "web"}, "aaaaaaaaaaaaaaaa", false},
		{"a Talos action: no namespace", auditEntry{Node: "node-1", Object: "Service/kubelet"}, "aaaaaaaaaaaaaaaa", false},
		{"a reboot", auditEntry{Node: "node-1"}, "aaaaaaaaaaaaaaaa", false},
		{"the demo", auditEntry{Namespace: "web", Object: "Deployment/web", Demo: true}, "aaaaaaaaaaaaaaaa", false},
	}

	for _, c := range cases {
		if got := auditEventWanted(c.e, c.fingerprint); got != c.want {
			t.Errorf("%s: wanted %v, got %v", c.name, c.want, got)
		}
	}

	SetAuditEventClusters("")

	if auditEventsSomewhere() || auditEventWanted(object, "aaaaaaaaaaaaaaaa") {
		t.Fatal("events posted with no cluster recording")
	}
}

// The fingerprint of an action's context is the one the apps see in the config summary, for
// a Talos context and for a kubeconfig one, and "" for a context the config does not have.
func TestAuditClusterFingerprintMatchesTheSummary(t *testing.T) {
	talos := testConfig(t, time.Now().Add(24*time.Hour))

	var summary configSummary
	if err := json.Unmarshal([]byte(mustParse(t, ParseConfig, talos)), &summary); err != nil {
		t.Fatal(err)
	}

	for _, c := range summary.Contexts {
		if got := auditClusterFingerprint(talos, c.Name); got != c.Fingerprint || got == "" {
			t.Errorf("talos %s: %q, the summary says %q", c.Name, got, c.Fingerprint)
		}
	}

	if got, want := auditClusterFingerprint(talos, ""), summary.Contexts[0].Fingerprint; got != want {
		t.Errorf("talos current context: %q, want %q (lab)", got, want)
	}

	kube := newFakeKubeAPI(t, nil).kubeconfigFor("https://kube.invalid:6443")

	var kubeSummary kubeconfigSummary
	if err := json.Unmarshal([]byte(mustParse(t, ParseKubeconfig, kube)), &kubeSummary); err != nil {
		t.Fatal(err)
	}

	for _, c := range kubeSummary.Contexts {
		if got := auditClusterFingerprint(kube, c.Name); got != c.Fingerprint || got == "" {
			t.Errorf("kube %s: %q, the summary says %q", c.Name, got, c.Fingerprint)
		}
	}

	if auditClusterFingerprint(talos, "nope") != "" || auditClusterFingerprint(kube, "nope") != "" || auditClusterFingerprint("", "lab") != "" {
		t.Error("an unknown context has a fingerprint")
	}
}

func mustParse(t *testing.T, parse func(string) (string, error), config string) string {
	t.Helper()

	out, err := parse(config)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

// Two clusters, one recording: the Event reaches only its API server.
func TestRecordOutcomePostsOnlyOnClustersThatRecord(t *testing.T) {
	answers := map[string]string{"POST /api/v1/namespaces/web/events": `{"kind":"Event","metadata":{"name":"ichor-abc12"}}`}
	on, off := newFakeKubeAPI(t, answers), newFakeKubeAPI(t, answers)
	cfg := twoClusterKubeconfig(on, off)

	recordActionsOn(t, auditClusterFingerprint(cfg, "admin@on"))
	t.Cleanup(func() { auditEventPosted = nil })

	posted := make(chan struct{}, 4)
	auditEventPosted = func() { posted <- struct{}{} }

	scale := auditAction{Action: "scale", Namespace: "web", Object: "Deployment/web", Params: "replicas=2"}
	recordOutcome(cfg, "admin@off", scale, nil)
	recordOutcome(cfg, "admin@on", scale, nil)
	waitPosted(t, posted)

	if got := countPosts(on); got != 1 {
		t.Errorf("the recording cluster got %d Events, want 1", got)
	}

	if got := countPosts(off); got != 0 {
		t.Errorf("the other cluster got %d Events, want none", got)
	}
}

func countPosts(f *fakeKubeAPI) int {
	n := 0

	for _, r := range f.recorded() {
		if r.method == "POST" {
			n++
		}
	}

	return n
}

// twoClusterKubeconfig has the contexts admin@on and admin@off, each on its own fake API server.
func twoClusterKubeconfig(on, off *fakeKubeAPI) string {
	ca := func(f *fakeKubeAPI) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.Certificate().Raw}))
	}

	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: admin@on
clusters:
- name: on
  cluster:
    server: %s
    certificate-authority-data: %s
- name: off
  cluster:
    server: %s
    certificate-authority-data: %s
users:
- name: admin
  user:
    token: secret-token
contexts:
- name: admin@on
  context: {cluster: on, user: admin}
- name: admin@off
  context: {cluster: off, user: admin}
`, on.URL, ca(on), off.URL, ca(off))
}

// TestRecordOutcomePostsEvent: on a cluster that records actions, a recorded Kubernetes action
// posts one Event on its object, on the cluster the action ran on; a failure to post is swallowed.
func TestRecordOutcomePostsEvent(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"POST /api/v1/namespaces/web/events": `{"kind":"Event","metadata":{"name":"ichor-abc12"}}`,
	})

	cfg := f.kubeconfigFor(f.URL)

	recordActionsOn(t, auditClusterFingerprint(cfg, "admin@test"))
	t.Cleanup(func() { auditEventPosted = nil })

	posted := make(chan struct{}, 4)
	auditEventPosted = func() { posted <- struct{}{} }

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
