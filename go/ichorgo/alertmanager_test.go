package ichorgo

import (
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const amTestSource = `{"mode":"proxy","kind":"alertmanager","namespace":"monitoring","service":"alertmanager-operated","port":9093}`

const amTestProxy = "/api/v1/namespaces/monitoring/services/alertmanager-operated:9093/proxy"

const amTestAlerts = `[
 {"fingerprint":"f1","labels":{"alertname":"DiskFull","severity":"critical","instance":"n1"},"annotations":{"summary":"disk","description":"full","runbook_url":"https://r/1"},
  "startsAt":"2026-01-01T10:00:00.5Z","endsAt":"2026-01-01T11:00:00Z","updatedAt":"2026-01-01T10:01:00Z","receivers":[{"name":"pager"}],"status":{"state":"active","silencedBy":[],"inhibitedBy":[]}},
 {"fingerprint":"f2","labels":{"alertname":"DiskFull","severity":"warning","instance":"n2"},"annotations":{"message":"old style"},
  "startsAt":"2026-01-01T12:00:00Z","endsAt":"2026-01-01T13:00:00Z","receivers":[{"name":"chat"}],"status":{"state":"suppressed","silencedBy":["s1"],"inhibitedBy":[]}},
 {"fingerprint":"f3","labels":{"alertname":"PodCrash","severity":"Warn"},"annotations":{},"startsAt":"2026-01-01T09:00:00Z","endsAt":"","receivers":[],"status":{"state":"active"}},
 {"fingerprint":"f4","labels":{"alertname":"Watchdog","severity":"none"},"startsAt":"2026-01-01T08:00:00Z","status":{"state":"unprocessed"}},
 {"fingerprint":"f5","labels":{"alertname":"Odd","severity":"p5"},"startsAt":"2026-01-01T08:00:00Z","status":{"state":"active"}},
 {"fingerprint":"f6","labels":{"alertname":"Odd"},"startsAt":"2026-01-01T08:30:00Z","status":{"state":"suppressed","inhibitedBy":["f1"]}}
]`

func TestParseAMAlerts(t *testing.T) {
	res, err := parseAMAlerts([]byte(amTestAlerts))
	if err != nil {
		t.Fatal(err)
	}

	if want := (amCounts{Critical: 1, Warning: 1, Info: 1, Other: 1, Suppressed: 2}); res.Counts != want || res.Total != 6 || res.Truncated {
		t.Fatalf("counts %+v total %d", res.Counts, res.Total)
	}

	var order []string
	for _, g := range res.Groups {
		order = append(order, fmt.Sprintf("%s:%s:%d/%d", g.Alertname, g.Severity, g.Active, g.Count))
	}

	if got, want := strings.Join(order, " "), "DiskFull:critical:1/2 PodCrash:warning:1/1 Watchdog:info:1/1 Odd:other:1/2"; got != want {
		t.Fatalf("groups %s, want %s", got, want)
	}

	disk := res.Groups[0].Alerts
	if disk[0].Fingerprint != "f1" || disk[1].Fingerprint != "f2" {
		t.Fatalf("active alerts first: %+v", disk)
	}

	a := disk[0]
	if a.Summary != "disk" || a.Description != "full" || a.RunbookURL != "https://r/1" || a.StartsAt != 1767261600500 || a.Receivers[0] != "pager" || a.State != amStateActive {
		t.Fatalf("alert %+v", a)
	}

	if b := disk[1]; b.Description != "old style" || b.SilencedBy[0] != "s1" || b.State != amStateSuppressed {
		t.Fatalf("suppressed %+v", b)
	}

	out, _ := json.Marshal(res.Groups[1].Alerts[0])
	for _, want := range []string{`"receivers":[]`, `"silencedBy":[]`, `"inhibitedBy":[]`, `"annotations":{}`, `"endsAt":0`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("%s lacks %s", out, want)
		}
	}

	if _, err := parseAMAlerts([]byte(`{"status":"success"}`)); err == nil {
		t.Fatal("a Prometheus answer was taken for alerts")
	}
}

func TestGroupAMAlertsTruncates(t *testing.T) {
	alerts := make([]amAlert, amMaxAlerts+5)
	for i := range alerts {
		alerts[i] = amAlert{Fingerprint: fmt.Sprint(i), Alertname: "Many", Severity: amSeverityWarning, State: amStateActive}
	}

	res := groupAMAlerts(alerts)
	if !res.Truncated || res.Total != amMaxAlerts+5 || res.Groups[0].Count != amMaxAlerts || res.Counts.Warning != amMaxAlerts+5 {
		t.Fatalf("truncated %v total %d count %d", res.Truncated, res.Total, res.Groups[0].Count)
	}
}

