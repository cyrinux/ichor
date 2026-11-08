package ichorgo

import (
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"
)

// All of it invented: a customer's names in every place the Kubernetes sections state them, and
// in the free text of messages, which is what no structured field covers.
var kubeDiagnosisAnswers = map[string]string{
	"GET /version": `{"gitVersion":"v1.34.1"}`,
	"GET /apis":    `{"groups":[]}`,
	"GET /api/v1/nodes": `{"items":[
		{"metadata":{"name":"acme-node-1.acme.example","creationTimestamp":"2026-01-01T00:00:00Z"},
		 "status":{"conditions":[{"type":"Ready","status":"True"},{"type":"MemoryPressure","status":"True"}],
		           "nodeInfo":{"kubeletVersion":"v1.34.1"}}}]}`,
	"GET /apis/apps/v1/deployments": `{"items":[
		{"metadata":{"namespace":"acme-billing","name":"invoices"},
		 "spec":{"replicas":3,"template":{"spec":{"containers":[{"image":"registry.acme.example/billing/invoices:1"}]}}},
		 "status":{"readyReplicas":1}},
		{"metadata":{"namespace":"acme-billing","name":"archived"},
		 "spec":{"replicas":0,"template":{"spec":{"containers":[{"image":"registry.acme.example/billing/archived:1"}]}}},
		 "status":{}}]}`,
	"GET /apis/apps/v1/statefulsets": `{"items":[]}`,
	"GET /apis/apps/v1/daemonsets":   `{"items":[]}`,
	"GET /api/v1/pods": `{"items":[
		{"metadata":{"namespace":"acme-billing","name":"invoices-7d9f-x2x4z","ownerReferences":[{"kind":"ReplicaSet","name":"invoices-7d9f"}]},
		 "spec":{"nodeName":"acme-node-1.acme.example","containers":[{"name":"acme-api","image":"registry.acme.example/billing/invoices:1"}]},
		 "status":{"phase":"Running","containerStatuses":[{"name":"acme-api","restartCount":14,
		   "state":{"waiting":{"reason":"CrashLoopBackOff","message":"back-off 5m0s restarting failed container=acme-api pod=invoices-7d9f-x2x4z_acme-billing"}}}]}},
		{"metadata":{"namespace":"acme-web","name":"portal-0"},
		 "spec":{"containers":[{"name":"portal","image":"registry.acme.example/web/portal:2"}]},
		 "status":{"phase":"Pending"}},
		{"metadata":{"namespace":"acme-web","name":"fine-0"},
		 "spec":{"containers":[{"name":"fine","image":"nginx:1"}]},
		 "status":{"phase":"Running","containerStatuses":[{"name":"fine","ready":true,"state":{}}]}}]}`,
	"GET /api/v1/events": `{"items":[
		{"metadata":{"namespace":"acme-billing"},"involvedObject":{"kind":"Pod","namespace":"acme-billing","name":"invoices-7d9f-x2x4z"},
		 "type":"Warning","reason":"Failed","count":3,"lastTimestamp":"2026-10-31T05:00:00Z",
		 "message":"Failed to pull image \"registry.acme.example/billing/other:9\": dial tcp 192.0.2.77:443: connection refused, Bearer abc123def456"},
		{"metadata":{"namespace":"acme-web"},"involvedObject":{"kind":"Pod","namespace":"acme-web","name":"portal-0"},
		 "type":"Warning","reason":"FailedMount","count":1,"lastTimestamp":"2026-10-31T04:00:00Z",
		 "message":"MountVolume.SetUp failed for volume \"data\" : secret \"payroll\" not found, exceeded quota: acme-compute"}]}`,
}

func kubeDiagnosisFor(t *testing.T, answers map[string]string, anonymize bool) *Diagnosis {
	t.Helper()

	f := newFakeKubeAPI(t, answers)

	d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", anonymize)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func TestKubeDiagnosisReport(t *testing.T) {
	report := kubeDiagnosisFor(t, kubeDiagnosisAnswers, false).Report()

	for _, part := range []string{
		"Kubernetes cluster report",
		"NODES (1: 1 ready, 0 not ready)",
		"acme-node-1.acme.example: ready, under MemoryPressure",
		"WORKLOADS NOT READY (1)",
		"Deployment acme-billing/invoices: 1 of 3 ready",
		"Failed to pull image",
		"PODS IN TROUBLE (2)",
		"acme-billing/invoices-7d9f-x2x4z: CrashLoopBackOff, 0 of 1 ready, 14 restarts",
		"acme-web/portal-0: Pending",
		"WARNING EVENTS (2, newest first)",
		"CHECKUP FINDINGS",
		"podCrashLoop",
		"[redacted]",
		"2 secrets-like strings redacted",
	} {
		if !strings.Contains(report, part) {
			t.Errorf("report lacks %q:\n%s", part, report)
		}
	}

	// Scaled to zero on purpose, and running fine: neither is a finding.
	for _, absent := range []string{"archived", "fine-0", "abc123def456", "Talos"} {
		if strings.Contains(report, absent) {
			t.Errorf("report shows %q:\n%s", absent, report)
		}
	}
}

