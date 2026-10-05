package ichorgo

import (
	"archive/tar"
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// auditStart is a whole minute, so ten minutes of events fill exactly a ten-minute window.
var auditStart = time.Unix(1_800_000_000-1_800_000_000%60, 0).UTC()

type auditLine struct {
	at                       time.Duration // after auditStart
	user, agent, verb        string
	resource, namespace, obj string
	group, subresource, ip   string
	code                     int
	latency                  time.Duration
}

// lines renders a request as the audit log has it: RequestReceived, then for a watch
// ResponseStarted, then ResponseComplete.
func (l auditLine) lines(t *testing.T) []string {
	t.Helper()

	received := auditStart.Add(l.at)
	// As the API server logs them: a watch (long-running) also at ResponseStarted, refused
	// or not; a request refused for its credentials only there.
	stages := []string{"RequestReceived", "ResponseComplete"}

	switch {
	case l.code == 401:
		stages = []string{"ResponseStarted"}
	case l.verb == "watch":
		stages = []string{"RequestReceived", "ResponseStarted", "ResponseComplete"}
	}

	out := make([]string, 0, len(stages))

	for _, stage := range stages {
		e := map[string]any{
			"kind": "Event", "apiVersion": "audit.k8s.io/v1", "level": "Metadata", "stage": stage, "verb": l.verb,
			"user":      map[string]any{"username": l.user, "groups": []string{"system:authenticated"}},
			"userAgent": l.agent + "/v1.2.3 (linux/amd64) kubernetes/abcdef",
			"objectRef": map[string]any{"resource": l.resource, "namespace": l.namespace, "name": l.obj, "apiVersion": "v1",
				"apiGroup": l.group, "subresource": l.subresource},
			"sourceIPs":                []string{cmp.Or(l.ip, "10.0.0.1")},
			"requestReceivedTimestamp": received.Format(time.RFC3339Nano),
			"stageTimestamp":           received.Add(l.latency).Format(time.RFC3339Nano),
		}
		if l.code == 401 {
			e["user"] = map[string]any{}
		}

		if stage != "RequestReceived" {
			e["responseStatus"] = map[string]any{"code": l.code}
		}

		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}

		out = append(out, string(b))
	}

	return out
}

// every repeats a request every interval over ten minutes.
func every(interval time.Duration, l auditLine) []auditLine {
	var out []auditLine

	for at := time.Duration(0); at < 10*time.Minute; at += interval {
		l.at = at
		out = append(out, l)
	}

	return out
}

const (
	saExporter = "system:serviceaccount:monitoring:pod-exporter"
	saJellyfin = "system:serviceaccount:media:jellyfin"
	saArgo     = "system:serviceaccount:argocd:argocd-application-controller"
	saHass     = "system:serviceaccount:home:home-assistant"
	saKSM      = "system:serviceaccount:monitoring:kube-state-metrics"
)

// auditFixture is ten minutes of a cluster with one problem of each kind, and normal
// traffic that must not be reported (lease renewals every 2 s).
func auditFixture(t *testing.T) string {
	t.Helper()

	var requests []auditLine

	requests = append(requests, every(500*time.Millisecond, auditLine{user: saExporter, agent: "pod-exporter", verb: "list", resource: "pods", code: 200, latency: 300 * time.Millisecond})...)
	requests = append(requests, every(40*time.Second, auditLine{user: saExporter, agent: "pod-exporter", verb: "list", resource: "pods", code: 429})...)
	requests = append(requests, every(30*time.Second, auditLine{user: saJellyfin, agent: "jellyfin", verb: "list", resource: "configmaps", namespace: "media", code: 403})...)
	requests = append(requests, every(5*time.Second, auditLine{user: saArgo, agent: "argocd-application-controller", verb: "update", resource: "applications", namespace: "argocd", obj: "immich", code: 200})...)
	requests = append(requests, every(2*time.Second, auditLine{user: "system:kube-controller-manager", agent: "kube-controller-manager", verb: "update", resource: "leases", group: "coordination.k8s.io", namespace: "kube-system", obj: "kube-controller-manager", code: 200})...)
	requests = append(requests, every(20*time.Second, auditLine{user: saHass, agent: "home-assistant", verb: "watch", resource: "pods", namespace: "home", code: 200, latency: 18 * time.Second})...)
	requests = append(requests, every(50*time.Second, auditLine{user: saKSM, agent: "kube-state-metrics", verb: "list", resource: "podsecuritypolicies", code: 404})...)
	requests = append(requests, every(50*time.Second, auditLine{user: "admin", agent: "kubectl", verb: "list", resource: "events", code: 200, latency: 3 * time.Second})...)

	var b strings.Builder

	// Two hours older than the window: left out.
	for _, line := range (auditLine{at: -2 * time.Hour, user: "old", agent: "old", verb: "list", resource: "pods", code: 500}).lines(t) {
		b.WriteString(line + "\n")
	}

	b.WriteString("not json at all\n")

	for _, r := range requests {
		for _, line := range r.lines(t) {
			b.WriteString(line + "\n")
		}
	}

	return b.String()
}

