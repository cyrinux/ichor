package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

const grafanaTestPrefix = "/api/plugins/grafana-irm-app/resources/api/v1"

// grafanaTestConfig serves handler over TLS as a Grafana IRM stack and returns the settings.
func grafanaTestConfig(t *testing.T, handler http.HandlerFunc) (string, *httptest.Server) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	saved := grafanaHTTPClient
	grafanaHTTPClient = srv.Client
	t.Cleanup(func() { grafanaHTTPClient = saved })

	cfg, _ := json.Marshal(onCallConfig{Kind: onCallKindGrafana, Token: onCallTestToken, BaseURL: srv.URL + grafanaTestPrefix + "/"}) //nolint:errcheck

	return string(cfg), srv
}

func grafanaTestGroup(id, state, title, created string, alerts ...map[string]string) map[string]any {
	payloadAlerts := make([]any, 0, len(alerts))
	for _, labels := range alerts {
		payloadAlerts = append(payloadAlerts, map[string]any{"status": "firing", "labels": labels})
	}

	return map[string]any{
		"id": id, "state": state, "title": title, "created_at": created,
		"permalinks": map[string]any{"web": "https://sample.grafana.net/a/grafana-irm-app/alert-groups/" + id},
		"last_alert": map[string]any{"payload": map[string]any{"alerts": payloadAlerts}},
	}
}

func TestOnCallGrafanaListsPagesAndMatches(t *testing.T) {
	var states []string

	cfg, srv := grafanaTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != onCallTestToken {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}

		if r.URL.Path != grafanaTestPrefix+"/alert_groups/" {
			t.Errorf("unexpected %s", r.URL.Path)

			return
		}

		state := r.URL.Query().Get("state")
		states = append(states, state+"/"+r.URL.Query().Get("page"))

		var page map[string]any

		switch {
		case state == "new" && r.URL.Query().Get("page") == "":
			page = map[string]any{
				"results": []any{grafanaTestGroup("IG1", "new", "[FIRING:2] DiskFull", "2026-01-02T03:04:05.123456Z",
					map[string]string{"alertname": "DiskFull", "instance": "node-a", "severity": "critical"},
					map[string]string{"alertname": "DiskFull", "instance": "node-b", "severity": "critical"})},
				"next": "https://" + r.Host + grafanaTestPrefix + "/alert_groups/?state=new&page=2",
			}
		case state == "new":
			page = map[string]any{"results": []any{}, "next": nil}
		case state == "acknowledged":
			page = map[string]any{"results": []any{grafanaTestGroup("IG2", "acknowledged", "OtherThing is off", "bad")}, "next": nil}
		default:
			t.Errorf("state %q", state)
		}

		body, _ := json.Marshal(page) //nolint:errcheck
		_, _ = w.Write(body)
	})
	_ = srv

	alerts := `[{"fingerprint":"fa","labels":{"alertname":"DiskFull","instance":"node-a","severity":"critical"}},
	 {"fingerprint":"fb","labels":{"alertname":"DiskFull","instance":"node-b","severity":"critical"}},
	 {"fingerprint":"fc","labels":{"alertname":"DiskFull","instance":"node-c"}},
	 {"fingerprint":"fo","labels":{"alertname":"OtherThing"}}]`

	out, err := OnCallIncidents("", "ctx", cfg, alerts)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, onCallTestToken) {
		t.Fatal("the token is in the answer")
	}

	if !slices.Equal(states, []string{"new/", "new/2", "acknowledged/"}) {
		t.Errorf("pages read: %v", states)
	}

	var res onCallResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	want := map[string]onCallMatch{
		"fa": {"IG1", onCallStatusTriggered, "exact"},
		"fb": {"IG1", onCallStatusTriggered, "exact"},
		"fc": {"IG1", onCallStatusTriggered, "alertname"},
		"fo": {"IG2", onCallStatusAcknowledged, "alertname"},
	}
	if len(res.Matches) != len(want) {
		t.Fatalf("matches %+v", res.Matches)
	}

	for fp, m := range want {
		if res.Matches[fp] != m {
			t.Errorf("%s: %+v, want %+v", fp, res.Matches[fp], m)
		}
	}

	if first := res.Incidents[0]; first.CreatedAt == 0 || first.URL == "" || first.Number != 0 {
		t.Errorf("first %+v", first)
	}
}

