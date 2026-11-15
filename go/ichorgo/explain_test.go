package ichorgo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// All of it invented: a CrashLoop pod with a previous run, its events and its log.
var explainPodAnswers = map[string]string{
	"GET /api/v1/namespaces/acme-shop/pods/cart-5c9-tt7x2": `{
		"metadata":{"namespace":"acme-shop","name":"cart-5c9-tt7x2","ownerReferences":[{"kind":"ReplicaSet","name":"cart-5c9"}]},
		"spec":{"nodeName":"acme-node-1","containers":[{"name":"sidecar","image":"envoy:1"},{"name":"cart","image":"registry.acme.example/shop/cart:3"}]},
		"status":{"phase":"Running","containerStatuses":[
			{"name":"sidecar","ready":true,"restartCount":0,"state":{"running":{}}},
			{"name":"cart","ready":false,"restartCount":23,
			 "state":{"waiting":{"reason":"CrashLoopBackOff","message":"back-off 5m0s restarting failed container"}},
			 "lastState":{"terminated":{"reason":"OOMKilled","exitCode":137}}}]}}`,
	"GET /api/v1/namespaces/acme-shop/events": `{"items":[
		{"metadata":{"namespace":"acme-shop"},"involvedObject":{"kind":"Pod","namespace":"acme-shop","name":"cart-5c9-tt7x2"},
		 "type":"Warning","reason":"BackOff","count":40,"lastTimestamp":"2026-10-31T05:00:00Z",
		 "message":"Back-off restarting failed container cart in pod cart-5c9-tt7x2"}]}`,
	"GET /api/v1/namespaces/acme-shop/pods/cart-5c9-tt7x2/log": "\x1b[31mstarting cart\x1b[0m\nallocating the price cache\nfatal: out of memory\n",
}

func explainFor(t *testing.T, answers map[string]string, kind, subject string, anonymize bool) (*Diagnosis, *fakeKubeAPI) {
	t.Helper()

	f := newFakeKubeAPI(t, answers)
	useFakeKube(t, f)

	d, err := CollectExplanation("cfg", "ctx", "", kind, subject, anonymize)
	if err != nil {
		t.Fatal(err)
	}

	return d, f
}

func TestExplainPod(t *testing.T) {
	d, f := explainFor(t, explainPodAnswers, explainPod, `{"namespace":"acme-shop","name":"cart-5c9-tt7x2"}`, false)
	report := d.Report()

	for _, part := range []string{
		"Pod acme-shop/cart-5c9-tt7x2",
		"status CrashLoopBackOff, phase Running, 1 of 2 ready, 23 restarts, on acme-node-1",
		"cart: waiting CrashLoopBackOff (back-off 5m0s restarting failed container), 23 restarts, last ended OOMKilled (exit 137)",
		"sidecar: running",
		"EVENTS (1, newest first)",
		"BackOff, 40 times",
		"LOG of container cart, previous run (last 3 lines)",
		"fatal: out of memory",
	} {
		if !strings.Contains(report, part) {
			t.Errorf("report lacks %q:\n%s", part, report)
		}
	}

	if strings.Contains(report, "\x1b") {
		t.Errorf("ANSI escapes left in the report:\n%q", report)
	}

	var logQuery string

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, "/log") {
			logQuery = r.query
		}
	}

	if !strings.Contains(logQuery, "container=cart") || !strings.Contains(logQuery, "previous=true") || !strings.Contains(logQuery, "tailLines=40") {
		t.Errorf("log read with %q", logQuery)
	}

	if d.kind != diagnosisExplain || d.tools != nil {
		t.Errorf("kind %v, tools %v: a single-shot report", d.kind, d.tools)
	}
}

func TestExplainPodAnonymized(t *testing.T) {
	d, _ := explainFor(t, explainPodAnswers, explainPod, `{"namespace":"acme-shop","name":"cart-5c9-tt7x2"}`, true)
	report := d.Report()

	for _, real := range []string{"acme-shop", "cart-5c9-tt7x2", "registry.acme.example", "acme-node-1"} {
		if strings.Contains(report, real) {
			t.Errorf("anonymized report still names %q:\n%s", real, report)
		}
	}

	if got := d.reveal(d.names.mask("acme-shop/cart-5c9-tt7x2")); got != "acme-shop/cart-5c9-tt7x2" {
		t.Errorf("reveal gives %q", got)
	}
}