// tarGz packs content as a Copy of the log file answers.
func tarGz(t *testing.T, name, content string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}

	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func readAuditFixture(t *testing.T) auditReport {
	t.Helper()

	window := newAuditWindow(10)

	events, err := scanAuditTarGz(bytes.NewReader(tarGz(t, "kube-apiserver.log", auditFixture(t))), window)
	if err != nil {
		t.Fatal(err)
	}

	if events == 0 {
		t.Fatal("no event parsed")
	}

	report, err := buildAuditReport([]*auditWindow{window}, []auditNodeRead{{Node: "cp1", Events: events}})
	if err != nil {
		t.Fatal(err)
	}

	return report
}

func findingOf(r auditReport, kind, user string) *auditFinding {
	for i, f := range r.Findings {
		if f.Kind == kind && f.Actor.User == user {
			return &r.Findings[i]
		}
	}

	return nil
}

func TestAuditFindsEachProblem(t *testing.T) {
	r := readAuditFixture(t)

	cases := []struct {
		kind, user, severity string
		count                int
	}{
		{findThrottled, saExporter, sevCritical, 15},
		{findListLoop, saExporter, sevCritical, 1215},
		{findForbidden, saJellyfin, sevWarning, 20},
		{findHotObject, saArgo, sevWarning, 120},
		{findWatchChurn, saHass, sevWarning, 30},
		{findMissingAPI, saKSM, sevWarning, 12},
		{findSlow, "admin", sevWarning, 12},
	}

	for _, c := range cases {
		f := findingOf(r, c.kind, c.user)
		if f == nil {
			t.Errorf("%s of %s not found in %+v", c.kind, c.user, r.Findings)

			continue
		}

		if f.Severity != c.severity || f.Count != c.count {
			t.Errorf("%s of %s: %s ×%d, want %s ×%d", c.kind, c.user, f.Severity, f.Count, c.severity, c.count)
		}
	}

	// Lease renewals every 2 s are what leader election does: not reported.
	if f := findingOf(r, findHotObject, "system:kube-controller-manager"); f != nil {
		t.Errorf("lease renewals reported: %+v", f)
	}

	if len(r.Findings) != len(cases) || r.Findings[0].Severity != sevCritical {
		t.Errorf("findings %+v", r.Findings)
	}
}

func TestAuditWindowAndActors(t *testing.T) {
	r := readAuditFixture(t)

	// The old request is out of the window; watches count once, not at each stage.
	if r.Requests != 1215+20+120+300+30+12+12 {
		t.Fatalf("requests %d", r.Requests)
	}

	if r.Seconds < 590 || r.Seconds > 600 || r.From != auditStart.UnixMilli() {
		t.Fatalf("span %v from %v", r.Seconds, r.From)
	}

	top := r.Actors[0]
	if top.Actor.Kind != "serviceAccount" || top.Actor.Namespace != "monitoring" || top.Actor.Name != "pod-exporter" ||
		top.Actor.Agent != "pod-exporter" || top.Throttled != 15 || top.TopVerb != "list" || top.TopResource != "pods" {
		t.Fatalf("top actor %+v", top)
	}

	// Every half second, give or take the 429s mixed in.
	if f := findingOf(r, findListLoop, saExporter); f.Value < 0.45 || f.Value > 0.55 {
		t.Fatalf("list interval %v", f.Value)
	}
}

