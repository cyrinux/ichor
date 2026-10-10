package ichorgo

import (
	"regexp"
	"slices"
	"time"
)

const demoAMSilenceID = "5b0d7c3e-9a41-4f2e-8c6d-1e2f3a4b5c6d"

func demoAMDiscovery() promDiscovery {
	return promDiscovery{Sources: []promSource{
		{Mode: promModeProxy, Kind: "alertmanager", Namespace: "monitoring", Service: "alertmanager-operated", Port: 9093},
	}}
}

// demoAMAlerts are the demo cluster's alerts, matching its workloads: a node filling up
// (critical), the crash-looping worker (warning, twice), the always-firing Watchdog (info)
// and CoreDNS throttling, silenced by the demo silence.
func demoAMAlerts(now time.Time) []amAlert {
	alert := func(fp string, age time.Duration, receivers []string, labels, annotations map[string]string) amAlert {
		a := amAPIAlert{Fingerprint: fp, Labels: labels, Annotations: annotations, GeneratorURL: "http://prometheus.demo.invalid/graph"}
		a.StartsAt = now.Add(-age).UTC().Format(time.RFC3339)
		a.EndsAt = now.Add(4 * time.Minute).UTC().Format(time.RFC3339)
		a.UpdatedAt = now.Add(-30 * time.Second).UTC().Format(time.RFC3339)
		a.Status.State = amStateActive

		for _, r := range receivers {
			a.Receivers = append(a.Receivers, struct {
				Name string `json:"name"`
			}{r})
		}

		return a.toAlert()
	}

	const runbooks = "https://runbooks.prometheus-operator.dev/runbooks/"

	alerts := []amAlert{
		alert("a1c3e5f7a9b1c3d5", 47*time.Minute, []string{"oncall-pager", "team-chat"},
			map[string]string{"alertname": "NodeFilesystemAlmostOutOfSpace", "severity": "critical", "instance": "demo-worker-1", "mountpoint": "/var", "device": "/dev/sda5", "job": "node-exporter", "namespace": "monitoring"},
			map[string]string{"summary": "Filesystem has less than 5% space left.", "description": "Filesystem on /dev/sda5 at demo-worker-1 has only 3.8% available space left.", "runbook_url": runbooks + "node/nodefilesystemalmostoutofspace"}),
		alert("b2d4f6a8c0e2f4a6", 3*time.Hour, []string{"team-chat"},
			map[string]string{"alertname": "KubePodCrashLooping", "severity": "warning", "namespace": "demo", "pod": "worker-6f4b8-uvwxy", "container": "worker", "job": "kube-state-metrics"},
			map[string]string{"summary": "Pod is crash looping.", "description": "Pod demo/worker-6f4b8-uvwxy (worker) is in waiting state (reason: \"CrashLoopBackOff\").", "runbook_url": runbooks + "kubernetes/kubepodcrashlooping"}),
		alert("c3e5a7b9d1f3a5c7", 3*time.Hour+10*time.Minute, []string{"team-chat"},
			map[string]string{"alertname": "KubeDeploymentReplicasMismatch", "severity": "warning", "namespace": "demo", "deployment": "worker", "job": "kube-state-metrics"},
			map[string]string{"summary": "Deployment has not matched the expected number of replicas.", "description": "Deployment demo/worker has not matched the expected number of replicas for longer than 15 minutes.", "runbook_url": runbooks + "kubernetes/kubedeploymentreplicasmismatch"}),
		alert("d4f6b8c0e2a4b6d8", 72*time.Hour, []string{"null"},
			map[string]string{"alertname": "Watchdog", "severity": "none"},
			map[string]string{"summary": "An alert that should always be firing to certify that Alertmanager is working properly.", "description": "This is an alert meant to ensure that the entire alerting pipeline is functional."}),
	}

	throttled := alert("e5a7c9d1f3b5c7e9", 26*time.Hour, []string{"team-chat"},
		map[string]string{"alertname": "CPUThrottlingHigh", "severity": "info", "namespace": "kube-system", "pod": "coredns-5c6b7-aaaaa", "container": "coredns"},
		map[string]string{"summary": "Processes experience elevated CPU throttling.", "description": "27.4% throttling of CPU in namespace kube-system for container coredns in pod coredns-5c6b7-aaaaa.", "runbook_url": runbooks + "kubernetes/cputhrottlinghigh"})
	throttled.State, throttled.SilencedBy = amStateSuppressed, []string{demoAMSilenceID}

	return append(alerts, throttled)
}

// demoAMSilences is the active silence muting CoreDNS throttling, plus, withExpired, one
// that ended yesterday.
func demoAMSilences(now time.Time, withExpired bool) []amSilence {
	out := []amSilence{{
		ID: demoAMSilenceID, State: amStateActive, CreatedBy: amCreatedBy, Comment: "CoreDNS limits are being raised, see the platform board",
		Matchers: []amMatcher{{Name: "alertname", Value: "CPUThrottlingHigh", IsEqual: true}, {Name: "namespace", Value: "kube-system", IsEqual: true}},
		StartsAt: now.Add(-20 * time.Hour).UnixMilli(), EndsAt: now.Add(28 * time.Hour).UnixMilli(), UpdatedAt: now.Add(-20 * time.Hour).UnixMilli(),
	}}

	if withExpired {
		out = append(out, amSilence{
			ID: "0c1d2e3f-4a5b-4c6d-8e7f-a0b1c2d3e4f5", State: amStateExpired, CreatedBy: "ops", Comment: "Planned node maintenance",
			Matchers: []amMatcher{{Name: "instance", Value: "demo-worker-.*", IsRegex: true, IsEqual: true}},
			StartsAt: now.Add(-50 * time.Hour).UnixMilli(), EndsAt: now.Add(-26 * time.Hour).UnixMilli(), UpdatedAt: now.Add(-26 * time.Hour).UnixMilli(),
		})
	}

	return out
}

// amAlertFilter applies GET /api/v2/alerts' filters, for the demo, as Alertmanager does.
type amAlertFilter struct {
	active, silenced, inhibited bool
	receiver                    *regexp.Regexp // nil: any
	matchers                    []amMatcher
}

func (f amAlertFilter) apply(alerts []amAlert) []amAlert {
	return slices.DeleteFunc(alerts, func(a amAlert) bool { return !f.keeps(a) })
}

func (f amAlertFilter) keeps(a amAlert) bool {
	switch {
	case !f.silenced && len(a.SilencedBy) > 0, !f.inhibited && len(a.InhibitedBy) > 0:
		return false
	case !f.active && a.State != amStateSuppressed:
		return false
	case f.receiver != nil && !slices.ContainsFunc(a.Receivers, f.receiver.MatchString):
		return false
	}

	return !slices.ContainsFunc(f.matchers, func(m amMatcher) bool { return !m.matches(a.Labels) })
}