func TestExplainPodLogDenied(t *testing.T) {
	answers := map[string]string{}
	for k, v := range explainPodAnswers {
		answers[k] = v
	}

	delete(answers, "GET /api/v1/namespaces/acme-shop/pods/cart-5c9-tt7x2/log")

	f := newFakeKubeAPI(t, answers)
	f.answerWith("GET /api/v1/namespaces/acme-shop/pods/cart-5c9-tt7x2/log", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"no"}`)
	useFakeKube(t, f)

	d, err := CollectExplanation("cfg", "ctx", "", explainPod, `{"namespace":"acme-shop","name":"cart-5c9-tt7x2"}`, false)
	if err != nil {
		t.Fatal(err)
	}

	if report := d.Report(); !strings.Contains(report, "logs not readable: the credentials may not read pods/log") || !strings.Contains(report, "OOMKilled") {
		t.Errorf("a denied log read must leave a note and the rest:\n%s", report)
	}
}

func TestExplainAlert(t *testing.T) {
	answers := map[string]string{
		"GET " + amTestProxy + "/api/v2/alerts": `[
			{"fingerprint":"other","labels":{"alertname":"Watchdog"},"status":{"state":"active"}},
			{"fingerprint":"fp1","labels":{"alertname":"KubePodCrashLooping","severity":"warning","namespace":"acme-shop","pod":"cart-5c9-tt7x2","container":"cart"},
			 "annotations":{"summary":"Pod is crash looping.","description":"Pod acme-shop/cart-5c9-tt7x2 (cart) is in waiting state.","runbook_url":"https://runbooks.example/kubepodcrashlooping"},
			 "startsAt":"2026-10-31T04:00:00Z","status":{"state":"active"}}]`,
		"GET /api/v1/namespaces/acme-shop/events": explainPodAnswers["GET /api/v1/namespaces/acme-shop/events"],
	}
	subject, _ := json.Marshal(map[string]string{"source": amTestSource, "fingerprint": "fp1"})

	d, f := explainFor(t, answers, explainAlert, string(subject), false)
	report := d.Report()

	for _, part := range []string{
		"Alert KubePodCrashLooping (warning), active since 2026-10-31 04:00",
		"summary: Pod is crash looping.",
		"runbook: https://runbooks.example/kubepodcrashlooping",
		"pod: cart-5c9-tt7x2",
		"EVENTS (1, newest first)",
	} {
		if !strings.Contains(report, part) {
			t.Errorf("report lacks %q:\n%s", part, report)
		}
	}

	if strings.Contains(report, "Watchdog") {
		t.Errorf("another alert in the report:\n%s", report)
	}

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, "/api/v2/alerts") && !strings.Contains(r.query, "silenced=true") {
			t.Errorf("a silenced alert must be found too: %s", r.query)
		}
	}

	subject, _ = json.Marshal(map[string]string{"source": amTestSource, "fingerprint": "gone"})
	if _, err := CollectExplanation("cfg", "ctx", "", explainAlert, string(subject), false); err == nil || !strings.Contains(err.Error(), "no longer") {
		t.Errorf("a resolved alert: %v", err)
	}
}

func TestExplainFinding(t *testing.T) {
	answers := map[string]string{
		"GET /api/v1/namespaces/acme-shop/events": `{"items":[
			{"metadata":{"namespace":"acme-shop"},"involvedObject":{"kind":"PersistentVolumeClaim","namespace":"acme-shop","name":"orders-db"},
			 "type":"Warning","reason":"ProvisioningFailed","count":2,"lastTimestamp":"2026-10-31T05:00:00Z","message":"storageclass \"fast\" not found"},
			{"metadata":{"namespace":"acme-shop"},"involvedObject":{"kind":"Pod","namespace":"acme-shop","name":"cart-5c9-tt7x2"},
			 "type":"Warning","reason":"BackOff","count":1,"lastTimestamp":"2026-10-31T05:00:00Z","message":"unrelated"}]}`,
	}
	subject := `{"finding":{"kind":"pvcPending","severity":"warning","namespace":"acme-shop","name":"PersistentVolumeClaim/orders-db","reason":"ProvisioningFailed"},
		"text":"A volume claim waits for a volume","fix":"Check the storage class"}`

	d, _ := explainFor(t, answers, explainFinding, subject, false)
	report := d.Report()

	for _, part := range []string{
		"Checkup finding: A volume claim waits for a volume",
		"[warning] pvcPending acme-shop/PersistentVolumeClaim/orders-db: ProvisioningFailed",
		"suggested fix: Check the storage class",
		"storageclass \"fast\" not found",
	} {
		if !strings.Contains(report, part) {
			t.Errorf("report lacks %q:\n%s", part, report)
		}
	}

	if strings.Contains(report, "unrelated") {
		t.Errorf("the events of another object:\n%s", report)
	}
}

func TestExplainBadSubject(t *testing.T) {
	for name, c := range map[string][2]string{
		"kind":        {"node", `{}`},
		"json":        {explainPod, `{`},
		"pod name":    {explainPod, `{"namespace":"a","name":"B C"}`},
		"fingerprint": {explainAlert, `{"source":{},"fingerprint":""}`},
		"finding":     {explainFinding, `{}`},
	} {
		if _, err := CollectExplanation("cfg", "ctx", "", c[0], c[1], false); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestExplainReportCap(t *testing.T) {
	answers := map[string]string{}
	for k, v := range explainPodAnswers {
		answers[k] = v
	}

	answers["GET /api/v1/namespaces/acme-shop/pods/cart-5c9-tt7x2/log"] = strings.Repeat(strings.Repeat("y", 290)+"\n", 40)

	f := newFakeKubeAPI(t, answers)
	useFakeKube(t, f)

	events := `{"items":[`
	for i := range 15 {
		if i > 0 {
			events += ","
		}

		events += `{"metadata":{"namespace":"acme-shop"},"involvedObject":{"kind":"Pod","namespace":"acme-shop","name":"cart-5c9-tt7x2"},"type":"Warning","reason":"BackOff","message":"` + strings.Repeat("z", 390) + `"}`
	}

	f.answers["GET /api/v1/namespaces/acme-shop/events"] = events + "]}"

	d, err := CollectExplanation("cfg", "ctx", "", explainPod, `{"namespace":"acme-shop","name":"cart-5c9-tt7x2"}`, false)
	if err != nil {
		t.Fatal(err)
	}

	if n := len(d.Report()); n > explainMaxReport+200 {
		t.Errorf("report of %d bytes, cap %d", n, explainMaxReport)
	}
}

func TestExplainDemo(t *testing.T) {
	alert, _ := json.Marshal(map[string]string{"source": amTestSource, "fingerprint": "b2d4f6a8c0e2f4a6"})

	for kind, subject := range map[string]string{
		explainPod:     `{"namespace":"demo","name":"worker-6f4b8-uvwxy"}`,
		explainAlert:   string(alert),
		explainFinding: `{"finding":{"kind":"podCrashLoop","severity":"critical","namespace":"demo","name":"worker-6f4b8-uvwxy"},"text":"A pod keeps crashing"}`,
	} {
		for _, demo := range explainDemos(t) {
			d, err := CollectExplanation(demo.config, demo.context, "", kind, subject, false)
			if err != nil {
				t.Fatalf("%s on %s: %v", kind, demo.context, err)
			}

			if report := d.Report(); !strings.Contains(report, "worker-6f4b8-uvwxy") || !strings.Contains(report, "BackOff") {
				t.Errorf("%s demo report:\n%s", kind, report)
			}
		}
	}
}

func explainDemos(t *testing.T) []struct{ config, context string } {
	t.Helper()

	talos, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	kube, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	return []struct{ config, context string }{{talos, ""}, {kube, demoKubeContext}}
}

func TestExplainPrompts(t *testing.T) {
	d := &Diagnosis{kind: diagnosisExplain}

	big, small := d.systemPromptFor(aiAnthropic, "fr"), d.systemPromptFor(aiDevice, "fr")
	if big == small || !strings.Contains(small, "French") || !strings.Contains(big, "under 180 words") {
		t.Errorf("prompts:\n%s\n---\n%s", big, small)
	}

	if !strings.Contains(d.Prompt("en", "the pod"), "<report>") {
		t.Error("Prompt lacks the report")
	}
}
