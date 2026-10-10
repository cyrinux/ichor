package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const promTestSource = `{"mode":"proxy","kind":"prometheus","namespace":"monitoring","service":"prometheus-operated","port":9090}`

const promTestProxy = "/api/v1/namespaces/monitoring/services/prometheus-operated:9090/proxy"

const promTestRuleDir = "/etc/prometheus/rules/prometheus-main-rulefiles-0/"

// Two operator groups (one per file naming), one plain rule file.
const promTestRules = `{"status":"success","data":{"groups":[
 {"name":"quiet","file":"/etc/prometheus/rules.yml","interval":60,"evaluationTime":0.001,"lastEvaluation":"0001-01-01T00:00:00Z","rules":[
  {"type":"recording","name":"job:up:sum","query":"sum by (job) (up)","health":"ok"}]},
 {"name":"apps","file":"` + promTestRuleDir + `monitoring-apps-rules-u1.yaml","interval":30,"evaluationTime":0.002,"lastEvaluation":"2026-01-01T10:00:00.5Z","rules":[
  {"type":"alerting","name":"Idle","state":"inactive","health":"ok","alerts":[]},
  {"type":"alerting","name":"Crash","state":"firing","health":"ok","duration":300,"labels":{"severity":"warning"},"alerts":[{"state":"firing"},{"state":"pending"}]},
  {"type":"alerting","name":"Slow","state":"PENDING","health":"ok","alerts":[{}]}]},
 {"name":"broken","file":"` + promTestRuleDir + `shop-shop-rules.yaml","interval":30,"rules":[
  {"type":"recording","name":"a:b","health":"ok"},
  {"type":"recording","name":"c:d","health":"err","lastError":"  many-to-many matching not allowed "},
  {"type":"alerting","name":"Odd","state":"weird","health":"strange"}]}
]}}`

