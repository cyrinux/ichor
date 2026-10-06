package ichorgo

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromSourceNormalize(t *testing.T) {
	for _, tc := range []struct {
		name, in, want, err string
	}{
		{"proxy", `{"mode":"proxy","namespace":" monitoring ","service":"prometheus-operated","port":9090,"pathPrefix":"/prometheus/","tenant":"team-a"}`,
			`{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090,"pathPrefix":"/prometheus","tenant":"team-a"}`, ""},
		{"proxy bad name", `{"mode":"proxy","namespace":"Mon","service":"p","port":9090}`, "", "namespace"},
		{"proxy bad port", `{"mode":"proxy","namespace":"m","service":"p","port":0}`, "", "port"},
		{"proxy traversal", `{"mode":"proxy","namespace":"m","service":"p","port":1,"pathPrefix":"/../x"}`, "", "path prefix"},
		{"proxy query in prefix", `{"mode":"proxy","namespace":"m","service":"p","port":1,"pathPrefix":"/x?y=1"}`, "", "path prefix"},
		{"proxy credentials", `{"mode":"proxy","namespace":"m","service":"p","port":1,"auth":"bearer","secret":"s"}`, "", "credentials"},
		{"url", `{"mode":"url","url":"https://mimir.example.com/prometheus/","auth":"basic","username":"u","secret":"p","tenant":"a|b"}`,
			`{"mode":"url","url":"https://mimir.example.com/prometheus","auth":"basic","username":"u","secret":"p","tenant":"a|b"}`, ""},
		{"url plain no auth", `{"mode":"url","url":"http://10.0.0.5:9090","insecureSkipVerify":true}`, `{"mode":"url","url":"http://10.0.0.5:9090"}`, ""},
		{"url http credentials", `{"mode":"url","url":"http://p.example","auth":"bearer","secret":"t"}`, "", "https"},
		{"url userinfo", `{"mode":"url","url":"https://u:p@p.example"}`, "", "authentication fields"},
		{"url scheme", `{"mode":"url","url":"ftp://p.example"}`, "", "https://host"},
		{"url query", `{"mode":"url","url":"https://p.example/?a=1"}`, "", "query"},
		{"bearer without token", `{"mode":"url","url":"https://p.example","auth":"bearer"}`, "", "token"},
		{"secret newline", `{"mode":"url","url":"https://p.example","auth":"bearer","secret":"a\nb"}`, "", "lines"},
		{"secret trimmed, kind checked", `{"mode":"url","kind":"<script>","url":"https://p.example","auth":"bearer","secret":" tok\n"}`,
			`{"mode":"url","url":"https://p.example","auth":"bearer","secret":"tok"}`, ""},
		{"bad CA", `{"mode":"url","url":"https://p.example","ca":"nope"}`, "", "PEM"},
		{"tenant header injection", `{"mode":"url","url":"https://p.example","tenant":"a\r\nX: y"}`, "", "tenant"},
		{"mode", `{"mode":"ssh"}`, "", "mode"},
		{"no mode is the proxy", `{"namespace":"m","service":"p","port":1}`, `{"mode":"proxy","namespace":"m","service":"p","port":1}`, ""},
		{"json", `nope`, "", "bad metrics source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, err := parsePromSource(tc.in)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("got %v, want an error with %q", err, tc.err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got, _ := toJSON(src); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestNormalizePromSourceDropsSecret(t *testing.T) {
	out, err := NormalizePromSource(`{"mode":"url","url":"https://p.example","auth":"bearer","secret":"token"}`)
	if err != nil || strings.Contains(out, "token\"") || !strings.Contains(out, `"auth":"bearer"`) {
		t.Fatalf("got %s %v", out, err)
	}
}

func TestPromRangeGrid(t *testing.T) {
	g, err := promRangeGrid(1000, 4600, 0)
	if err != nil || g.step != 15 || g.start != 990 || g.end() != 4605 || g.n != 242 {
		t.Fatalf("auto step: %+v %v", g, err)
	}

	if g, _ := promRangeGrid(1, 86401, 0); g.step != 346 || g.n > promTargetPoints+2 {
		t.Fatalf("day: %+v", g)
	}

	// Inside one step, the range still makes two points.
	if g, _ := promRangeGrid(95, 100, 15); g.n != 2 || g.start != 90 {
		t.Fatalf("short: %+v", g)
	}

	for _, tc := range []struct{ start, end, step int64 }{{10, 10, 0}, {20, 10, 0}, {1, 32 * 86400, 0}, {1, 86400, 1}, {-10, 10, 0}, {1, 1 << 62, 0}} {
		if _, err := promRangeGrid(tc.start, tc.end, tc.step); err == nil {
			t.Errorf("%+v accepted", tc)
		}
	}
}

func TestParsePromAnswer(t *testing.T) {
	grid := promGrid{start: 100, step: 10, n: 4}

	matrix := `{"status":"success","warnings":["w1"],"data":{"resultType":"matrix","result":[
		{"metric":{"__name__":"up","job":"node","instance":"a"},"values":[[100,"1"],[120,"NaN"],[130.0004,"2.5"],[999,"7"]]},
		{"metric":{},"values":[[110,"+Inf"]]}]}}`

	res, err := parsePromAnswer(200, []byte(matrix), grid)
	if err != nil {
		t.Fatal(err)
	}

	if fmt.Sprint(res.Times) != "[100000 110000 120000 130000]" || res.Total != 2 || res.Truncated || len(res.Warnings) != 1 {
		t.Fatalf("got %+v", res)
	}

	if s := res.Series[0]; s.Name != `up{instance="a",job="node"}` || *s.Values[0] != 1 || s.Values[1] != nil || s.Values[2] != nil || *s.Values[3] != 2.5 {
		t.Fatalf("series 0: %+v", s)
	}

	if s := res.Series[1]; s.Name != "{}" || s.Values[1] != nil {
		t.Fatalf("series 1: %+v", s)
	}

	vector := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"ns":"a"},"value":[500,"3"]}]}}`
	if res, err := parsePromAnswer(200, []byte(vector), promGrid{start: 500, step: 1, n: 1}); err != nil || *res.Series[0].Values[0] != 3 || res.Times[0] != 500000 {
		t.Fatalf("vector: %+v %v", res, err)
	}

	scalar := `{"status":"success","data":{"resultType":"scalar","result":[500,"4"]}}`
	if res, err := parsePromAnswer(200, []byte(scalar), promGrid{start: 500, step: 1, n: 1}); err != nil || *res.Series[0].Values[0] != 4 {
		t.Fatalf("scalar: %+v %v", res, err)
	}

	for body, want := range map[string]string{
		`{"status":"error","errorType":"bad_data","error":"parse error at char 4"}`: "bad_data: parse error",
		`{"status":"success","data":{"resultType":"string","result":[1,"x"]}}`:      "cannot be charted",
		`404 page not found`: "path prefix",
		`<html>`:             "not from a Prometheus",
		`no org id`:          promRefused,
	} {
		status := 200

		switch body {
		case `404 page not found`:
			status = 404
		case `no org id`:
			status = 401
		case `{"status":"error","errorType":"bad_data","error":"parse error at char 4"}`:
			status = 400
		}

		if _, err := parsePromAnswer(status, []byte(body), grid); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", body, err, want)
		}
	}
}

func TestParsePromAnswerSkipsHistograms(t *testing.T) {
	body := `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"a":"1"},"histogram":[1,{"count":"2","sum":"3"}]},
		{"metric":{"a":"2"},"value":[1,"5"]},
		{"metric":{"a":"3"},"value":["bad",5]}]}}`

	res, err := parsePromAnswer(200, []byte(body), promGrid{start: 1, step: 1, n: 1})
	if err != nil || len(res.Series) != 2 || *res.Series[0].Values[0] != 5 || res.Series[1].Values[0] != nil || len(res.Warnings) != 1 {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestParsePromAnswerCapsSeries(t *testing.T) {
	var items []string
	for i := range promMaxSeries + 5 {
		items = append(items, fmt.Sprintf(`{"metric":{"i":"%d"},"value":[1,"1"]}`, i))
	}

	body := `{"status":"success","data":{"resultType":"vector","result":[` + strings.Join(items, ",") + `]}}`

	res, err := parsePromAnswer(200, []byte(body), promGrid{start: 1, step: 1, n: 1})
	if err != nil || len(res.Series) != promMaxSeries || !res.Truncated || res.Total != promMaxSeries+5 {
		t.Fatalf("got %d series, truncated %v, total %d, %v", len(res.Series), res.Truncated, res.Total, err)
	}
}

func TestPromCandidates(t *testing.T) {
	var services []promService

	add := func(ns, name string, labels map[string]string, ports ...string) {
		var s promService

		data := fmt.Sprintf(`{"metadata":{"name":%q,"namespace":%q},"spec":{"ports":[%s]}}`, name, ns, strings.Join(ports, ","))
		if err := json.Unmarshal([]byte(data), &s); err != nil {
			t.Fatal(err)
		}

		s.Metadata.Labels = labels
		services = append(services, s)
	}

	add("monitoring", "kps-kube-prometheus-stack-prometheus", map[string]string{"app.kubernetes.io/component": "prometheus"}, `{"name":"http-web","port":9090},{"name":"reloader-web","port":8080}`)
	add("monitoring", "prometheus-operated", nil, `{"name":"web","port":9090}`)
	add("monitoring", "kps-kube-prometheus-stack-operator", nil, `{"name":"https","port":443}`)
	add("monitoring", "kps-prometheus-node-exporter", nil, `{"name":"http-metrics","port":9100}`)
	add("monitoring", "kps-kube-prometheus-stack-alertmanager", nil, `{"name":"http-web","port":9093}`)
	add("mimir", "mimir-nginx", nil, `{"name":"http-metric","port":80}`)
	add("mimir", "mimir-query-frontend", nil, `{"name":"http-metrics","port":8080},{"name":"grpc","port":9095}`)
	add("mimir", "mimir-ingester", nil, `{"name":"http-metrics","port":8080}`)
	add("observability", "mimir", map[string]string{"app.kubernetes.io/name": "mimir"}, `{"name":"http-metrics","port":8080},{"name":"grpc","port":9095},{"name":"memberlist","port":7946}`)
	add("mimir", "mimir-query-scheduler", nil, `{"name":"http-metrics","port":8080}`)
	add("mimir", "mimir-gossip-ring", nil, `{"name":"gossip-ring","port":7946},{"name":"http-metrics","port":8080}`)
	add("vm", "vmselect-main", nil, `{"name":"http","port":8481}`)
	add("vm", "vmsingle-main", nil, `{"name":"http","port":8428}`)
	add("default", "prometheus-udp", nil, `{"name":"x","port":9090,"protocol":"UDP"}`)
	add("monitoring", "kube-prometheus-stack-thanos-discovery", nil, `{"name":"http","port":10902}`)
	add("default", "kubernetes", nil, `{"name":"https","port":443}`)

	var got []string
	for _, s := range promCandidates(services) {
		got = append(got, fmt.Sprintf("%s %s/%s:%d%s", s.Kind, s.Namespace, s.Service, s.Port, s.PathPrefix))
	}

	want := []string{
		"prometheus monitoring/prometheus-operated:9090",
		"mimir mimir/mimir-nginx:80/prometheus",
		"mimir mimir/mimir-query-frontend:8080/prometheus",
		"victoriametrics vm/vmsingle-main:8428",
		"mimir observability/mimir:8080/prometheus",
		"victoriametrics vm/vmselect-main:8481/select/0/prometheus",
		"prometheus monitoring/kps-kube-prometheus-stack-prometheus:9090",
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// useFakeKube points withKube at f for the test.
func useFakeKube(t *testing.T, f *fakeKubeAPI) {
	t.Helper()

	saved := kubeClients
	kubeClients = newKubeClientCache(func(kubeTarget) (*kubeClient, error) {
		return openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	})

	t.Cleanup(func() { kubeClients = saved })
}

func TestPromQueryRangeThroughServiceProxy(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/monitoring/services/mimir-nginx:80/proxy/prometheus/api/v1/query_range": `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"job":"a"},"values":[[60,"1"]]}]}}`,
		"GET /api/v1/services": `{"items":[{"metadata":{"name":"prometheus-operated","namespace":"monitoring"},"spec":{"ports":[{"name":"web","port":9090}]}}]}`,
	})
	useFakeKube(t, f)

	src := `{"mode":"proxy","namespace":"monitoring","service":"mimir-nginx","port":80,"pathPrefix":"/prometheus","tenant":"t1"}`

	out, err := PromQueryRange("cfg", "ctx", "", src, ` sum(up) `, 60, 120, 30)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"times":[60000,90000,120000]`) || !strings.Contains(out, `"values":[1,null,null]`) {
		t.Fatalf("got %s", out)
	}

	out, err = PromDiscover("cfg", "ctx", "")
	if err != nil || !strings.Contains(out, `"service":"prometheus-operated"`) {
		t.Fatalf("discover: %s %v", out, err)
	}

	// The fake answers unknown paths with the API server's own Status.
	missing := `{"mode":"proxy","namespace":"monitoring","service":"nope","port":9090}`
	if _, err := PromQueryRange("cfg", "ctx", "", missing, "up", 100, 101, 1); err == nil || !strings.Contains(err.Error(), "monitoring/nope: no such Service") {
		t.Fatalf("missing service: %v", err)
	}
}

