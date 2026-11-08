package ichorgo

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

const onCallTestToken = "sample-oncall-token-value"

const onCallTestDetails = "Labels:\n - alertname = DiskFull\n - instance = node-a\n - severity = critical\nAnnotations:\n - summary = full\nSource: http://x\n" +
	"Labels:\n - alertname = DiskFull\n - instance = node-b\n - severity = critical\nAnnotations:\n"

func onCallTestConfig(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	saved := pagerDutyBases
	pagerDutyBases = map[string]string{"us": srv.URL, "eu": srv.URL}

	t.Cleanup(func() { pagerDutyBases = saved })

	cfg, _ := json.Marshal(onCallConfig{Kind: onCallKindPagerDuty, Token: onCallTestToken, From: "someone@example.com", ServiceIDs: []string{"P123", "P456"}}) //nolint:errcheck

	return string(cfg)
}

func TestOnCallIncidentsListsAndMatches(t *testing.T) {
	var query string

	cfg := onCallTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token token="+onCallTestToken || !strings.Contains(r.Header.Get("Accept"), "version=2") {
			t.Errorf("headers: %v", r.Header)
		}

		switch r.URL.Path {
		case "/incidents":
			query = r.URL.RawQuery
			_, _ = io.WriteString(w, `{"incidents":[
			 {"id":"PA1","incident_number":7,"title":"[FIRING:2] DiskFull","status":"triggered","urgency":"high","created_at":"2026-01-02T03:04:05Z",
			  "html_url":"https://sample-co.pagerduty.com/incidents/PA1","service":{"summary":"Platform"},"assignments":[{"assignee":{"summary":"Sam"}}]},
			 {"id":"PA2","incident_number":8,"title":"OtherThing is off","status":"acknowledged","urgency":"low","created_at":"bad",
			  "acknowledgements":[{"acknowledger":{"summary":"Ann"}},{"acknowledger":{"summary":"Bob"}}]}]}`)
		case "/incidents/PA1/alerts":
			body, _ := json.Marshal(map[string]any{"alerts": []any{map[string]any{"body": map[string]any{"details": map[string]any{"firing": onCallTestDetails, "num_firing": "2"}}}}}) //nolint:errcheck
			_, _ = w.Write(body)
		case "/incidents/PA2/alerts":
			http.Error(w, "nope", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})

	alerts := `[{"fingerprint":"fa","labels":{"alertname":"DiskFull","instance":"node-a","severity":"critical"}},
	 {"fingerprint":"fb","labels":{"alertname":"DiskFull","instance":"node-b","severity":"critical"}},
	 {"fingerprint":"fc","labels":{"alertname":"DiskFull","instance":"node-c"}},
	 {"fingerprint":"fo","labels":{"alertname":"OtherThing"}},
	 {"fingerprint":"fn","labels":{"alertname":"Other"}}]`

	out, err := OnCallIncidents("", "ctx", cfg, alerts)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, onCallTestToken) {
		t.Fatal("the token is in the answer")
	}

	for _, want := range []string{"statuses%5B%5D=triggered", "statuses%5B%5D=acknowledged", "service_ids%5B%5D=P123", "service_ids%5B%5D=P456", "limit=100"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %s lacks %s", query, want)
		}
	}

	var res onCallResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	want := map[string]onCallMatch{
		"fa": {"PA1", "triggered", "exact"},
		"fb": {"PA1", "triggered", "exact"},
		"fc": {"PA1", "triggered", "alertname"},
		"fo": {"PA2", "acknowledged", "alertname"},
	}
	if len(res.Matches) != len(want) {
		t.Fatalf("matches %+v", res.Matches)
	}

	for fp, m := range want {
		if res.Matches[fp] != m {
			t.Errorf("%s: %+v, want %+v", fp, res.Matches[fp], m)
		}
	}

	first, second := res.Incidents[0], res.Incidents[1]
	if !slices.Equal(first.MatchedFingerprints, []string{"fa", "fb", "fc"}) || first.Number != 7 || first.Service != "Platform" ||
		first.URL == "" || first.CreatedAt == 0 || !slices.Equal(first.Assignees, []string{"Sam"}) {
		t.Errorf("first %+v", first)
	}

	if second.AcknowledgedBy != "Bob" || second.CreatedAt != 0 {
		t.Errorf("second %+v", second)
	}
}

