package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPromOperatorStatusAbsent(t *testing.T) {
	useFakeKube(t, newFakeKubeAPI(t, map[string]string{}))

	out, err := PromOperatorStatus("cfg", "ctx", "")
	if err != nil || out != `{"installed":false,"error":"","prometheuses":[],"alertmanagers":[],"serviceMonitors":0,"podMonitors":0,"prometheusRules":0,"probes":0}` {
		t.Fatalf("%s %v", out, err)
	}
}

func TestPromOperatorStatus(t *testing.T) {
	const base = "GET /apis/monitoring.coreos.com/v1"

	f := newFakeKubeAPI(t, map[string]string{
		base: `{"resources":[{"name":"prometheuses"},{"name":"prometheuses/status"},{"name":"alertmanagers"},{"name":"servicemonitors"},{"name":"podmonitors"},{"name":"prometheusrules"}]}`,
		base + "/prometheuses": `{"items":[
		  {"metadata":{"name":"main","namespace":"mon"},"spec":{"replicas":2,"shards":2,"version":"v3.5.0"},"status":{"availableReplicas":3,"conditions":[
		    {"type":"Available","status":"Degraded","reason":"SomePodsNotReady","message":"shard 1: pod main-shard-1-1 is not ready"},{"type":"Reconciled","status":"True"}]}},
		  {"metadata":{"name":"fine","namespace":"mon"},"spec":{},"status":{"availableReplicas":1,"conditions":[{"type":"Available","status":"True"},{"type":"Reconciled","status":"True"}]}},
		  {"metadata":{"name":"off","namespace":"mon"},"spec":{"replicas":0,"paused":true},"status":{"conditions":[{"type":"Reconciled","status":"False","reason":"InvalidConfiguration"}]}}]}`,
		base + "/alertmanagers":   `{"items":[{"metadata":{"name":"am","namespace":"mon"},"spec":{"replicas":3,"shards":4},"status":{"availableReplicas":0,"conditions":[{"type":"Available","status":"False","reason":"NoPodReady"}]}}]}`,
		base + "/servicemonitors": `{"items":[{"metadata":{"name":"a"}},{"metadata":{"name":"b"}}]}`,
		base + "/prometheusrules": `{"kind":"Table","columnDefinitions":[{"name":"Name"}],"rows":[{"cells":["r"],"object":{"metadata":{"name":"r","namespace":"mon","uid":"u"}}}]}`,
	})
	f.answerWith(base+"/podmonitors", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"podmonitors is forbidden"}`)
	useFakeKube(t, f)

	out, err := PromOperatorStatus("cfg", "ctx", "")
	if err != nil {
		t.Fatal(err)
	}

	var st promOperatorStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}

	if !st.Installed || st.ServiceMonitors != 2 || st.PrometheusRules != 1 || st.PodMonitors != 0 || st.Probes != 0 || !strings.Contains(st.Error, "forbidden") {
		t.Fatalf("%s", out)
	}

	var got []string
	for _, s := range st.Prometheuses {
		got = append(got, s.Name+":"+s.Health)
	}

	if want := []string{"main:warning", "off:warning", "fine:ok"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prometheuses %v, want %v", got, want)
	}

	main := st.Prometheuses[0]
	if main.Replicas != 2 || main.Shards != 2 || main.Desired != 4 || main.Available != 3 || main.Version != "v3.5.0" || len(main.Conditions) != 2 || main.Conditions[0].Reason != "SomePodsNotReady" {
		t.Fatalf("main %+v", main)
	}

	if off := st.Prometheuses[1]; off.Desired != 0 || !off.Paused || len(off.Conditions) != 1 {
		t.Fatalf("off %+v", off)
	}

	if am := st.Alertmanagers[0]; am.Health != healthCritical || am.Shards != 1 || am.Desired != 3 {
		t.Fatalf("alertmanager %+v", am)
	}
}

func TestPromOperatorStatusDiscoveryError(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	f.answerWith("GET /apis/monitoring.coreos.com/v1", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"forbidden"}`)
	useFakeKube(t, f)

	if _, err := PromOperatorStatus("cfg", "ctx", ""); err == nil {
		t.Fatal("a refused discovery passed for no operator")
	}
}

// checkupMonitoringOn runs the monitoring section against f.
func checkupMonitoringOn(t *testing.T, f *fakeKubeAPI, groups map[string]string) checkupSection {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return checkupMonitoring(context.Background(), k, checkupInput{now: checkupNow, groups: groups})
}

const promTestServices = `{"items":[{"metadata":{"name":"prometheus-operated","namespace":"monitoring"},"spec":{"ports":[{"name":"web","port":9090}]}}]}`

func TestCheckupMonitoring(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/services":                               promTestServices,
		"GET " + promTestProxy + "/api/v1/rules":             promTestRules,
		"GET " + promTestProxy + "/api/v1/targets":           promTestTargets,
		"GET /apis/monitoring.coreos.com/v1/prometheusrules": `{"items":[{"metadata":{"name":"shop-rules","namespace":"shop"}}]}`,
	})

	s := checkupMonitoringOn(t, f, map[string]string{groupMonitoring: "v1"})

	if got, want := findingKinds(s), []string{"scrapeTargetsDown:critical:prometheus-operated", "prometheusRuleErrors:warning:shop-rules"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %v, want %v (error %q)", got, want, s.Error)
	}

	if down := s.Findings[0]; down.Count != 3 || down.Limit != 6 || down.Value != 50 || down.Extra != "serviceMonitor/shop/web/0" || down.Namespace != "monitoring" {
		t.Fatalf("down %+v", down)
	}

	if rule := s.Findings[1]; rule.Namespace != "shop" || rule.Count != 1 || rule.Reason != "c:d" || rule.Message != "many-to-many matching not allowed" {
		t.Fatalf("rule %+v", rule)
	}

	if s.Status != healthCritical || s.Checked != 13 || s.Error != "" {
		t.Fatalf("section %+v", s)
	}

	// Without the operator, the failing group is named by its file.
	s = checkupMonitoringOn(t, f, map[string]string{})
	if got := findingKinds(s); len(got) != 2 || got[1] != "ruleGroupErrors:warning:broken" || s.Findings[1].Extra != promTestRuleDir+"shop-shop-rules.yaml" {
		t.Fatalf("findings %v %+v", got, s.Findings)
	}
}