func TestParsePromRules(t *testing.T) {
	data, err := promEnvelopeData(http.StatusOK, []byte(promTestRules))
	if err != nil {
		t.Fatal(err)
	}

	res, err := parsePromRules(data)
	if err != nil {
		t.Fatal(err)
	}

	if want := (promRuleCounts{Groups: 3, Rules: 7, Firing: 1, Pending: 1, Errors: 1}); res.Counts != want || res.Truncated {
		t.Fatalf("counts %+v", res.Counts)
	}

	var order []string
	for _, g := range res.Groups {
		var rules []string
		for _, r := range g.Rules {
			rules = append(rules, r.Name+"/"+r.State+"/"+r.Health)
		}

		order = append(order, g.Name+"["+strings.Join(rules, " ")+"]")
	}

	want := "broken[c:d/inactive/err a:b/inactive/ok Odd/inactive/unknown] apps[Crash/firing/ok Slow/pending/ok Idle/inactive/ok] quiet[job:up:sum/inactive/ok]"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("order\n got  %s\n want %s", got, want)
	}

	broken, apps, quiet := res.Groups[0], res.Groups[1], res.Groups[2]
	if broken.LastError != "many-to-many matching not allowed" || broken.Errors != 1 || broken.Rules[0].Type != promRuleRecording {
		t.Fatalf("broken %+v", broken)
	}

	if crash := apps.Rules[0]; crash.Alerts != 2 || crash.Severity != "warning" || crash.Duration != 300 || crash.Type != promRuleAlerting || apps.LastEvaluation != 1767261600500 {
		t.Fatalf("crash %+v group %+v", crash, apps)
	}

	if quiet.LastEvaluation != 0 || quiet.Interval != 60 {
		t.Fatalf("quiet %+v", quiet)
	}

	if !res.fromOperator() {
		t.Fatal("operator files not seen")
	}

	res.setOwners([]kubeRowMeta{{Namespace: "monitoring", Name: "apps-rules", UID: "u1"}, {Namespace: "shop", Name: "shop-rules", UID: "u2"}, {Namespace: "x", Name: "quiet"}})

	if g := res.Groups[0]; g.RuleNamespace != "shop" || g.RuleName != "shop-rules" {
		t.Errorf("old-style file: %+v", g)
	}

	if g := res.Groups[1]; g.RuleNamespace != "monitoring" || g.RuleName != "apps-rules" {
		t.Errorf("uid file: %+v", g)
	}

	if g := res.Groups[2]; g.RuleName != "" {
		t.Errorf("plain file owned: %+v", g)
	}

	for _, bad := range []string{`{}`, `[]`, `"x"`} {
		if _, err := parsePromRules(json.RawMessage(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestParsePromRulesTruncates(t *testing.T) {
	rules := make([]string, promMaxRules+3)
	for i := range rules {
		rules[i] = fmt.Sprintf(`{"type":"alerting","name":"r%d","state":"firing","health":"ok"}`, i)
	}

	res, err := parsePromRules(json.RawMessage(`{"groups":[{"name":"g","rules":[` + strings.Join(rules, ",") + `]}]}`))
	if err != nil {
		t.Fatal(err)
	}

	if !res.Truncated || res.Counts.Rules != promMaxRules+3 || res.Counts.Firing != promMaxRules+3 || len(res.Groups[0].Rules) != promMaxRules {
		t.Fatalf("truncated %v counts %+v listed %d", res.Truncated, res.Counts, len(res.Groups[0].Rules))
	}
}

func TestRuleFileOwner(t *testing.T) {
	owners := []kubeRowMeta{{Namespace: "a-b", Name: "c", UID: "1"}, {Namespace: "a", Name: "b-c", UID: "2"}}

	for file, want := range map[string]string{
		promTestRuleDir + "a-b-c-1.yaml": "a-b/c",
		promTestRuleDir + "a-b-c-2.yaml": "a/b-c",
		promTestRuleDir + "a-b-c-3.yaml": "",
		"/etc/rules/a-b-c-1.yaml":        "", // not the operator's directory
		promTestRuleDir + "a-b-c.yaml":   "a-b/c",
	} {
		got := ""
		if o, ok := ruleFileOwner(file, owners); ok {
			got = o.Namespace + "/" + o.Name
		}

		if got != want {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
}

func TestPromRulesThroughServiceProxy(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET " + promTestProxy + "/api/v1/rules":             promTestRules,
		"GET /apis/monitoring.coreos.com/v1/prometheusrules": `{"items":[{"metadata":{"name":"apps-rules","namespace":"monitoring","uid":"u1"}}]}`,
	})
	useFakeKube(t, f)

	out, err := PromRules("cfg", "ctx", "", promTestSource)
	if err != nil {
		t.Fatal(err)
	}

	var res promRulesResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Groups[1].RuleName != "apps-rules" || res.Groups[0].RuleName != "" || res.Counts.Errors != 1 {
		t.Fatalf("%s", out)
	}

	if _, err := PromRules("cfg", "ctx", "", `{"mode":"ftp"}`); err == nil {
		t.Fatal("bad source accepted")
	}
}

func TestPromRulesErrors(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET " + promTestProxy + "/api/v1/rules":                                    `{"status":"error","errorType":"unavailable","error":"rule manager not ready"}`,
		"GET /api/v1/namespaces/monitoring/services/thanos:9090/proxy/api/v1/rules": `{"status":"success","data":{}}`,
	})
	f.answerWith("GET "+promTestProxy+"/api/v1/rules", http.StatusServiceUnavailable, `{"status":"error","errorType":"unavailable","error":"rule manager not ready"}`)
	f.answerWith("GET /api/v1/namespaces/monitoring/services/mimir:8080/proxy/api/v1/rules", http.StatusNotFound, "404 page not found")
	useFakeKube(t, f)

	for src, want := range map[string]string{
		promTestSource: "monitoring/prometheus-operated: unavailable: rule manager not ready",
		strings.Replace(promTestSource, "prometheus-operated", "thanos", 1):                                    "monitoring/thanos: the rules answer has no groups",
		strings.Replace(strings.Replace(promTestSource, "prometheus-operated", "mimir", 1), "9090", "8080", 1): "monitoring/mimir: /api/v1/rules is not served here",
	} {
		if _, err := PromRules("cfg", "ctx", "", src); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %s", src, err, want)
		}
	}
}
