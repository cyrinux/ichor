package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func logsTestSource(t *testing.T, kind, url string) string {
	t.Helper()

	src, err := json.Marshal(promSource{Mode: promModeURL, Kind: kind, URL: url})
	if err != nil {
		t.Fatal(err)
	}

	return string(src)
}

func logsService(name string, labels map[string]string, port int, portName string) promService {
	var s promService

	s.Metadata.Name, s.Metadata.Namespace, s.Metadata.Labels = name, "logging", labels
	s.Spec.Ports = append(s.Spec.Ports, struct {
		Name     string `json:"name"`
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
	}{Name: portName, Port: port})

	return s
}

func TestLogsBackendDiscoverRanking(t *testing.T) {
	services := []promService{
		logsService("loki", nil, 3100, "http-metrics"),
		logsService("loki-read", nil, 3100, "http-metrics"),
		logsService("loki-gateway", nil, 80, "http-metrics"),
		logsService("query-frontend", map[string]string{"app.kubernetes.io/name": "loki"}, 3100, "http-metrics"),
		logsService("query-frontend", nil, 3100, "http-metrics"),
		logsService("loki-canary", nil, 3500, "http-metrics"),
		logsService("loki-index-gateway", nil, 3100, "http-metrics"),
		logsService("loki-headless", nil, 3100, "http-metrics"),
		logsService("promtail-loki", nil, 3101, "http-metrics"),
		logsService("vlselect-main", nil, 9471, "http"),
		logsService("vlstorage-main", nil, 9491, "http"),
		logsService("victoria-logs-single-server", nil, 9428, "http"),
		logsService("prometheus-operated", nil, 9090, "web"),
	}

	var got []string
	for _, c := range logsDiscoveryOf(rankSources(services, logsMatch)).Candidates {
		got = append(got, c.Kind+"/"+c.Source.Service)
	}

	want := []string{
		"loki/loki-gateway", "loki/loki-read", "victorialogs/vlselect-main", "loki/query-frontend", "victorialogs/victoria-logs-single-server", "loki/loki",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLogsBackendLokiQuery(t *testing.T) {
	var got *http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"streams","result":[
			{"stream":{"pod":"a"},"values":[["3000000000","third"],["1000000000","first"]]},
			{"stream":{"pod":"b"},"values":[["2000000000","second"]]}]}}`)
	}))
	t.Cleanup(srv.Close)

	out, err := LogsQuery("cfg", "ctx", "", logsTestSource(t, logsKindLoki, srv.URL), `{namespace="demo-shop"} |= "timeout"`, 1000, 5000, 3)
	if err != nil {
		t.Fatal(err)
	}

	var res logsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Stats != (logsStats{Returned: 3, Truncated: true}) || res.Lines[0].Text != "third" || res.Lines[1].Text != "second" || res.Lines[2].Text != "first" ||
		res.Lines[0].Time != 3000 || res.Lines[1].Labels["pod"] != "b" {
		t.Fatalf("result %+v", res)
	}

	q := got.URL.Query()
	if got.URL.Path != "/loki/api/v1/query_range" || q.Get("direction") != "backward" || q.Get("limit") != "3" ||
		q.Get("start") != "1000000000" || q.Get("end") != "5000000000" || q.Get("query") != `{namespace="demo-shop"} |= "timeout"` {
		t.Fatalf("request %s", got.URL)
	}
}

func TestLogsBackendLokiMetricQueryRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
	}))
	t.Cleanup(srv.Close)

	_, err := LogsQuery("cfg", "ctx", "", logsTestSource(t, logsKindLoki, srv.URL), `rate({a="b"}[1m])`, 1000, 5000, 0)
	if err == nil || !strings.Contains(err.Error(), "metric query") {
		t.Fatalf("err %v", err)
	}
}

func TestLogsBackendLokiErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadRequest, "parse error at line 1\n", "Loki refused: parse error"},
		{http.StatusNotFound, "404 page not found", "no Loki API here"},
		{http.StatusOK, "not json", "cannot be read"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))

		_, err := LogsQuery("cfg", "ctx", "", logsTestSource(t, logsKindLoki, srv.URL), `{a="b"}`, 1000, 5000, 0)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d: %v, want %s", tc.status, err, tc.want)
		}

		srv.Close()
	}
}

func TestLogsBackendVictoriaLogsQuery(t *testing.T) {
	var got *http.Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, `{"_msg":"older","_time":"2026-01-01T10:00:00Z","_stream":"{}","_stream_id":"1","pod":"a"}
{"_msg":"newer","_time":"2026-01-01T11:00:00.5Z","_stream":"{}","_stream_id":"1","pod":"b"}
`)
	}))
	t.Cleanup(srv.Close)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

	out, err := LogsQuery("cfg", "ctx", "", logsTestSource(t, logsKindVictoriaLogs, srv.URL), `namespace:"demo-shop" AND timeout`, start, start+3600_000*24, 10)
	if err != nil {
		t.Fatal(err)
	}

	var res logsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Stats != (logsStats{Returned: 2}) || res.Lines[0].Text != "newer" || res.Lines[1].Labels["pod"] != "a" {
		t.Fatalf("result %+v", res)
	}

	if _, own := res.Lines[0].Labels["_stream"]; own {
		t.Fatalf("own field in labels %+v", res.Lines[0].Labels)
	}

	// Without a time filter VictoriaLogs scans everything: start and end always go out.
	q := got.URL.Query()
	if got.URL.Path != "/select/logsql/query" || q.Get("start") != "2026-01-01T00:00:00Z" || q.Get("end") != "2026-01-02T00:00:00Z" || q.Get("limit") != "10" {
		t.Fatalf("request %s", got.URL)
	}
}

func TestLogsBackendLabels(t *testing.T) {
	var paths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)

		switch r.URL.Path {
		case "/loki/api/v1/labels":
			_, _ = io.WriteString(w, `{"status":"success","data":["pod","namespace"]}`)
		case "/loki/api/v1/label/namespace/values":
			_, _ = io.WriteString(w, `{"status":"success","data":["demo-shop"]}`)
		case "/select/logsql/field_names":
			if r.URL.Query().Get("start") == "" {
				t.Error("no start on field_names")
			}

			_, _ = io.WriteString(w, `{"values":[{"value":"_msg","hits":3},{"value":"pod","hits":3},{"value":"namespace","hits":3}]}`)
		case "/select/logsql/field_values":
			if r.URL.Query().Get("field") != "namespace" {
				t.Errorf("field %q", r.URL.Query().Get("field"))
			}

			_, _ = io.WriteString(w, `{"values":[{"value":"demo-shop","hits":3}]}`)
		}
	}))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		kind, label, want string
	}{
		{logsKindLoki, "", `{"values":["namespace","pod"]}`},
		{logsKindLoki, "namespace", `{"values":["demo-shop"]}`},
		{logsKindVictoriaLogs, "", `{"values":["namespace","pod"]}`},
		{logsKindVictoriaLogs, "namespace", `{"values":["demo-shop"]}`},
	} {
		out, err := LogsLabels("cfg", "ctx", "", logsTestSource(t, tc.kind, srv.URL), tc.label)
		if err != nil || out != tc.want {
			t.Errorf("%s %q: %s %v, want %s", tc.kind, tc.label, out, err, tc.want)
		}
	}

	if _, err := LogsLabels("cfg", "ctx", "", logsTestSource(t, logsKindLoki, srv.URL), "a\nb"); err == nil {
		t.Error("bad label accepted")
	}
}

func TestLogsBackendProbeFindsTheKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/select/logsql/field_names" {
			_, _ = io.WriteString(w, `{"values":[]}`)

			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	out, err := LogsProbe("cfg", "ctx", "", logsTestSource(t, "", srv.URL))
	if err != nil || !strings.Contains(out, `"kind":"victorialogs"`) {
		t.Fatalf("probe: %s %v", out, err)
	}

	// A known kind is only asked as itself.
	if _, err = LogsProbe("cfg", "ctx", "", logsTestSource(t, logsKindLoki, srv.URL)); err == nil || !strings.Contains(err.Error(), "no Loki API here") {
		t.Fatalf("err %v", err)
	}
}

func TestLogsBackendProbeKeepsNoSecret(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"success","data":[]}`)
	}))
	t.Cleanup(srv.Close)

	src, _ := json.Marshal(promSource{Mode: promModeURL, URL: srv.URL, Auth: promAuthBearer, Secret: "tok", InsecureSkipVerify: true})

	out, err := LogsProbe("cfg", "ctx", "", string(src))
	if err != nil || strings.Contains(out, "tok") || !strings.Contains(out, `"kind":"loki"`) {
		t.Fatalf("probe: %s %v", out, err)
	}
}

