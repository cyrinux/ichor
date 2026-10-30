package ichorgo

import (
	"net/http"
	"strings"
	"testing"
)

// namedDiagnosis has namespaces and pods that name a customer, in every place a report states
// them. All of it invented.
func namedDiagnosis() diagnosisData {
	data := sampleDiagnosis()

	data.Nodes[1].IdlePods = []string{
		"acme-billing/invoices-7d9f-x2x4z (acme-invoices exited)",
		"kube-system/coredns-abc (coredns exited)",
		"and 4 more",
	}
	data.Nodes[1].Logs = []serviceLogTail{{Service: "kubelet", Lines: []string{
		`"Error syncing pod" pod="acme-payments/ledger-0" containerName="acme-ledger" err="crash", sent Bearer abc123def456`,
		"acme-billing/invoices-7d9f-x2x4z restarted",
	}}}
	data.Events = []nodeEvent{{Node: "192.0.2.20", Kind: "service", Subject: "etcd", Message: `pod="acme-eu/cache-1" gone`, Severity: "error"}}
	data.GitOps = &gitopsState{
		Argo: &argoStatus{Apps: []argoApp{{
			Namespace: "argocd", Name: "acme-portal", Level: healthCritical, Health: "Degraded",
			Destination:   argoDest{Namespace: "acme-web"},
			Operation:     &argoOperation{Phase: "Failed", Failed: []argoResult{{Kind: "Secret", Namespace: "acme-web", Name: "portal-creds"}}},
			UnhealthyPods: []kubePod{{Namespace: "acme-web", Name: "portal-1"}},
		}}},
		Flux: &fluxStatus{Apps: []fluxApp{
			{Kind: "Kustomization", Namespace: "flux-system", Name: "flux-system", Level: healthCritical},
			{Kind: "HelmRelease", Namespace: "acme-data", Name: "acme-redis", Level: healthCritical},
		}},
	}

	return data
}

func namedAnonymized(data diagnosisData) *Diagnosis {
	d := &Diagnosis{report: renderDiagnosis(data)}
	d.applyMask("", true, data)

	return d
}

func TestAnonymizedReportHidesKubernetesNames(t *testing.T) {
	d := namedAnonymized(namedDiagnosis())

	for _, private := range []string{
		"acme", "invoices", "x2x4z", "ledger", "cache-1", "portal", "abc123", "coredns-abc", "pvc-aaaa",
	} {
		if strings.Contains(d.Report(), private) {
			t.Errorf("anonymized report shows %q:\n%s", private, d.Report())
		}
	}

	for _, want := range []string{
		"kube-system/",            // identifies nothing
		"coredns exited",          // a kept namespace's container
		"flux-system/flux-system", // kept, not matched in part
		"[redacted]",              // the scrub ran first
		"ETCD\n",                  // a section title is not a name
		"1 secrets-like strings redacted",
		"ns-1",
		"pod-1",
		"kube-scheduler-", // Kubernetes' own name
	} {
		if !strings.Contains(d.Report(), want) {
			t.Errorf("anonymized report lacks %q:\n%s", want, d.Report())
		}
	}
}

// Without anonymize names stay; the secrets go in both modes.
func TestRealNamesReportStillScrubs(t *testing.T) {
	data := namedDiagnosis()
	d := &Diagnosis{report: renderDiagnosis(data)}
	d.applyMask("", false, data)

	if !strings.Contains(d.Report(), "acme-billing/invoices-7d9f-x2x4z") || strings.Contains(d.Report(), "abc123") {
		t.Fatalf("report:\n%s", d.Report())
	}
}

func TestNamesAvoidWhatTheReportAlreadySays(t *testing.T) {
	data := diagnosisData{Nodes: []nodeDiagnosis{{IdlePods: []string{"acme-billing/invoices-1 (sidecar exited)"}}}}

	names := newDiagnosisNames(data, "upstream ns-1 and pod-1 and ctr-1 are someone else's")

	got := names.mask("acme-billing/invoices-1 sidecar")
	if got != "ns-2/pod-2 ctr-2" {
		t.Fatalf("masked: %q", got)
	}

	if back := names.reveal(got + " ns-1 pod-20"); back != "acme-billing/invoices-1 sidecar ns-1 pod-20" {
		t.Fatalf("revealed: %q", back)
	}
}

// What leaves is placeholders; what shows has the real names, whatever the chunks' boundaries.
func TestDiagnosisRoundTripOfKubernetesNames(t *testing.T) {
	d := namedAnonymized(namedDiagnosis())

	ns, pod := d.names.reals["acme-billing"], d.names.reals["invoices-7d9f-x2x4z"]
	if ns == "" || pod == "" {
		t.Fatalf("not learned: %+v", d.names.reals)
	}

	cut := len(ns) - 1
	srv, call := fakeProvider(t, http.StatusOK, "text/event-stream", openAIStream("stop",
		"Look at kubectl -n "+ns[:cut], ns[cut:]+" logs "+pod[:2], pod[2:]+"."))

	l, errMessage := askAndWait(t, d, "openai", testAPIKey, srv.URL, "is acme-billing down?")
	if errMessage != "" {
		t.Fatal(errMessage)
	}

	want := "Look at kubectl -n acme-billing logs invoices-7d9f-x2x4z."
	if got := l.answers[len(l.answers)-1]; got != want {
		t.Fatalf("answer: %q", got)
	}

	sent := call.body["messages"].([]any)[1].(map[string]any)["content"].(string)
	if strings.Contains(sent, "acme") || !strings.Contains(sent, "<operator_note>\nis "+ns+" down?") {
		t.Fatalf("request text:\n%s", sent)
	}

	// The note may use the real name or the placeholder read in the report.
	if got := d.maskNote(pod + " and invoices-7d9f-x2x4z"); got != pod+" and "+pod {
		t.Fatalf("note: %q", got)
	}
}

// Screenshot mode has its own mask, yet an anonymized report still hides the names.
func TestDiagnosisAnonymizedInScreenshotMode(t *testing.T) {
	enableMask(t, "")

	data := namedDiagnosis()
	d := &Diagnosis{report: renderDiagnosis(data)}
	d.screenshotMode, d.screenshotResets = privacy.state()
	d.applyMask("", true, data)

	if d.mask != privacy || strings.Contains(d.Report(), "acme-billing") {
		t.Fatalf("report:\n%s", d.Report())
	}

	if got := d.reveal(d.names.reals["acme-billing"]); got != "acme-billing" {
		t.Fatalf("reveal: %q", got)
	}
}