// Everything the new report states goes through the mask and the scrubber, the free text too.
func TestKubeDiagnosisAnonymized(t *testing.T) {
	d := kubeDiagnosisFor(t, kubeDiagnosisAnswers, true)

	for _, private := range []string{
		"acme", "invoices", "x2x4z", "portal", "payroll", "billing", "registry", "192.0.2.77", "abc123", "acme-compute",
	} {
		if strings.Contains(d.Report(), private) {
			t.Errorf("anonymized report shows %q:\n%s", private, d.Report())
		}
	}

	for _, want := range []string{
		"CrashLoopBackOff", "MemoryPressure", "Failed to pull image", "MountVolume.SetUp failed", "back-off",
		"not found", "exceeded quota", "ns-1", "pod-1", "[redacted]", "2 secrets-like strings redacted",
	} {
		if !strings.Contains(d.Report(), want) {
			t.Errorf("anonymized report lacks %q:\n%s", want, d.Report())
		}
	}

	if !d.Anonymized() {
		t.Error("not anonymized")
	}
}

// What leaves the phone is placeholders; the answer shown has the real names.
func TestKubeDiagnosisRoundTrip(t *testing.T) {
	d := kubeDiagnosisFor(t, kubeDiagnosisAnswers, true)

	ns, pod := d.names.reals["acme-billing"], d.names.reals["invoices-7d9f-x2x4z"]
	if ns == "" || pod == "" {
		t.Fatalf("not learned: %+v", d.names.reals)
	}

	srv, call := fakeProvider(t, http.StatusOK, "text/event-stream", openAIStream("stop", "Look at kubectl -n "+ns+" logs "+pod+"."))

	l, errMessage := askAndWait(t, d, "openai", testAPIKey, srv.URL, "is acme-billing down?")
	if errMessage != "" {
		t.Fatal(errMessage)
	}

	if got, want := l.answers[len(l.answers)-1], "Look at kubectl -n acme-billing logs invoices-7d9f-x2x4z."; got != want {
		t.Fatalf("answer: %q", got)
	}

	messages := call.body["messages"].([]any)
	system := messages[0].(map[string]any)["content"].(string)
	sent := messages[1].(map[string]any)["content"].(string)

	if strings.Contains(sent, "acme") || strings.Contains(sent, "registry.") || strings.Contains(sent, "192.0.2.77") {
		t.Fatalf("request text:\n%s", sent)
	}

	if !strings.Contains(system, "Kubernetes API") || strings.Contains(system, "Talos") {
		t.Fatalf("system prompt:\n%s", system)
	}
}

// A line clipped in the middle of a name no longer matches the name: it is still hidden.
func TestKubeDiagnosisClippedNameStaysHidden(t *testing.T) {
	long := strings.Repeat("x ", 140) + `pulling "registry.acme.example/billing/invoices-with-a-long-name:1"`

	kube := kubeDiagnosisData{At: time.Now(), Events: []kubeEvent{{Kind: "Pod", Namespace: "acme-web", Name: "portal-0", Message: long}}}
	data := diagnosisData{At: kube.At, Kube: &kube, KubeOnly: true}

	d := &Diagnosis{report: renderKubeDiagnosis(kube)}
	d.applyMask("", true, data)

	if strings.Contains(d.Report(), "acme") || strings.Contains(d.Report(), "regist") {
		t.Fatalf("report:\n%s", d.Report())
	}
}

// A name glued to a known one: masked in part, it would no longer match when learned as it reads.
func TestKubeDiagnosisNameAroundAKnownName(t *testing.T) {
	kube := kubeDiagnosisData{At: time.Now(), Events: []kubeEvent{{
		Kind: "Ingress", Namespace: "acme-web", Name: "portal",
		Message: "certificate for vault.acme-web.example, secret portal-tls, no pod ready, secret data missing",
	}}}

	d := &Diagnosis{report: renderKubeDiagnosis(kube)}
	d.applyMask("", true, diagnosisData{At: kube.At, Kube: &kube, KubeOnly: true})

	for _, private := range []string{"acme", "vault", "portal"} {
		if strings.Contains(d.Report(), private) {
			t.Errorf("report shows %q:\n%s", private, d.Report())
		}
	}

	// The ordinary words of a message stay readable.
	if !strings.Contains(d.Report(), "no pod ready, secret data missing") {
		t.Errorf("report:\n%s", d.Report())
	}

	if got := d.reveal(d.names.reals["vault.acme-web.example"]); got != "vault.acme-web.example" {
		t.Errorf("reveal: %q", got)
	}
}

