package ichorgo

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestParsePromMetricNames(t *testing.T) {
	body := `{"status":"success","data":["up","node_load1","bad name","up","1abc",":rule:x","node_cpu_seconds_total"]}`

	names, err := parsePromMetricNames(200, []byte(body))
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{":rule:x", "node_cpu_seconds_total", "node_load1", "up"}; !slices.Equal(names, want) {
		t.Fatalf("got %q, want %q", names, want)
	}

	for body, want := range map[string]string{
		`{"status":"error","errorType":"bad_data","error":"invalid label"}`: "bad_data: invalid label",
		`{"status":"success","data":{"a":1}}`:                               "no data",
		`404 page not found`:                                                "path prefix",
		`<html>`:                                                            "not from a Prometheus",
	} {
		status := 200
		if strings.HasPrefix(body, "404") {
			status = 404
		}

		if _, err := parsePromMetricNames(status, []byte(body)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", body, err, want)
		}
	}

	// Capped, never nil.
	many := make([]string, promMetricNamesMax+10)
	for i := range many {
		many[i] = fmt.Sprintf("m%06d", i)
	}

	raw, _ := json.Marshal(map[string]any{"status": "success", "data": many})

	if names, err := parsePromMetricNames(200, raw); err != nil || len(names) != promMetricNamesMax {
		t.Fatalf("cap: %d names, %v", len(names), err)
	}

	if names, err := parsePromMetricNames(200, []byte(`{"status":"success","data":[]}`)); err != nil || names == nil || len(names) != 0 {
		t.Fatalf("empty: %v %v", names, err)
	}
}

func TestPromMetricNamesURLMode(t *testing.T) {
	var path, tenant string

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, tenant = r.URL.Path, r.Header.Get("X-Scope-OrgID")
		_, _ = io.WriteString(w, `{"status":"success","data":["up","node_load1"]}`)
	}))
	t.Cleanup(srv.Close)

	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	src, _ := json.Marshal(promSource{Mode: promModeURL, URL: srv.URL + "/prometheus", Tenant: "t1", CA: ca})

	out, err := PromMetricNames("cfg", "ctx", "", string(src))
	if err != nil || out != `["node_load1","up"]` {
		t.Fatalf("got %s, %v", out, err)
	}

	if path != "/prometheus/api/v1/label/__name__/values" || tenant != "t1" {
		t.Fatalf("path %q, tenant %q", path, tenant)
	}

	srv.Close()

	if _, err := PromMetricNames("cfg", "ctx", "", string(src)); err == nil {
		t.Fatal("a closed server must be an error")
	}
}

func TestPromMetricNamesThroughServiceProxy(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/monitoring/services/prometheus-operated:9090/proxy/api/v1/label/__name__/values": `{"status":"success","data":["up"]}`,
	})
	useFakeKube(t, f)

	out, err := PromMetricNames("cfg", "ctx", "", `{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090}`)
	if err != nil || out != `["up"]` {
		t.Fatalf("got %s, %v", out, err)
	}

	if _, err := PromMetricNames("cfg", "ctx", "", `{"mode":"proxy","namespace":"monitoring","service":"nope","port":9090}`); err == nil || !strings.Contains(err.Error(), "no such Service") {
		t.Fatalf("missing service: %v", err)
	}
}

func TestPromMetricNamesDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := PromMetricNames(cfg, "Demo cluster", "", `{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090}`)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil || !slices.IsSorted(names) {
		t.Fatalf("demo names: %s", out)
	}

	// Every preset's metrics are there, so the assistant is not told they are missing.
	for _, p := range promPresets {
		for _, m := range []string{"node_cpu_seconds_total", "container_memory_working_set_bytes", "kube_pod_container_status_restarts_total", "apiserver_request_total", "etcd_disk_wal_fsync_duration_seconds_bucket", "node_filesystem_avail_bytes"} {
			if strings.Contains(p.Query, m) && !slices.Contains(names, m) {
				t.Errorf("%s: %s is not in the demo names", p.ID, m)
			}
		}
	}
}