func TestAMSeverity(t *testing.T) {
	for in, want := range map[string]string{"critical": "critical", " CRIT ": "critical", "page": "critical", "warning": "warning", "error": "warning",
		"info": "info", "none": "info", "": "other", "p3": "other"} {
		if got := amSeverity(in); got != want {
			t.Errorf("%q: %s, want %s", in, got, want)
		}
	}
}

func TestParseAMSilences(t *testing.T) {
	body := `[
	 {"id":"e1","status":{"state":"expired"},"matchers":[{"name":"a","value":"1","isRegex":false,"isEqual":true}],"startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-01T01:00:00Z"},
	 {"id":"a2","status":{"state":"active"},"matchers":[],"createdBy":"me","comment":"later","startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-03T00:00:00Z"},
	 {"id":"p1","status":{"state":"pending"},"startsAt":"2026-01-05T00:00:00Z","endsAt":"2026-01-06T00:00:00Z"},
	 {"id":"a1","status":{"state":"active"},"createdBy":"me","comment":"sooner","startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-02T00:00:00Z"},
	 {"id":"e2","status":{"state":"expired"},"startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-01T05:00:00Z"}
	]`

	for _, tc := range []struct {
		expired bool
		want    string
	}{{false, "a1 a2 p1"}, {true, "a1 a2 p1 e2 e1"}} {
		res, err := parseAMSilences([]byte(body), tc.expired)
		if err != nil {
			t.Fatal(err)
		}

		var ids []string
		for _, s := range res.Silences {
			ids = append(ids, s.ID)
		}

		if got := strings.Join(ids, " "); got != tc.want {
			t.Errorf("expired=%v: %s, want %s", tc.expired, got, tc.want)
		}

		if s := res.Silences[0]; s.Comment != "sooner" || s.EndsAt != time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli() || s.Matchers == nil {
			t.Errorf("first %+v", s)
		}
	}

	if _, err := parseAMSilences([]byte(`"nope"`), false); err == nil {
		t.Fatal("bad answer accepted")
	}
}

func TestSortAMSilencesCapsExpired(t *testing.T) {
	var silences []amSilence
	for i := range amMaxExpired + 3 {
		silences = append(silences, amSilence{ID: fmt.Sprint(i), State: amStateExpired, EndsAt: int64(i)})
	}

	silences = append(silences, amSilence{ID: "live", State: amStateActive})

	got := sortAMSilences(silences)
	if len(got) != amMaxExpired+1 || got[0].ID != "live" || got[1].ID != fmt.Sprint(amMaxExpired+2) {
		t.Fatalf("got %d, first %s %s", len(got), got[0].ID, got[1].ID)
	}
}