func TestKubeDiagnosisSweepWords(t *testing.T) {
	n := newDiagnosisNames(diagnosisData{}, "")
	n.keep = append(n.keep, "node-1")

	for _, safe := range []string{
		"back-off", "5m0s", "v1.34.1", "2026-10-31", "137", "Deployment", "CrashLoopBackOff", "Init:CrashLoopBackOff",
		"MountVolume.SetUp", "node-1", "ns-1/pod-2", "storage.k8s.io", "node-role.kubernetes.io/control-plane", "failed",
	} {
		if !n.sweepSafe(safe, false) {
			t.Errorf("%q is hidden", safe)
		}
	}

	for _, private := range []string{
		"acme-compute", "registry.acme.example/billing/api:1", "acme/billing", "web1", "http://acme.example/health", "Deployment/acme",
	} {
		if n.sweepSafe(private, false) {
			t.Errorf("%q stays", private)
		}
	}

	if n.sweepSafe("payroll", true) || !n.sweepSafe("payroll", false) {
		t.Error("a plain word is only suspect once something marks it as a name")
	}
}

// Credentials of a single namespace: each section refused says so, and the report still comes.
func TestKubeDiagnosisDenied(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /version": `{"gitVersion":"v1.34.1"}`, "GET /apis": `{"groups":[]}`})

	for _, path := range []string{
		"/api/v1/nodes", "/api/v1/pods", "/api/v1/events",
		"/apis/apps/v1/deployments", "/apis/apps/v1/statefulsets", "/apis/apps/v1/daemonsets",
	} {
		f.answerWith("GET "+path, http.StatusForbidden, `{"kind":"Status","code":403,"message":"forbidden"}`)
	}

	d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"left out: the credentials may not list nodes",
		"left out: the credentials may not list pods",
		"left out: the credentials may not list events",
		"left out: the credentials may not list Deployments, StatefulSets and DaemonSets",
	} {
		if !strings.Contains(d.Report(), want) {
			t.Errorf("report lacks %q:\n%s", want, d.Report())
		}
	}
}

// One kind refused: the others are shown, and the section says it is not whole.
func TestKubeDiagnosisOneKindDenied(t *testing.T) {
	f := newFakeKubeAPI(t, maps.Clone(kubeDiagnosisAnswers))
	f.answerWith("GET /apis/apps/v1/daemonsets", http.StatusForbidden, `{"kind":"Status","code":403,"message":"forbidden"}`)

	d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"not complete: the credentials may not list daemonsets", "Deployment acme-billing/invoices: 1 of 3 ready"} {
		if !strings.Contains(d.Report(), want) {
			t.Errorf("report lacks %q:\n%s", want, d.Report())
		}
	}
}

// An API server that answers nothing is an error to show, not a report of nothing.
func TestKubeDiagnosisNoAnswerFails(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /version": `{"gitVersion":"v1.34.1"}`})

	for _, path := range []string{
		"/api/v1/nodes", "/api/v1/pods", "/api/v1/events",
		"/apis/apps/v1/deployments", "/apis/apps/v1/statefulsets", "/apis/apps/v1/daemonsets",
	} {
		f.answerWith("GET "+path, http.StatusInternalServerError, `{"kind":"Status","code":500,"message":"down"}`)
	}

	if _, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", false); err == nil {
		t.Fatal("no error")
	}
}

func TestKubeDiagnosisPagesAreBounded(t *testing.T) {
	calls := 0
	f := newFakeKubeAPI(t, nil)
	f.answerQuery = func(path, _ string) (string, bool) {
		if path != "/api/v1/pods" {
			return "", false
		}

		calls++

		return `{"metadata":{"continue":"more"},"items":[]}`, true
	}

	k := openFakeKube(t, f)

	_, partial, err := readTroubledPods(t.Context(), k)
	if err != nil || !partial || calls != diagnosisKubePodPages {
		t.Fatalf("partial %v, %d pages, err %v", partial, calls, err)
	}
}

// The report is cut before the prompt, and a name cut there is hidden like any other.
func TestKubeDiagnosisReportIsCapped(t *testing.T) {
	kube := kubeDiagnosisData{At: time.Now()}
	for range 2000 {
		kube.Findings = append(kube.Findings, checkupFinding{Kind: findPodFailed, Severity: sevWarning, Message: strings.Repeat("y ", 100)})
	}

	report := renderKubeDiagnosis(kube)
	if len(report) > maxKubeReportBytes+200 || !strings.Contains(report, "[The report was cut here") {
		t.Fatalf("%d bytes", len(report))
	}
}

