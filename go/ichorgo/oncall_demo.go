package ichorgo

import "time"

// demoOnCall is the demo cluster's incidents: the full filesystem page, still ringing, on the
// demo's critical alert, and the crash-looping worker someone is already on.
func demoOnCall(now time.Time) []onCallIncident {
	return []onCallIncident{
		{
			ID: "PDEMO01", Number: 1042, Title: "[FIRING:1] NodeFilesystemAlmostOutOfSpace (demo-worker-1)",
			Status: onCallStatusTriggered, Urgency: "high", CreatedAt: now.Add(-45 * time.Minute).UnixMilli(),
			URL: "https://demo.pagerduty.invalid/incidents/PDEMO01", Service: "Demo platform", Assignees: []string{"Sam Sample"},
			Details: []string{"Labels:\n - alertname = NodeFilesystemAlmostOutOfSpace\n - device = /dev/sda5\n - instance = demo-worker-1\n" +
				" - job = node-exporter\n - mountpoint = /var\n - namespace = monitoring\n - severity = critical\nAnnotations:\n"},
		},
		{
			ID: "PDEMO02", Number: 1039, Title: "[FIRING:1] KubePodCrashLooping (demo/worker)",
			Status: onCallStatusAcknowledged, Urgency: "low", CreatedAt: now.Add(-3 * time.Hour).UnixMilli(),
			URL: "https://demo.pagerduty.invalid/incidents/PDEMO02", Service: "Demo workloads", Assignees: []string{"Alex Example"}, AcknowledgedBy: "Alex Example",
		},
	}
}
