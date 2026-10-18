package relay

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSeverity(t *testing.T) {
	for label, want := range map[string]string{
		"critical": SeverityCritical, " Page ": SeverityCritical, "warn": SeverityWarning,
		"error": SeverityWarning, "info": SeverityInfo, "none": SeverityInfo, "": SeverityOther, "p1": SeverityOther,
	} {
		if got := Severity(label); got != want {
			t.Errorf("Severity(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestWebhookAlertsOrderAndFingerprint(t *testing.T) {
	at := func(m int) time.Time { return testNow.Add(time.Duration(m) * time.Minute) }
	msg := WebhookMessage{Alerts: []WebhookAlert{
		{Status: "resolved", Labels: map[string]string{"alertname": "A", "severity": "critical"}, StartsAt: at(1), Fingerprint: "a"},
		{Status: "firing", Labels: map[string]string{"alertname": "B", "severity": "info"}, StartsAt: at(2), Fingerprint: "b"},
		{Status: "firing", Labels: map[string]string{"alertname": "C", "severity": "critical"}, StartsAt: at(3), Fingerprint: "c"},
		{Status: "firing", Labels: map[string]string{"alertname": "D", "severity": "critical"}, StartsAt: at(4)},
	}}

	alerts := webhookAlerts(msg)
	var names []string
	for _, a := range alerts {
		names = append(names, a.Alertname)
	}
	if got := strings.Join(names, ","); got != "D,C,A,B" {
		t.Errorf("order %s, want D,C,A,B (critical firing newest first, critical resolved, then info)", got)
	}
	if alerts[0].Fingerprint == "" || alerts[0].Fingerprint != pushAlert(msg.Alerts[3]).Fingerprint {
		t.Errorf("derived fingerprint %q is empty or unstable", alerts[0].Fingerprint)
	}
}

func TestBuildPayloadTruncates(t *testing.T) {
	var msg WebhookMessage
	for i := range 200 {
		sev := []string{"info", "warning", "critical"}[i%3]
		msg.Alerts = append(msg.Alerts, WebhookAlert{
			Status:      "firing",
			Labels:      map[string]string{"alertname": fmt.Sprintf("Alert%03d", i), "severity": sev, "namespace": "shop"},
			Annotations: map[string]string{"summary": strings.Repeat("s", 60)},
			Fingerprint: fmt.Sprintf("%016d", i),
		})
	}

	buf, err := buildPayload("fp", webhookAlerts(msg), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(buf) > maxPlaintext {
		t.Fatalf("payload is %d bytes, over %d", len(buf), maxPlaintext)
	}

	var p Payload
	if err := json.Unmarshal(buf, &p); err != nil {
		t.Fatal(err)
	}
	if !p.Truncated || p.Omitted != 200-len(p.Alerts) || len(p.Alerts) == 0 {
		t.Errorf("truncated %v, omitted %d, kept %d", p.Truncated, p.Omitted, len(p.Alerts))
	}
	for _, a := range p.Alerts {
		if a.Severity != SeverityCritical {
			t.Fatalf("kept a %s alert while critical ones were dropped", a.Severity)
		}
	}

	// One more alert would not fit: the cut is the largest that does.
	more := p
	more.Alerts = webhookAlerts(msg)[:len(p.Alerts)+1]
	more.Omitted--
	if bigger, _ := json.Marshal(more); len(bigger) <= maxPlaintext {
		t.Errorf("%d alerts would still fit (%d bytes)", len(more.Alerts), len(bigger))
	}
}

func TestDedupeWindow(t *testing.T) {
	now := testNow
	d := newDedupe(func() time.Time { return now })
	alerts := []PushAlert{{Fingerprint: "a", Status: StatusFiring}, {Fingerprint: "b", Status: StatusFiring}}

	if got := d.claim("p", alerts); len(got) != 2 {
		t.Fatalf("first claim: %d", len(got))
	}
	if got := d.claim("p", alerts); len(got) != 0 {
		t.Errorf("repeat within the window: %d", len(got))
	}
	if got := d.claim("q", alerts); len(got) != 2 {
		t.Errorf("another device: %d", len(got))
	}

	d.release("p", alerts[:1])
	if got := d.claim("p", alerts); len(got) != 1 || got[0].Fingerprint != "a" {
		t.Errorf("after release: %+v", got)
	}

	now = now.Add(dedupeWindow)
	if got := d.claim("p", alerts); len(got) != 2 {
		t.Errorf("after the window: %d", len(got))
	}
}

func TestNtfyBodyIsCut(t *testing.T) {
	var alerts []PushAlert
	for i := range 300 {
		alerts = append(alerts, PushAlert{Alertname: fmt.Sprintf("SomeRatherLongAlertName%03d", i), Severity: SeverityWarning, Status: StatusFiring, Namespace: "shop"})
	}

	body := ntfyBody(Device{}, alerts)
	if len(body) > maxNtfyBody {
		t.Errorf("body is %d bytes", len(body))
	}
	if !strings.HasSuffix(body, " more") || !strings.Contains(body, "… and ") {
		t.Errorf("body does not end with the count of the rest: %q", body[len(body)-40:])
	}
	if title := ntfyTitle(Device{Label: "homelab"}, []PushAlert{{Status: StatusResolved}}); title != "Ichor · homelab: 1 resolved" {
		t.Errorf("resolved title %q", title)
	}
}