func TestMatchOnCallPrefersExactThenTriggered(t *testing.T) {
	exact := onCallIncident{ID: "E", Status: onCallStatusAcknowledged, Details: []string{"Labels:\n - alertname = A\n - pod = p\nAnnotations:\n"}}
	named := onCallIncident{ID: "N", Title: "A is firing", Status: onCallStatusTriggered}
	older := onCallIncident{ID: "O", Title: "A again", Status: onCallStatusAcknowledged}
	alerts := []onCallAmAlert{{Fingerprint: "f", Labels: map[string]string{"alertname": "A", "pod": "p"}}}

	for _, tc := range []struct {
		name      string
		incidents []onCallIncident
		want      onCallMatch
	}{
		{"exact wins", []onCallIncident{named, exact}, onCallMatch{"E", onCallStatusAcknowledged, onCallConfidenceExact}},
		{"triggered wins", []onCallIncident{older, named}, onCallMatch{"N", onCallStatusTriggered, onCallConfidenceAlertname}},
		{"first of equals", []onCallIncident{older, {ID: "O2", Title: "A", Status: onCallStatusAcknowledged}}, onCallMatch{"O", onCallStatusAcknowledged, onCallConfidenceAlertname}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchOnCall(tc.incidents, alerts).Matches["f"]; got != tc.want {
				t.Fatalf("%+v, want %+v", got, tc.want)
			}
		})
	}

	// A longer alertname does not match by its prefix, a metric-style one by its part.
	res := matchOnCall([]onCallIncident{{ID: "X", Title: "ABC fired, also A:b"}}, alerts)
	if len(res.Matches) != 0 {
		t.Fatalf("%+v", res.Matches)
	}
}

func TestOnCallResolvedDetailsDoNotMatch(t *testing.T) {
	texts := pagerDutyDetailTexts(json.RawMessage(`{"firing":"f","resolved":"r","num_firing":"1","other":2}`))
	if !slices.Equal(texts, []string{"f"}) {
		t.Fatalf("%v", texts)
	}

	if texts := pagerDutyDetailTexts(json.RawMessage(`"plain"`)); !slices.Equal(texts, []string{"plain"}) {
		t.Fatalf("%v", texts)
	}

	if texts := pagerDutyDetailTexts(json.RawMessage(`[1]`)); texts != nil {
		t.Fatalf("%v", texts)
	}
}

func TestOnCallActRequest(t *testing.T) {
	for _, tc := range []struct{ verb, status string }{{onCallVerbAcknowledge, "acknowledged"}, {onCallVerbResolve, "resolved"}} {
		t.Run(tc.verb, func(t *testing.T) {
			cfg := onCallTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body) //nolint:errcheck
				if r.Method != http.MethodPut || r.URL.Path != "/incidents/PA1" || r.Header.Get("From") != "someone@example.com" ||
					!strings.Contains(string(body), `"status":"`+tc.status+`"`) || !strings.Contains(string(body), `"type":"incident_reference"`) {
					t.Errorf("%s %s %v %s", r.Method, r.URL.Path, r.Header, body)
				}

				_, _ = io.WriteString(w, `{"incident":{"id":"PA1","incident_number":7}}`)
			})

			if err := OnCallAct("", "ctx", cfg, "PA1", tc.verb); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOnCallActIsAudited(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	cfg := onCallTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/PA9") {
			w.WriteHeader(http.StatusForbidden)

			return
		}

		_, _ = io.WriteString(w, `{"incident":{"id":"PA1","incident_number":7}}`)
	})

	if err := OnCallAct("", "ctx", cfg, "PA1", onCallVerbAcknowledge); err != nil {
		t.Fatal(err)
	}

	if err := OnCallAct("", "ctx", cfg, "PA9", onCallVerbResolve); err == nil {
		t.Fatal("the refusal was lost")
	}

	entries := readAudit(t, "", "")
	if len(entries) != 2 {
		t.Fatalf("%+v", entries)
	}

	if e := entries[1]; e.Action != "oncall.acknowledge" || e.Params != "incident #7" || e.Outcome != auditOK {
		t.Errorf("acknowledge %+v", e)
	}

	if e := entries[0]; e.Action != "oncall.resolve" || e.Params != "incident PA9" || e.Outcome != auditFailed || !strings.Contains(e.Error, "cannot change incidents") {
		t.Errorf("resolve %+v", e)
	}

	for _, e := range entries {
		if strings.Contains(e.Params+e.Error, onCallTestToken) {
			t.Fatalf("the token is in the action log: %+v", e)
		}
	}
}

func TestOnCallActRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		want string
	}{
		{"token", http.StatusUnauthorized, `{"error":{"message":"Unauthorized"}}`, "the PagerDuty token was refused"},
		{"scope", http.StatusForbidden, ``, "this token cannot change incidents: it needs the incidents write scope"},
		{"from", http.StatusBadRequest, `{"error":{"message":"Requester User Not Found"}}`, "add your PagerDuty email"},
		{"gone", http.StatusNotFound, ``, "no longer knows"},
		{"limit", http.StatusTooManyRequests, ``, "limiting"},
		{"other", http.StatusConflict, `{"error":{"message":"Conflict","errors":["busy"]}}`, "PagerDuty refused (409): Conflict busy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := onCallTestConfig(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			})

			err := OnCallAct("", "ctx", cfg, "PA1", onCallVerbAcknowledge)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), onCallTestToken) {
				t.Fatalf("%v, want %s", err, tc.want)
			}
		})
	}
}