func TestParseAMMatchers(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string // the filter syntax, or "error"
	}{
		{``, ``},
		{`[]`, ``},
		{`[{"name":"alertname","value":"X"}]`, `alertname="X"`},
		{`[{"name":" job ","value":"a\"b","isEqual":false}]`, `job!="a\"b"`},
		{`[{"name":"pod","value":"web-.*","isRegex":true}]`, `pod=~"web-.*"`},
		{`[{"name":"pod","value":"web-.*","isRegex":true,"isEqual":false}]`, `pod!~"web-.*"`},
		{`[{"name":"pod","value":"(","isRegex":true}]`, `error`},
		{`[{"name":"","value":"x"}]`, `error`},
		{`[{"name":"a b","value":"x"}]`, `error`},
		{`[{"name":"a","value":"x\u0000"}]`, `error`},
		{`[{"name":"a","value":"` + strings.Repeat("x", amMaxLabelValue+1) + `"}]`, `error`},
		{`[` + strings.Repeat(`{"name":"a","value":"b"},`, amMaxMatchers) + `{"name":"a","value":"b"}]`, `error`},
		{`{"name":"a"}`, `error`},
	} {
		matchers, err := parseAMMatchers(tc.in)

		got := amMatchersText(matchers)
		got = strings.TrimSuffix(strings.TrimPrefix(got, "{"), "}")

		if err != nil {
			got = "error"
		}

		if got != tc.want {
			t.Errorf("%.60s: got %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestAMMatcherMatches(t *testing.T) {
	labels := map[string]string{"alertname": "X", "pod": "web-1"}

	for _, tc := range []struct {
		m    amMatcher
		want bool
	}{
		{amMatcher{Name: "alertname", Value: "X", IsEqual: true}, true},
		{amMatcher{Name: "alertname", Value: "X"}, false},
		{amMatcher{Name: "pod", Value: "web-.*", IsRegex: true, IsEqual: true}, true},
		{amMatcher{Name: "pod", Value: "web", IsRegex: true, IsEqual: true}, false},
		{amMatcher{Name: "missing", Value: "", IsEqual: true}, true},
		{amMatcher{Name: "pod", Value: "db-.*", IsRegex: true}, true},
	} {
		if got := tc.m.matches(labels); got != tc.want {
			t.Errorf("%s: %v", tc.m.filter(), got)
		}
	}

	if !amMatchesEverything([]amMatcher{{Name: "a", Value: ".*", IsRegex: true, IsEqual: true}, {Name: "b", Value: "x"}}) {
		t.Error(".* and != match everything")
	}

	if amMatchesEverything([]amMatcher{{Name: "a", Value: ".*", IsRegex: true, IsEqual: true}, {Name: "b", Value: "x", IsEqual: true}}) {
		t.Error("b=x does not match everything")
	}
}

func TestAlertmanagerSilenceMatchers(t *testing.T) {
	out, err := AlertmanagerSilenceMatchers(`{"severity":"warning","pod":"web-1","alertname":"KubePodCrashLooping","prometheus_replica":"prometheus-0","namespace":"shop","empty":""}`)
	if err != nil {
		t.Fatal(err)
	}

	var matchers []amMatcher
	if err := json.Unmarshal([]byte(out), &matchers); err != nil {
		t.Fatal(err)
	}

	if got, want := amMatchersText(matchers), `{alertname="KubePodCrashLooping", namespace="shop", pod="web-1", severity="warning"}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	if _, err := AlertmanagerSilenceMatchers(`["x"]`); err == nil {
		t.Fatal("bad labels accepted")
	}
}

func TestAMCandidates(t *testing.T) {
	var services []promService

	add := func(ns, name string, labels map[string]string, ports string) {
		var s promService
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"metadata":{"name":%q,"namespace":%q},"spec":{"ports":[%s]}}`, name, ns, ports)), &s); err != nil {
			t.Fatal(err)
		}

		s.Metadata.Labels = labels
		services = append(services, s)
	}

	am := map[string]string{"app.kubernetes.io/name": "alertmanager"}

	add("monitoring", "kps-kube-prometheus-stack-alertmanager", nil, `{"name":"http-web","port":9093},{"name":"reloader-web","port":8080}`)
	add("monitoring", "alertmanager-operated", nil, `{"name":"web","port":9093},{"name":"tcp-mesh","port":9094}`)
	add("monitoring", "alertmanager-main", am, `{"name":"web","port":9093}`)
	add("monitoring", "kps-kube-prometheus-stack-prometheus", nil, `{"name":"http-web","port":9090}`)
	add("monitoring", "alertmanager-webhook-relay", nil, `{"name":"http","port":9093}`)
	add("monitoring", "kps-kube-prometheus-stack-operator", nil, `{"name":"https","port":443}`)
	add("vm", "vmalertmanager-main", map[string]string{"app.kubernetes.io/name": "vmalertmanager"}, `{"name":"http","port":9093}`)
	add("ops", "alerts", map[string]string{"app": "alertmanager"}, `{"name":"http","port":9093}`)
	add("mimir", "mimir-alertmanager", nil, `{"name":"http-metrics","port":8080},{"name":"grpc","port":9095}`)
	add("mimir", "mimir-alertmanager-headless", nil, `{"name":"grpc","port":9095}`)
	add("default", "alertmanager-udp", nil, `{"name":"web","port":9093,"protocol":"UDP"}`)

	var got []string
	for _, s := range rankSources(services, amMatch) {
		got = append(got, fmt.Sprintf("%s %s/%s:%d%s", s.Kind, s.Namespace, s.Service, s.Port, s.PathPrefix))
	}

	want := []string{
		"alertmanager monitoring/alertmanager-operated:9093",
		"alertmanager monitoring/alertmanager-main:9093",
		"alertmanager monitoring/kps-kube-prometheus-stack-alertmanager:9093",
		"alertmanager vm/vmalertmanager-main:9093",
		"alertmanager ops/alerts:9093",
		"mimir mimir/mimir-alertmanager:8080/alertmanager",
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Prometheus discovery still leaves Alertmanager out.
	for _, s := range promCandidates(services) {
		if strings.Contains(s.Service, "alertmanager") {
			t.Errorf("PromDiscover offers %s", s.Service)
		}
	}
}