func TestLogsBackendQueryValidation(t *testing.T) {
	src := logsTestSource(t, logsKindLoki, "http://logs.invalid")

	for name, tc := range map[string]struct {
		query      string
		start, end int64
		want       string
	}{
		"empty":     {"  ", 1, 2, "empty"},
		"long":      {strings.Repeat("x", promMaxQuery+1), 1, 2, "longer"},
		"backwards": {"q", 5, 2, "ends before"},
		"no start":  {"q", 0, 2, "ends before"},
		"too wide":  {"q", 1, promMaxRange.Milliseconds() + 2, "31 days"},
	} {
		if _, err := LogsQuery("cfg", "ctx", "", src, tc.query, tc.start, tc.end, 0); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %s", name, err, tc.want)
		}
	}
}

func TestLogsBackendPodQuery(t *testing.T) {
	if got := LogsPodQuery(logsKindLoki, "ns", "pod-1"); got != `{namespace="ns",pod="pod-1"}` {
		t.Errorf("loki: %s", got)
	}

	if got := LogsPodQuery(logsKindVictoriaLogs, "ns", "pod-1"); got != `namespace:"ns" AND pod:"pod-1"` {
		t.Errorf("victorialogs: %s", got)
	}
}

func TestLogsBackendWorkloadQuery(t *testing.T) {
	if got := LogsWorkloadQuery(logsKindLoki, "ns", "api"); got != `{namespace="ns",pod=~"api-.*"}` {
		t.Errorf("loki: %s", got)
	}

	if got := LogsWorkloadQuery(logsKindVictoriaLogs, "ns", "api"); got != `namespace:"ns" AND pod:~"api-.*"` {
		t.Errorf("victorialogs: %s", got)
	}
}