func TestCheckupMonitoringPartial(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/services":                   promTestServices,
		"GET " + promTestProxy + "/api/v1/rules": `{"status":"success","data":{"groups":[]}}`,
	})
	// A server without the targets API (Mimir): that part is left out, not an error.
	f.answerWith("GET "+promTestProxy+"/api/v1/targets", http.StatusNotFound, "404 page not found")

	if s := checkupMonitoringOn(t, f, nil); s.Status != healthOK || s.Error != "" || len(s.Findings) != 0 {
		t.Fatalf("no targets API: %+v", s)
	}

	// A Prometheus that does not answer makes the section unknown.
	f = newFakeKubeAPI(t, map[string]string{"GET /api/v1/services": promTestServices})
	f.answerWith("GET "+promTestProxy+"/api/v1/rules", http.StatusServiceUnavailable, `{"kind":"Status","message":"no endpoints available"}`)
	f.answerWith("GET "+promTestProxy+"/api/v1/targets", http.StatusOK, `{"status":"success","data":{}}`)

	if s := checkupMonitoringOn(t, f, nil); s.Status != checkUnknown || !strings.Contains(s.Error, "no ready pod") || !strings.Contains(s.Error, "no active targets") {
		t.Fatalf("unreachable: %+v", s)
	}

	// No Prometheus: absent; no Services list: unknown.
	if s := checkupMonitoringOn(t, newFakeKubeAPI(t, map[string]string{"GET /api/v1/services": `{"items":[]}`}), nil); s.Status != checkAbsent {
		t.Fatalf("absent: %+v", s)
	}

	if s := checkupMonitoringOn(t, newFakeKubeAPI(t, map[string]string{}), nil); s.Status != checkUnknown {
		t.Fatalf("no services: %+v", s)
	}
}

func TestMonitoringFindingsThreshold(t *testing.T) {
	src := promSource{Namespace: "m", Service: "p"}

	for down, want := range map[int]string{0: "", 1: sevWarning, 25: sevWarning, 26: sevCritical} {
		res := promTargetsResult{Down: down, Total: 100}

		got := ""
		if f := monitoringFindings(src, &res, nil); len(f) == 1 {
			got = f[0].Severity
		}

		if got != want {
			t.Errorf("%d down of 100: %q, want %q", down, got, want)
		}
	}

	// Two groups of one PrometheusRule make one finding.
	rules := promRulesResult{Groups: []promRuleGroup{
		{Name: "a", RuleNamespace: "n", RuleName: "r", Errors: 1, Rules: []promRule{{Name: "x", Health: promHealthErr, LastError: "e"}}},
		{Name: "b", RuleNamespace: "n", RuleName: "r", Errors: 2},
		{Name: "c", Errors: 3},
	}}

	f := monitoringFindings(src, nil, &rules)
	if len(f) != 2 || f[0].Kind != findPromRuleErrors || f[0].Count != 3 || f[0].Reason != "x" || f[1].Kind != findRuleGroupErrors || f[1].Count != 3 {
		t.Fatalf("%+v", f)
	}
}

func TestPromOperatorDemo(t *testing.T) {
	for name, cfg := range map[string]string{"talos": mustDemoConfig(t), "kube": demoKubeconfigForTest(t)} {
		ctx := "Demo cluster"
		if name == "kube" {
			ctx = ""
		}

		out, err := PromRules(cfg, ctx, "", promTestSource)
		if err != nil {
			t.Fatal(err)
		}

		var rules promRulesResult
		_ = json.Unmarshal([]byte(out), &rules)

		if rules.Counts.Firing != 1 || rules.Counts.Errors != 1 || rules.Groups[0].RuleName != "hello-ichor" || rules.Groups[1].RuleName != "kube-prometheus-stack-kubernetes-apps" {
			t.Fatalf("%s rules %s", name, out)
		}

		out, err = PromTargets(cfg, ctx, "", promTestSource)
		if err != nil {
			t.Fatal(err)
		}

		var targets promTargetsResult
		_ = json.Unmarshal([]byte(out), &targets)

		if targets.Down != 2 || targets.Pools[0].Pool != "serviceMonitor/demo/hello-ichor/0" || len(targets.Pools[0].Targets) != 2 || targets.Pools[0].Targets[0].Pod != "hello-ichor-7d9c5-fghij" {
			t.Fatalf("%s targets %s", name, out)
		}

		out, err = PromOperatorStatus(cfg, ctx, "")
		if err != nil || !strings.Contains(out, `"installed":true`) {
			t.Fatalf("%s operator %s %v", name, out, err)
		}

		var st promOperatorStatus
		_ = json.Unmarshal([]byte(out), &st)

		if st.Prometheuses[0].Health != healthOK || st.Alertmanagers[0].Health != healthOK {
			t.Fatalf("%s operator %s", name, out)
		}
	}

	s := demoCheckupMonitoring(time.Now())
	if got, want := findingKinds(s), []string{"scrapeTargetsDown:warning:prometheus-operated", "prometheusRuleErrors:warning:hello-ichor"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("demo checkup %v", got)
	}
}