func TestAlertmanagerThroughServiceProxy(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/services":                         `{"items":[{"metadata":{"name":"alertmanager-operated","namespace":"monitoring"},"spec":{"ports":[{"name":"web","port":9093}]}}]}`,
		"GET " + amTestProxy + "/api/v2/alerts":        amTestAlerts,
		"GET " + amTestProxy + "/api/v2/silences":      `[{"id":"s1","status":{"state":"active"},"matchers":[{"name":"alertname","value":"DiskFull","isRegex":false,"isEqual":true}],"createdBy":"ichor","comment":"c","startsAt":"2026-01-01T00:00:00Z","endsAt":"2026-01-02T00:00:00Z"}]`,
		"POST " + amTestProxy + "/api/v2/silences":     `{"silenceID":"new-1"}`,
		"DELETE " + amTestProxy + "/api/v2/silence/s1": ``,
	})
	f.answerWith("DELETE "+amTestProxy+"/api/v2/silence/gone", http.StatusNotFound, `"silence gone not found"`)
	useFakeKube(t, f)

	out, err := AlertmanagerDiscover("cfg", "ctx", "")
	if err != nil || !strings.Contains(out, `"service":"alertmanager-operated","port":9093`) {
		t.Fatalf("discover: %s %v", out, err)
	}

	out, err = AlertmanagerAlerts("cfg", "ctx", "", amTestSource, true, false, true, "pager|chat", `[{"name":"alertname","value":"DiskFull"},{"name":"instance","value":"n.*","isRegex":true}]`)
	if err != nil || !strings.Contains(out, `"counts":{"critical":1,"warning":1,"info":1,"other":1,"suppressed":2}`) {
		t.Fatalf("alerts: %s %v", out, err)
	}

	last := f.recorded()[len(f.recorded())-1]
	q, _ := url.ParseQuery(last.query)
	if q.Get("active") != "true" || q.Get("unprocessed") != "true" || q.Get("silenced") != "false" || q.Get("inhibited") != "true" || q.Get("receiver") != "pager|chat" ||
		strings.Join(q["filter"], " ") != `alertname="DiskFull" instance=~"n.*"` {
		t.Fatalf("alerts query %q", last.query)
	}

	if out, err = AlertmanagerSilences("cfg", "ctx", "", amTestSource, false); err != nil || !strings.Contains(out, `"id":"s1","state":"active"`) {
		t.Fatalf("silences: %s %v", out, err)
	}

	withAMClock(t, time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))

	id, err := AlertmanagerSilence("cfg", "ctx", "", amTestSource, `[{"name":"alertname","value":"DiskFull"}]`, 90, "  disk being replaced ")
	if err != nil || id != "new-1" {
		t.Fatalf("silence: %q %v", id, err)
	}

	post := f.recorded()[len(f.recorded())-1]

	var sent map[string]any
	if err := json.Unmarshal([]byte(post.body), &sent); err != nil || post.method != http.MethodPost || post.contentType != "application/json" {
		t.Fatalf("post %+v", post)
	}

	if sent["startsAt"] != "2026-03-01T12:00:00Z" || sent["endsAt"] != "2026-03-01T13:30:00Z" || sent["createdBy"] != "ichor" || sent["comment"] != "disk being replaced" {
		t.Fatalf("sent %v", sent)
	}

	if err := AlertmanagerExpire("cfg", "ctx", "", amTestSource, "s1"); err != nil {
		t.Fatal(err)
	}

	if err := AlertmanagerExpire("cfg", "ctx", "", amTestSource, "gone"); err == nil || !strings.Contains(err.Error(), "Alertmanager refused") {
		t.Fatalf("expire unknown: %v", err)
	}

	entries := readAudit(t, "", "")
	if len(entries) != 3 {
		t.Fatalf("got %d audit entries", len(entries))
	}

	if e := entries[2]; e.Action != "alertmanager-silence" || e.Namespace != "monitoring" || e.Object != "Service/alertmanager-operated" || e.Outcome != auditOK ||
		e.Params != `{alertname="DiskFull"} for 90m: disk being replaced` {
		t.Errorf("silence entry %+v", e)
	}

	if e := entries[1]; e.Action != "alertmanager-expire" || e.Params != "silence=s1" || e.Outcome != auditOK {
		t.Errorf("expire entry %+v", e)
	}

	if e := entries[0]; e.Outcome != auditFailed {
		t.Errorf("failed expire entry %+v", e)
	}
}