func TestPromProxyPassesTenant(t *testing.T) {
	var tenant, query, timeout string

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, `{}`)

			return
		}

		tenant, query, timeout = r.Header.Get("X-Scope-OrgID"), r.URL.Query().Get("query"), r.URL.Query().Get("timeout")
		_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}))
	t.Cleanup(srv.Close)

	useFakeKube(t, &fakeKubeAPI{Server: srv})

	src := `{"mode":"proxy","namespace":"m","service":"s","port":1,"tenant":"t1"}`
	if _, err := PromQueryRange("cfg", "ctx", "", src, `rate(x{a="b c"}[5m])`, 100, 101, 1); err != nil {
		t.Fatal(err)
	}

	if tenant != "t1" || query != `rate(x{a="b c"}[5m])` || timeout != promServerTimeout {
		t.Fatalf("tenant %q query %q timeout %q", tenant, query, timeout)
	}
}

func TestPromQueryURLMode(t *testing.T) {
	var seen http.Header

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/prometheus/api/v1/query_range":
			seen = r.Header.Clone()
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[100,"42"]]}]}}`)
		default:
			http.Redirect(w, r, "https://elsewhere.example/", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)

	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	source := func(path string) string {
		b, _ := json.Marshal(promSource{Mode: promModeURL, URL: srv.URL + path, Auth: promAuthBearer, Secret: "tok", Tenant: "t1", CA: ca})

		return string(b)
	}

	out, err := PromQueryRange("cfg", "ctx", "", source("/prometheus"), "up", 100, 101, 1)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"values":[42,null]`) || seen.Get("Authorization") != "Bearer tok" || seen.Get("X-Scope-OrgID") != "t1" {
		t.Fatalf("got %s, headers %v", out, seen)
	}

	if _, err := PromQueryRange("cfg", "ctx", "", source("/moved"), "up", 100, 101, 1); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("redirect: %v", err)
	}

	// Without the CA, the test server's certificate is not trusted.
	untrusted, _ := json.Marshal(promSource{Mode: promModeURL, URL: srv.URL + "/prometheus"})
	if _, err := PromQueryRange("cfg", "ctx", "", string(untrusted), "up", 100, 101, 1); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}

func TestPromQueryRejectsBadQueries(t *testing.T) {
	src := `{"mode":"url","url":"https://p.invalid"}`

	for _, q := range []string{"  ", strings.Repeat("x", promMaxQuery+1), "up\x00"} {
		if _, err := PromQueryRange("cfg", "ctx", "", src, q, 1, 2, 1); err == nil {
			t.Errorf("query %.20q accepted", q)
		}
	}
}

func TestPromDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := PromDiscover(cfg, "Demo cluster", "")
	if err != nil || !strings.Contains(out, "prometheus-operated") {
		t.Fatalf("discover: %s %v", out, err)
	}

	src := `{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090}`

	for _, p := range promPresets {
		out, err := PromQueryRange(cfg, "Demo cluster", "", src, p.Query, 1, 3600, 0)
		if err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}

		var res promResult
		if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Series) == 0 || len(res.Series[0].Values) != len(res.Times) || res.Series[0].Values[0] == nil {
			t.Fatalf("%s: %s", p.ID, out)
		}
	}
}