func TestLogsBackendDemo(t *testing.T) {
	cfg := mustDemoConfig(t)
	ctx := "Demo cluster"

	out, err := LogsDiscover(cfg, ctx, "")
	if err != nil || !strings.Contains(out, `"kind":"loki"`) || !strings.Contains(out, "loki-gateway") {
		t.Fatalf("discover: %s %v", out, err)
	}

	src := `{"mode":"proxy","kind":"loki","namespace":"monitoring","service":"loki-gateway","port":80}`

	if out, err = LogsLabels(cfg, ctx, "", src, "namespace"); err != nil || !strings.Contains(out, "demo-shop") {
		t.Fatalf("labels: %s %v", out, err)
	}

	if out, err = LogsProbe(cfg, ctx, "", src); err != nil || !strings.Contains(out, `"kind":"loki"`) {
		t.Fatalf("probe: %s %v", out, err)
	}

	out, err = LogsQuery(cfg, ctx, "", src, `{namespace="demo-shop",pod="p1"} |= "timeout"`, 1000, 5000, 0)
	if err != nil {
		t.Fatal(err)
	}

	var res logsResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Stats.Returned != 2 || res.Lines[0].Time < res.Lines[1].Time || res.Lines[0].Labels["pod"] != "p1" {
		t.Fatalf("demo %+v", res)
	}
}