func TestKubeDiagnosisDemo(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)

	d, err := CollectDiagnosis(cfg, demoKubeContext, "", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, part := range []string{
		"Kubernetes cluster report", "demo-worker-2: ready, under MemoryPressure", "Deployment demo/worker: 1 of 2 ready",
		"Deployment home/mealie: 0 of 1 ready", "demo/worker-6f4b8-uvwxy: CrashLoopBackOff", "demo/reports-5b7c9-xkq2p: ImagePullBackOff",
		"Back-off restarting failed container", "podCrashLoop", "GITOPS",
	} {
		if !strings.Contains(d.Report(), part) {
			t.Errorf("demo report lacks %q:\n%s", part, d.Report())
		}
	}

	if strings.Contains(d.Report(), "stirling-pdf") {
		t.Errorf("a stopped workload is listed:\n%s", d.Report())
	}

	masked, err := CollectDiagnosis(cfg, demoKubeContext, "", true)
	if err != nil {
		t.Fatal(err)
	}

	for _, private := range []string{"demo-worker", "worker-6f4b8", "reports-5b7c9", "mealie", "demo/"} {
		if strings.Contains(masked.Report(), private) {
			t.Errorf("anonymized demo shows %q:\n%s", private, masked.Report())
		}
	}
}

// The Talos report's sections are read with os:admin only, and a refusal is a note.
func TestCollectKubeTrouble(t *testing.T) {
	f := newFakeKubeAPI(t, kubeDiagnosisAnswers)
	target := kubeTarget{f.kubeconfigFor(f.URL), "admin@test", ""}

	if got := collectKubeTrouble(t.Context(), target, []string{"os:reader"}); got != nil {
		t.Fatalf("without os:admin: %+v", got)
	}

	got := collectKubeTrouble(t.Context(), target, []string{adminRole})
	if got == nil || len(got.Workloads) != 1 || got.Workloads[0].Name != "invoices" || got.Workloads[0].ready != 1 || len(got.Pods) != 2 {
		t.Fatalf("with os:admin: %+v", got)
	}

	denied := newFakeKubeAPI(t, map[string]string{})
	denied.answerWith("GET /api/v1/pods", http.StatusForbidden, `{"kind":"Status","code":403,"message":"forbidden"}`)

	got = collectKubeTrouble(t.Context(), kubeTarget{denied.kubeconfigFor(denied.URL), "admin@test", ""}, []string{adminRole})
	if got == nil || got.PodsNote != "left out: the credentials may not list pods" {
		t.Fatalf("denied: %+v", got)
	}
}

// A Talos report gains the two sections, and they go through the mask and the scrubber too.
func TestTalosReportGainsKubeSections(t *testing.T) {
	data := namedDiagnosis()
	data.Kube = &kubeDiagnosisData{
		Workloads: []kubeTroubledWorkload{{
			idleWorkload: idleWorkload{
				inventoryWorkload: inventoryWorkload{Kind: "Deployment", Namespace: "acme-shop", Name: "checkout", Desired: 3, Reason: `pulling "registry.acme.example/shop/checkout:1", token=hunter2hunter2`},
				images:            []string{"registry.acme.example/shop/checkout:1"}, ready: 1,
			},
		}},
		Pods: []kubeTroubledPod{{kubePod: kubePod{Namespace: "acme-shop", Name: "checkout-7d9f-abcde", Status: "ImagePullBackOff", Containers: 1}}},
	}

	plain := renderDiagnosis(data)
	for _, part := range []string{
		"WORKLOADS NOT READY (1)", "Deployment acme-shop/checkout: 1 of 3 ready", "PODS IN TROUBLE (1)",
		"acme-shop/checkout-7d9f-abcde: ImagePullBackOff", "ETCD\n", "RECENT WARNING AND ERROR EVENTS",
	} {
		if !strings.Contains(plain, part) {
			t.Errorf("report lacks %q:\n%s", part, plain)
		}
	}

	if strings.Contains(plain, "hunter2") {
		t.Errorf("a token stays:\n%s", plain)
	}

	d := namedAnonymized(data)
	for _, private := range []string{"acme", "checkout", "registry."} {
		if strings.Contains(d.Report(), private) {
			t.Errorf("anonymized report shows %q:\n%s", private, d.Report())
		}
	}

	if !strings.Contains(d.Report(), "WORKLOADS NOT READY (1)") || !strings.Contains(d.Report(), "PODS IN TROUBLE (1)") {
		t.Errorf("the sections are gone:\n%s", d.Report())
	}
}