func TestDescribeActor(t *testing.T) {
	cases := map[string]auditActor{
		"system:serviceaccount:ns:sa": {User: "system:serviceaccount:ns:sa", Kind: "serviceAccount", Namespace: "ns", Name: "sa"},
		"system:node:worker-1":        {User: "system:node:worker-1", Kind: "node", Name: "worker-1"},
		"system:kube-scheduler":       {User: "system:kube-scheduler", Kind: "controlPlane", Name: "kube-scheduler"},
		"system:anonymous":            {User: "system:anonymous", Kind: "anonymous", Name: "system:anonymous"},
		"admin":                       {User: "admin", Kind: "user", Name: "admin"},
	}

	for user, want := range cases {
		if got := describeActor(user, "", ""); got != want {
			t.Errorf("%s: %+v", user, got)
		}
	}

	if got := describeActor("", "curl", "198.51.100.7"); got.Kind != "unauthenticated" || got.Name != "198.51.100.7" {
		t.Errorf("unauthenticated: %+v", got)
	}

	if agentName("argocd-application-controller/v0.0.0 (linux/amd64) kubernetes/$Format") != "argocd-application-controller" || agentName("") != "" {
		t.Error("agentName")
	}
}

func TestAuditMissingLogAndNodes(t *testing.T) {
	if _, err := scanAuditTarGz(bytes.NewReader(tarGz(t, "other.txt", "x")), newAuditWindow(5)); err == nil {
		t.Fatal("a Copy without the log must fail")
	}

	// One node failing still gives the others' report; all failing is an error.
	ok, err := buildAuditReport([]*auditWindow{newAuditWindow(5), newAuditWindow(5)}, []auditNodeRead{{Node: "a", Error: "refused"}, {Node: "b"}})
	if err != nil || ok.Findings == nil || ok.Actors == nil {
		t.Fatalf("%+v %v", ok, err)
	}

	if _, err := buildAuditReport([]*auditWindow{newAuditWindow(5)}, []auditNodeRead{{Node: "a", Error: "refused"}}); err == nil || err.Error() != "refused" {
		t.Fatalf("all nodes failing: %v", err)
	}
}

func TestKubeAuditAnalysisDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeAuditAnalysis(cfg, "", 0)
	if err != nil {
		t.Fatal(err)
	}

	var r auditReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}

	kinds := map[string]bool{}
	for _, f := range r.Findings {
		kinds[f.Kind] = true
	}

	if r.Seconds != 15*60 || len(r.Nodes) != 3 || len(kinds) != 11 || r.Findings[0].Severity != sevCritical {
		t.Fatalf("demo %s", out)
	}
}

// windowOf reads requests into a ten-minute window, as one node's log.
func windowOf(t *testing.T, requests []auditLine) *auditWindow {
	t.Helper()

	var b strings.Builder

	for _, r := range requests {
		for _, line := range r.lines(t) {
			b.WriteString(line + "\n")
		}
	}

	w := newAuditWindow(10)
	if _, err := scanAuditLines(strings.NewReader(b.String()), w); err != nil {
		t.Fatal(err)
	}

	return w
}