func withAMClock(t *testing.T, at time.Time) {
	t.Helper()

	saved := amNow
	amNow = func() time.Time { return at }

	t.Cleanup(func() { amNow = saved })
}

func TestAlertmanagerErrors(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET " + amTestProxy + "/api/v2/alerts":    `not json`,
		"POST " + amTestProxy + "/api/v2/silences": `{"code":602,"message":"matchers in body is required"}`,
	})
	f.answerWith("POST "+amTestProxy+"/api/v2/silences", http.StatusBadRequest, `{"code":602,"message":"matchers in body is required"}`)
	useFakeKube(t, f)

	if _, err := AlertmanagerAlerts("cfg", "ctx", "", amTestSource, true, true, true, "", ""); err == nil || !strings.Contains(err.Error(), "not an Alertmanager answer") {
		t.Fatalf("bad answer: %v", err)
	}

	other := strings.Replace(amTestSource, "alertmanager-operated", "nope", 1)
	if _, err := AlertmanagerSilences("cfg", "ctx", "", other, false); err == nil || !strings.Contains(err.Error(), "no such Service") {
		t.Fatalf("missing service: %v", err)
	}

	if _, err := AlertmanagerSilence("cfg", "ctx", "", amTestSource, `[{"name":"a","value":"b"}]`, 5, "c"); err == nil || !strings.Contains(err.Error(), "Alertmanager refused: matchers in body is required") {
		t.Fatalf("refused: %v", err)
	}

	if _, err := AlertmanagerAlerts("cfg", "ctx", "", amTestSource, true, true, true, "(", ""); err == nil {
		t.Fatal("bad receiver regex accepted")
	}

	if _, err := AlertmanagerAlerts("cfg", "ctx", "", `{"mode":"ftp"}`, true, true, true, "", ""); err == nil {
		t.Fatal("bad source accepted")
	}
}

func TestAMCallAnswers(t *testing.T) {
	for _, tc := range []struct {
		method string
		status int
		body   string
		want   string
	}{
		{http.MethodGet, http.StatusNotFound, "404 page not found", "no Alertmanager v2 API here"},
		{http.MethodDelete, http.StatusNotFound, `"silence not found"`, "Alertmanager refused: silence not found"},
		{http.MethodPost, http.StatusBadRequest, "bad\n", "Alertmanager refused: bad"},
		{http.MethodPost, http.StatusInternalServerError, "", "Alertmanager refused: HTTP 500"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))

		src := promSource{Mode: promModeURL, URL: srv.URL}
		if _, err := amCall(kubeTarget{}, src, tc.method, "/api/v2/x", nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %d: %v, want %s", tc.method, tc.status, err, tc.want)
		}

		srv.Close()
	}
}