func TestOnCallActValidates(t *testing.T) {
	const cfg = `{"kind":"pagerduty","token":"x"}`

	for _, tc := range []struct{ id, verb string }{{"PA1", "snooze"}, {"../x", onCallVerbResolve}, {"", onCallVerbResolve}} {
		if err := OnCallAct("", "ctx", cfg, tc.id, tc.verb); err == nil {
			t.Errorf("%s %s went through", tc.id, tc.verb)
		}
	}
}

func TestOnCallTestIdentity(t *testing.T) {
	t.Run("user token", func(t *testing.T) {
		cfg := onCallTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/users/me" {
				t.Errorf("path %s", r.URL.Path)
			}

			_, _ = io.WriteString(w, `{"user":{"name":"Sam Sample","html_url":"https://sample-co.pagerduty.com/users/P1"}}`)
		})

		out, err := OnCallTest(cfg)
		if err != nil || out != `{"user":"Sam Sample","account":"sample-co"}` {
			t.Fatalf("%s %v", out, err)
		}
	})

	t.Run("account token", func(t *testing.T) {
		cfg := onCallTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/users/me" {
				w.WriteHeader(http.StatusBadRequest)

				return
			}

			_, _ = io.WriteString(w, `{"abilities":["sso"]}`)
		})

		out, err := OnCallTest(cfg)
		if err != nil || out != `{"user":"","account":""}` {
			t.Fatalf("%s %v", out, err)
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		cfg := onCallTestConfig(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })

		if _, err := OnCallTest(cfg); err == nil || err.Error() != "the PagerDuty token was refused" {
			t.Fatalf("%v", err)
		}
	})
}

func TestParseOnCallConfig(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"not json, not quoted", `{"token":"` + onCallTestToken, "not valid"},
		{"kind", `{"kind":"other","token":"x"}`, "not supported"},
		{"no token", `{"kind":"pagerduty"}`, "add the PagerDuty token"},
		{"line in token", `{"kind":"pagerduty","token":"a\nb"}`, "token is not valid"},
		{"region", `{"kind":"pagerduty","token":"x","region":"ap"}`, "region"},
		{"service", `{"kind":"pagerduty","token":"x","serviceIds":["../a"]}`, "service ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseOnCallConfig(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), onCallTestToken) {
				t.Fatalf("%v, want %s", err, tc.want)
			}
		})
	}

	c, err := parseOnCallConfig(`{"kind":"pagerduty","token":" x ","region":"EU"}`)
	if err != nil || c.Token != "x" || c.Region != "eu" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestOnCallIncidentsNeedsAService(t *testing.T) {
	cfg := onCallTestConfig(t, func(http.ResponseWriter, *http.Request) { t.Error("no request expected") })

	var c onCallConfig
	_ = json.Unmarshal([]byte(cfg), &c) //nolint:errcheck
	c.ServiceIDs = nil
	b, _ := json.Marshal(c) //nolint:errcheck

	if _, err := OnCallIncidents("", "ctx", string(b), ""); err == nil || !strings.Contains(err.Error(), "service ID") {
		t.Fatalf("%v", err)
	}
}

func TestOnCallDemo(t *testing.T) {
	var inputs []onCallAmAlert
	for _, a := range demoAMAlerts(time.Now()) {
		inputs = append(inputs, onCallAmAlert{Fingerprint: a.Fingerprint, Labels: a.Labels})
	}

	res := matchOnCall(demoOnCall(time.Now()), inputs)
	if len(res.Incidents) != 2 {
		t.Fatalf("%+v", res.Incidents)
	}

	if m := res.Matches["a1c3e5f7a9b1c3d5"]; m.Confidence != onCallConfidenceExact || m.Status != onCallStatusTriggered {
		t.Errorf("the filesystem page: %+v", m)
	}

	if m := res.Matches["b2d4f6a8c0e2f4a6"]; m.IncidentID != "PDEMO02" || m.Status != onCallStatusAcknowledged {
		t.Errorf("the crash loop: %+v", m)
	}

	withDataDir(t)
	withAuditClock(t, time.Now())

	for name, cfg := range map[string]string{"talos": mustDemoConfig(t), "kube": demoKubeconfigForTest(t)} {
		ctx := "Demo cluster"
		if name == "kube" {
			ctx = ""
		}

		out, err := OnCallIncidents(cfg, ctx, "", "")
		if err != nil || !strings.Contains(out, "PDEMO01") {
			t.Fatalf("%s: %s %v", name, out, err)
		}

		if err := OnCallAct(cfg, ctx, "", "PDEMO01", onCallVerbAcknowledge); !errors.Is(err, errDemoUnavailable) {
			t.Fatalf("%s: the demo acted: %v", name, err)
		}
	}

	for _, e := range readAudit(t, "", "") {
		if !e.Demo || e.Outcome != auditFailed {
			t.Errorf("demo entry %+v", e)
		}
	}
}
