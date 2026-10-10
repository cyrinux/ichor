package ichorgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const promTestTargets = `{"status":"success","data":{"activeTargets":[
 {"scrapePool":"serviceMonitor/shop/web/0","scrapeUrl":"http://10.1.0.2:8080/metrics","health":"up","labels":{"job":"web","namespace":"shop","pod":"web-a"}},
 {"scrapePool":"serviceMonitor/shop/web/0","scrapeUrl":"http://10.1.0.4:8080/metrics","health":"down","lastError":" connection refused ","lastScrape":"2026-01-01T10:00:00Z","lastScrapeDuration":0.5,
  "labels":{"job":"web","namespace":"shop","service":"web","pod":"web-c","instance":"10.1.0.4:8080","endpoint":"metrics"},"discoveredLabels":{"__address__":"10.1.0.4:8080"}},
 {"scrapePool":"serviceMonitor/shop/web/0","scrapeUrl":"http://10.1.0.3:8080/metrics","health":"down","lastScrape":"0001-01-01T00:00:00Z","labels":{"namespace":"shop","pod":"web-b"}},
 {"scrapePool":"podMonitor/shop/worker/1","health":"down","labels":{"namespace":"shop","pod":"worker-a"}},
 {"scrapePool":"kubernetes-nodes","health":"up","labels":{}},
 {"scrapePool":"kubernetes-nodes","health":"unknown","labels":{}}
]}}`

func TestParseScrapePool(t *testing.T) {
	for pool, want := range map[string]string{
		"serviceMonitor/monitoring/kube-prometheus-stack-kubelet/1": "ServiceMonitor monitoring kube-prometheus-stack-kubelet 1",
		"podMonitor/shop/worker/0":                                  "PodMonitor shop worker 0",
		"probe/shop/blackbox":                                       "Probe shop blackbox -1",
		"scrapeConfig/shop/static":                                  "ScrapeConfig shop static -1",
		"serviceMonitor/shop/web/x":                                 "ServiceMonitor shop web -1",
		"serviceMonitor/shop":                                       "   -1",
		"serviceMonitor//web/0":                                     "   -1",
		"serviceMonitor/a/b/c/d":                                    "   -1",
		"kubernetes-pods":                                           "   -1",
		"":                                                          "   -1",
	} {
		p := parseScrapePool(pool)
		if got := fmt.Sprintf("%s %s %s %d", p.Kind, p.Namespace, p.Name, p.Endpoint); got != want || p.Pool != pool || p.Targets == nil {
			t.Errorf("%q: %q, want %q", pool, got, want)
		}
	}
}

func TestParsePromTargets(t *testing.T) {
	data, err := promEnvelopeData(http.StatusOK, []byte(promTestTargets))
	if err != nil {
		t.Fatal(err)
	}

	res, err := parsePromTargets(data)
	if err != nil {
		t.Fatal(err)
	}

	if res.Up != 2 || res.Down != 3 || res.Unknown != 1 || res.Total != 6 || res.Truncated {
		t.Fatalf("totals %+v", res)
	}

	var order []string
	for _, p := range res.Pools {
		order = append(order, fmt.Sprintf("%s:%d/%d/%d", p.Pool, p.Up, p.Down, p.Unknown))
	}

	if got, want := strings.Join(order, " "), "serviceMonitor/shop/web/0:1/2/0 podMonitor/shop/worker/1:0/1/0 kubernetes-nodes:1/0/1"; got != want {
		t.Fatalf("pools %s, want %s", got, want)
	}

	web := res.Pools[0]
	if len(web.Targets) != 2 || web.Targets[0].Pod != "web-b" || web.Kind != "ServiceMonitor" || web.Endpoint != 0 {
		t.Fatalf("web %+v", web)
	}

	if c := web.Targets[1]; c.LastError != "connection refused" || c.LastScrape != 1767261600000 || c.Service != "web" || c.Instance != "10.1.0.4:8080" || c.Job != "web" || c.LastScrapeDuration != 0.5 {
		t.Fatalf("web-c %+v", c)
	}

	if web.Targets[0].LastScrape != 0 || res.Pools[2].Targets == nil {
		t.Fatalf("zero time or nil targets: %+v", res.Pools)
	}

	if _, err := parsePromTargets(json.RawMessage(`{"droppedTargets":[]}`)); err == nil {
		t.Fatal("an answer without active targets was accepted")
	}
}

func TestGroupPromTargetsTruncates(t *testing.T) {
	targets := make([]promRawTarget, promMaxDownTargets+4)
	for i := range targets {
		targets[i] = promRawTarget{ScrapePool: "p", Health: "down"}
	}

	res := groupPromTargets(targets)
	if !res.Truncated || res.Down != promMaxDownTargets+4 || res.Pools[0].Down != promMaxDownTargets+4 || len(res.Pools[0].Targets) != promMaxDownTargets {
		t.Fatalf("truncated %v down %d", res.Truncated, res.Down)
	}
}

func TestPromTargetsURLMode(t *testing.T) {
	var query string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery

		switch r.URL.Path {
		case "/prom/api/v1/targets":
			_, _ = io.WriteString(w, promTestTargets)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	src := `{"mode":"url","url":"` + srv.URL + `/prom"}`

	out, err := PromTargets("cfg", "ctx", "", src)
	if err != nil || query != "state=active" || !strings.Contains(out, `"up":2,"down":3,"unknown":1,"total":6`) {
		t.Fatalf("%s %v (query %q)", out, err, query)
	}

	if _, err := PromTargets("cfg", "ctx", "", `{"mode":"url","url":"`+srv.URL+`"}`); err == nil || !strings.Contains(err.Error(), "/api/v1/targets is not served here") {
		t.Fatalf("404: %v", err)
	}

	if _, err := PromTargets("cfg", "ctx", "", `{`); err == nil {
		t.Fatal("bad source accepted")
	}
}

func TestPromTargetsBadAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"success","data":"nope"}`)
	}))
	t.Cleanup(srv.Close)

	if _, err := PromTargets("cfg", "ctx", "", `{"mode":"url","url":"`+srv.URL+`"}`); err == nil || !strings.Contains(err.Error(), "no active targets") {
		t.Fatalf("%v", err)
	}
}