func TestAlertmanagerURLMode(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	var auth, tenant string

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, tenant = r.Header.Get("Authorization"), r.Header.Get("X-Scope-OrgID")

		switch r.URL.Path {
		case "/alertmanager/api/v2/silences":
			_, _ = io.WriteString(w, `{"silenceID":"u1"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	src, _ := json.Marshal(promSource{Mode: promModeURL, Kind: "alertmanager", URL: srv.URL + "/alertmanager", Auth: promAuthBearer, Secret: "tok", Tenant: "t1", CA: ca})

	id, err := AlertmanagerSilence("cfg", "ctx", "", string(src), `[{"name":"alertname","value":"X"}]`, 1, "c")
	if err != nil || id != "u1" || auth != "Bearer tok" || tenant != "t1" {
		t.Fatalf("id %q err %v auth %q tenant %q", id, err, auth, tenant)
	}

	if e := readAudit(t, "", "")[0]; e.Object != "" || !strings.HasPrefix(e.Params, "alertmanager="+srv.URL+"/alertmanager ") {
		t.Errorf("entry %+v", e)
	}
}

func TestAlertmanagerSilenceValidation(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	ok := `[{"name":"alertname","value":"X"}]`

	for name, tc := range map[string]struct {
		matchers string
		minutes  int
		comment  string
		want     string
	}{
		"no matchers":    {``, 60, "c", "at least one matcher"},
		"empty matchers": {`[]`, 60, "c", "at least one matcher"},
		"bad matchers":   {`[{"name":""}]`, 60, "c", "label name"},
		"match all":      {`[{"name":"alertname","value":".*","isRegex":true}]`, 60, "c", "mute every alert"},
		"too short":      {ok, 0, "c", "1 minute to 30 days"},
		"too long":       {ok, amMaxSilenceMinutes + 1, "c", "1 minute to 30 days"},
		"no comment":     {ok, 60, "   ", "needs a comment"},
		"long comment":   {ok, 60, strings.Repeat("x", amMaxComment+1), "longer than"},
	} {
		if _, err := AlertmanagerSilence("cfg", "ctx", "", amTestSource, tc.matchers, tc.minutes, tc.comment); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %s", name, err, tc.want)
		}
	}

	if _, err := AlertmanagerSilence("cfg", "ctx", "", `{"mode":"proxy"}`, ok, 60, "c"); err == nil {
		t.Error("bad source accepted")
	}

	for _, id := range []string{"", "../x", strings.Repeat("a", 65)} {
		if err := AlertmanagerExpire("cfg", "ctx", "", amTestSource, id); err == nil || !strings.Contains(err.Error(), "not a silence ID") {
			t.Errorf("expire %q: %v", id, err)
		}
	}

	if err := AlertmanagerExpire("cfg", "ctx", "", "{", "abc"); err == nil {
		t.Error("bad source accepted")
	}

	// Refused attempts are recorded too.
	for _, e := range readAudit(t, "", "") {
		if e.Outcome != auditFailed {
			t.Errorf("entry %+v", e)
		}
	}
}

func TestAlertmanagerDemo(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	for name, cfg := range map[string]string{"talos": mustDemoConfig(t), "kube": demoKubeconfigForTest(t)} {
		ctx := "Demo cluster"
		if name == "kube" {
			ctx = ""
		}

		out, err := AlertmanagerDiscover(cfg, ctx, "")
		if err != nil || !strings.Contains(out, "alertmanager-operated") {
			t.Fatalf("%s discover: %s %v", name, out, err)
		}

		out, err = AlertmanagerAlerts(cfg, ctx, "", amTestSource, true, true, true, "", "")
		if err != nil {
			t.Fatal(err)
		}

		var res amAlertsResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatal(err)
		}

		if want := (amCounts{Critical: 1, Warning: 2, Info: 1, Suppressed: 1}); res.Counts != want || res.Groups[0].Severity != amSeverityCritical {
			t.Fatalf("%s demo counts %+v", name, res.Counts)
		}

		filtered := map[string]struct {
			active, silenced bool
			receiver         string
			matchers         string
			want             int
		}{
			"unsilenced":  {true, false, "", "", 4},
			"silenced":    {false, true, "", "", 1},
			"receiver":    {true, true, "oncall-.*", "", 1},
			"matchers":    {true, true, "", `[{"name":"namespace","value":"demo"}]`, 2},
			"no matching": {true, true, "", `[{"name":"namespace","value":"nope"}]`, 0},
		}

		for fname, f := range filtered {
			out, err := AlertmanagerAlerts(cfg, ctx, "", amTestSource, f.active, f.silenced, true, f.receiver, f.matchers)
			if err != nil {
				t.Fatal(err)
			}

			var res amAlertsResult
			_ = json.Unmarshal([]byte(out), &res)

			if res.Total != f.want {
				t.Errorf("%s %s: %d alerts, want %d", name, fname, res.Total, f.want)
			}
		}

		out, err = AlertmanagerSilences(cfg, ctx, "", amTestSource, false)
		if err != nil || !strings.Contains(out, demoAMSilenceID) || strings.Contains(out, "expired") {
			t.Fatalf("%s silences: %s %v", name, out, err)
		}

		if out, _ = AlertmanagerSilences(cfg, ctx, "", amTestSource, true); !strings.Contains(out, `"state":"expired"`) {
			t.Fatalf("%s expired silences: %s", name, out)
		}

		if _, err := AlertmanagerSilence(cfg, ctx, "", amTestSource, `[{"name":"alertname","value":"Watchdog"}]`, 60, "c"); !errors.Is(err, errDemoUnavailable) {
			t.Errorf("%s silence: %v", name, err)
		}

		if err := AlertmanagerExpire(cfg, ctx, "", amTestSource, demoAMSilenceID); !errors.Is(err, errDemoUnavailable) {
			t.Errorf("%s expire: %v", name, err)
		}
	}

	for _, e := range readAudit(t, "", "") {
		if !e.Demo || e.Outcome != auditFailed {
			t.Errorf("demo entry %+v", e)
		}
	}
}

func mustDemoConfig(t *testing.T) string {
	t.Helper()

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	return cfg
}