// Five clients all getting 504 on their lease renewals: one finding about the server.
func TestAuditWidespreadErrorsAreTheServers(t *testing.T) {
	var requests []auditLine

	for i := range 5 {
		requests = append(requests, every(30*time.Second, auditLine{
			user: fmt.Sprintf("system:node:worker-%d", i), agent: "kubelet", verb: "update",
			resource: "leases", namespace: "kube-node-lease", obj: fmt.Sprintf("worker-%d", i), code: 504,
		})...)
	}

	r, err := buildAuditReport([]*auditWindow{windowOf(t, requests)}, []auditNodeRead{{Node: "cp1"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Findings) != 1 {
		t.Fatalf("findings %+v", r.Findings)
	}

	f := r.Findings[0]
	if f.Kind != findWideErrors || f.Actors != 5 || f.Count != 100 || f.Code != 504 || f.Resource != "leases" || f.Verb != "update" || f.Actor.User != "" {
		t.Fatalf("widespread %+v", f)
	}
}

// A node whose log stopped two hours earlier adds nothing to the window, but tells when.
func TestAuditAlignsNodesOnTheNewestEvent(t *testing.T) {
	recent := windowOf(t, every(10*time.Second, auditLine{user: "admin", agent: "kubectl", verb: "get", resource: "pods", namespace: "a", obj: "x", code: 200}))

	var stale []auditLine
	for _, l := range every(10*time.Second, auditLine{user: "old", agent: "kubectl", verb: "get", resource: "pods", code: 200}) {
		l.at -= 2 * time.Hour
		stale = append(stale, l)
	}

	r, err := buildAuditReport([]*auditWindow{recent, windowOf(t, stale)}, []auditNodeRead{{Node: "cp1"}, {Node: "cp2"}})
	if err != nil {
		t.Fatal(err)
	}

	if r.Seconds > 600 || r.Requests != 60 || len(r.Actors) != 1 {
		t.Fatalf("span %v, %d requests, actors %+v", r.Seconds, r.Requests, r.Actors)
	}

	if r.Nodes[1].Last == 0 || r.Nodes[1].Last > r.Nodes[0].Last-int64(time.Hour/time.Millisecond) {
		t.Fatalf("lag not told: %+v", r.Nodes)
	}

	if len(r.Findings) != 1 || r.Findings[0].Kind != findStaleLog || r.Findings[0].Name != "cp2" || r.Findings[0].Value < 7000 {
		t.Fatalf("stale log not reported: %+v", r.Findings)
	}
}

// An operator rewriting the status of three clusters every 5 s, and creating a role
// binding that exists in each namespace: two findings, not six.
func TestAuditGroupsAnActorsObjects(t *testing.T) {
	const operator = "system:serviceaccount:cnpg-system:cnpg-manager"

	var requests []auditLine

	for _, ns := range []string{"a", "b", "c"} {
		requests = append(requests, every(5*time.Second, auditLine{user: operator, agent: "manager", verb: "update", resource: "clusters/status", namespace: ns, obj: "db", code: 200})...)
		requests = append(requests, every(30*time.Second, auditLine{user: operator, agent: "manager", verb: "create", resource: "rolebindings", namespace: ns, code: 409})...)
	}

	r, err := buildAuditReport([]*auditWindow{windowOf(t, requests)}, []auditNodeRead{{Node: "cp1"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Findings) != 2 {
		t.Fatalf("findings %+v", r.Findings)
	}

	hot := findingOf(r, findHotObject, operator)
	if hot == nil || hot.Objects != 3 || hot.Count != 360 || hot.Value < 4.9 || hot.Value > 5.1 ||
		!slices.Equal(hot.Examples, []string{"a/db", "b/db", "c/db"}) || hot.Namespace != "" || hot.Name != "" {
		t.Fatalf("hot object %+v", hot)
	}

	exists := findingOf(r, findExists, operator)
	if exists == nil || exists.Objects != 3 || exists.Count != 60 || !slices.Equal(exists.Examples, []string{"a", "b", "c"}) {
		t.Fatalf("already exists %+v", exists)
	}
}

// What a healthy cluster does must not be reported, and the review's traps are counted right.
func TestAuditCountsLikeTheAPIServer(t *testing.T) {
	const ds = "system:serviceaccount:kube-system:cilium"

	var requests []auditLine

	// A token deleted with its secret: refused before the user is known.
	requests = append(requests, every(20*time.Second, auditLine{agent: "python-requests", verb: "get", resource: "pods", code: 401, ip: "192.0.2.40"})...)
	// A watch refused 5 times: 5 requests, not a loop of 10.
	requests = append(requests, every(2*time.Minute, auditLine{user: "u", agent: "a", verb: "watch", resource: "secrets", code: 403})...)
	// `kubectl logs -f` for minutes: not slow.
	requests = append(requests, every(time.Minute, auditLine{user: "admin", agent: "kubectl", verb: "get", resource: "pods", subresource: "log", namespace: "x", obj: "p", code: 200, latency: 5 * time.Minute})...)
	// The HPA lists pod metrics every 15 s by design.
	requests = append(requests, every(15*time.Second, auditLine{user: "system:serviceaccount:kube-system:horizontal-pod-autoscaler", agent: "kube-controller-manager", verb: "list", resource: "pods", group: "metrics.k8s.io", namespace: "web", code: 200})...)
	// A DaemonSet on 20 nodes, each listing nodes every 5 minutes.
	for i := range 20 {
		for at := time.Duration(i) * 15 * time.Second; at < 10*time.Minute; at += 5 * time.Minute {
			requests = append(requests, auditLine{at: at, user: ds, agent: "cilium-agent", verb: "list", resource: "nodes", code: 200, ip: fmt.Sprintf("10.1.0.%d", i)})
		}
	}
	// The kubelet's watches of mounted ConfigMaps last minutes: not churn. (They end within
	// the ten minutes, which a watch end would otherwise move.)
	for i := range 12 {
		requests = append(requests, auditLine{at: time.Duration(i) * 10 * time.Second, user: "system:node:w1", agent: "kubelet", verb: "watch",
			resource: "configmaps", namespace: "m", obj: fmt.Sprintf("c%d", i), code: 200, latency: 7 * time.Minute})
	}
	// An event recorded again and again is patched: event spam, not a hot object.
	requests = append(requests, every(5*time.Second, auditLine{user: "system:node:w1", agent: "kubelet", verb: "patch", resource: "events", namespace: "m", obj: "p.1", code: 200})...)

	r, err := buildAuditReport([]*auditWindow{windowOf(t, requests)}, []auditNodeRead{{Node: "cp1"}})
	if err != nil {
		t.Fatal(err)
	}

	kinds := map[string]int{}
	for _, f := range r.Findings {
		kinds[f.Kind]++
	}

	if len(r.Findings) != 1 || kinds[findUnauthorized] != 1 {
		t.Fatalf("findings %+v", r.Findings)
	}

	f := r.Findings[0]
	if f.Count != 30 || f.Actor.Kind != "unauthenticated" || f.Actor.Name != "192.0.2.40" || f.Actor.Agent != "python-requests" {
		t.Fatalf("unauthorized %+v", f)
	}

	// The refused watch counted once per request.
	for _, a := range r.Actors {
		if a.Actor.User == "u" && a.Requests != 5 {
			t.Fatalf("refused watch counted %d times", a.Requests)
		}
	}
}

// Events patched more than once a second are spam; the watch ends seconds after opening.
func TestAuditEventSpamAndShortWatches(t *testing.T) {
	var requests []auditLine

	requests = append(requests, every(500*time.Millisecond, auditLine{user: "system:node:w1", agent: "kubelet", verb: "patch", resource: "events", namespace: "m", obj: "p.1", code: 200})...)

	for _, resource := range []string{"pods", "services"} {
		requests = append(requests, every(25*time.Second, auditLine{user: "system:serviceaccount:t:traefik", agent: "traefik", verb: "watch", resource: resource, code: 200, latency: 20 * time.Second})...)
	}

	r, err := buildAuditReport([]*auditWindow{windowOf(t, requests)}, []auditNodeRead{{Node: "cp1"}})
	if err != nil {
		t.Fatal(err)
	}

	spam := findingOf(r, findEventSpam, "system:node:w1")
	churn := findingOf(r, findWatchChurn, "system:serviceaccount:t:traefik")

	if spam == nil || spam.Count != 1200 || churn == nil || churn.Objects != 2 || churn.Value < 19 || churn.Value > 21 ||
		!slices.Equal(churn.Examples, []string{"pods", "services"}) || len(r.Findings) != 2 {
		t.Fatalf("findings %+v", r.Findings)
	}
}

// One event longer than the buffer is skipped, the next ones still read.
func TestAuditSkipsOverlongLines(t *testing.T) {
	lines := auditLine{user: "admin", agent: "kubectl", verb: "get", resource: "pods", code: 200}.lines(t)
	text := strings.Repeat("x", auditMaxLine+10) + "\n" + strings.Join(lines, "\n") + "\n"

	events, err := scanAuditLines(strings.NewReader(text), newAuditWindow(5))
	if err != nil || events != len(lines) {
		t.Fatalf("%d events, %v", events, err)
	}
}