// A next link to another host is not followed: the token would go with it.
func TestOnCallGrafanaRefusesAForeignNextLink(t *testing.T) {
	calls := 0

	cfg, _ := grafanaTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		calls++

		page := map[string]any{"results": []any{}, "next": nil}
		if r.URL.Query().Get("state") == "new" {
			page["next"] = "https://elsewhere.invalid/steal?page=2"
		}

		body, _ := json.Marshal(page) //nolint:errcheck
		_, _ = w.Write(body)
	})

	if _, err := OnCallIncidents("", "ctx", cfg, ""); err == nil || !strings.Contains(err.Error(), "next page") {
		t.Errorf("err %v", err)
	}

	if calls != 1 {
		t.Errorf("%d calls", calls)
	}
}

func TestOnCallGrafanaActs(t *testing.T) {
	var got []string

	cfg, _ := grafanaTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+strings.TrimPrefix(r.URL.Path, grafanaTestPrefix))
	})

	for _, verb := range []string{onCallVerbAcknowledge, onCallVerbResolve} {
		if err := OnCallAct("", "ctx", cfg, "IG1", verb); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
	}

	if !slices.Equal(got, []string{"POST /alert_groups/IG1/acknowledge", "POST /alert_groups/IG1/resolve"}) {
		t.Errorf("requests %v", got)
	}
}

func TestOnCallGrafanaRefusals(t *testing.T) {
	for _, c := range []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "token was refused"},
		{http.StatusForbidden, "permission"},
		{http.StatusNotFound, "no longer exists"},
		{http.StatusTooManyRequests, "too many requests"},
	} {
		cfg, _ := grafanaTestConfig(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"detail":"nope"}`, c.status)
		})

		if err := OnCallAct("", "ctx", cfg, "IG1", onCallVerbResolve); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%d: %v", c.status, err)
		}
	}
}

func TestOnCallGrafanaTest(t *testing.T) {
	cfg, _ := grafanaTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != grafanaTestPrefix+"/users/current/" {
			t.Errorf("path %s", r.URL.Path)
		}

		_, _ = io.WriteString(w, `{"id":"U1","username":"sam","email":"sam@example.com"}`)
	})

	out, err := OnCallTest(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var who onCallIdentity
	if err := json.Unmarshal([]byte(out), &who); err != nil || who.User != "sam" || who.Account != "127" {
		t.Errorf("identity %s (%v)", out, err)
	}
}

func TestOnCallGrafanaSettings(t *testing.T) {
	for _, c := range []struct{ config, want string }{
		{`{"kind":"grafana","token":"t"}`, "base URL"},
		{`{"kind":"grafana","token":"t","baseURL":"http://sample.grafana.net/api"}`, "https"},
		{`{"kind":"grafana","token":"t","baseURL":"https://sample.grafana.net/api?x=1"}`, "base URL"},
		{`{"kind":"grafana","baseURL":"https://sample.grafana.net/api"}`, "Grafana IRM token"},
		{`{"kind":"opsgenie","token":"t"}`, "not supported"},
	} {
		if _, err := parseOnCallConfig(c.config); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.config, err)
		}
	}

	cfg, err := parseOnCallConfig(`{"kind":"grafana","token":" t ","baseURL":" https://sample.grafana.net/oncall/api/v1/ "}`)
	if err != nil || cfg.BaseURL != "https://sample.grafana.net/oncall/api/v1" || cfg.Token != "t" {
		t.Errorf("%+v, %v", cfg, err)
	}
}
