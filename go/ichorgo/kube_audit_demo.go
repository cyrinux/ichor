package ichorgo

import "time"

// demoAudit is a homelab where a metrics exporter lists every pod in a loop and is being
// throttled, next to the usual small mistakes: a missing RBAC rule, a removed API still
// listed, an Argo CD app flapping between two writers.
func demoAudit(minutes int) auditReport {
	seconds := float64(minutes * 60)
	to := time.Now().Add(-4 * time.Second)

	exporter := describeActor("system:serviceaccount:monitoring:pod-exporter", "pod-exporter", "")
	jellyfin := describeActor("system:serviceaccount:media:jellyfin", "jellyfin", "")
	ksm := describeActor("system:serviceaccount:monitoring:kube-state-metrics", "kube-state-metrics", "")
	argo := describeActor("system:serviceaccount:argocd:argocd-application-controller", "argocd-application-controller", "")
	hass := describeActor("system:serviceaccount:home:home-assistant", "home-assistant", "")
	backup := describeActor("system:serviceaccount:tools:it-backup", "restic", "")
	admin := describeActor("admin", "kubectl", "")
	kcm := describeActor("system:kube-controller-manager", "kube-controller-manager", "")
	node := describeActor("system:node:demo-worker-1", "kubelet", "")
	stale := describeActor("", "python-requests", "192.0.2.40")

	f := func(kind, severity string, actor auditActor, count int, verb, resource, ns, name string, value float64) auditFinding {
		return auditFinding{Kind: kind, Severity: severity, Actor: actor, Count: count, Rate: float64(count) / seconds,
			Verb: verb, Resource: resource, Namespace: ns, Name: name, Value: value}
	}

	pods := int(seconds / 2)
	total := pods*100/34 + 1

	findings := []auditFinding{
		f(findThrottled, sevCritical, exporter, 42, "", "", "", "", 0),
		f(findListLoop, sevCritical, exporter, pods, "list", "pods", "", "", 2),
		f(findHotClient, sevInfo, exporter, pods+42, "list", "pods", "", "", 34),
		f(findHotObject, sevWarning, argo, int(seconds/3), "update", "applications", "argocd", "immich", 3),
		f(findUnauthorized, sevWarning, stale, int(seconds/6), "", "", "", "", 0),
		f(findForbidden, sevWarning, jellyfin, int(seconds/10), "list", "configmaps", "media", "", 0),
		f(findMissingAPI, sevWarning, ksm, int(seconds/30), "list", "podsecuritypolicies", "", "", 0),
		f(findWatchChurn, sevWarning, hass, int(seconds/20), "watch", "pods", "home", "", 20),
		f(findSlow, sevWarning, admin, 12, "list", "events", "", "", 3240),
		f(findMissingObj, sevInfo, backup, int(seconds/15), "get", "secrets", "tools", "restic-repository", 0),
		{Kind: findStaleLog, Severity: sevCritical, Name: "192.0.2.12", Value: 3 * 3600},
	}

	row := func(actor auditActor, requests, errors, throttled int, latency float64, verb, resource string, top int) auditActorRow {
		return auditActorRow{Actor: actor, Requests: requests, Rate: float64(requests) / seconds, Share: float64(requests) / float64(total),
			Errors: errors, Throttled: throttled, LatencyMs: latency, TopVerb: verb, TopResource: resource, TopCount: top}
	}

	return auditReport{
		Nodes: []auditNodeRead{
			{Node: "192.0.2.10", Bytes: 6_400_000, Events: 512_000, Last: to.UnixMilli()},
			{Node: "192.0.2.11", Bytes: 5_900_000, Events: 471_000, Last: to.Add(-2 * time.Second).UnixMilli()},
			{Node: "192.0.2.12", Bytes: 6_100_000, Events: 489_000, Last: to.Add(-3 * time.Hour).UnixMilli()},
		},
		From: to.Add(-time.Duration(minutes) * time.Minute).UnixMilli(), To: to.UnixMilli(), Seconds: seconds,
		Requests: total,
		Findings: findings,
		Actors: []auditActorRow{
			row(exporter, pods+42, 0, 42, 410, "list", "pods", pods),
			row(node, total/8, 0, 0, 4, "get", "nodes", total/20),
			row(kcm, total/9, 0, 0, 6, "update", "leases", total/30),
			row(argo, total/10, 2, 0, 12, "update", "applications", int(seconds/3)),
			row(hass, int(seconds/20)+40, 0, 0, 3, "watch", "pods", int(seconds/20)),
			row(jellyfin, int(seconds/10), int(seconds/10), 0, 2, "list", "configmaps", int(seconds/10)),
			row(ksm, int(seconds/30)+300, int(seconds/30), 0, 9, "list", "podsecuritypolicies", int(seconds/30)),
			row(backup, int(seconds/15), 0, 0, 2, "get", "secrets", int(seconds/15)),
			row(admin, 30, 0, 0, 1400, "list", "events", 12),
		},
	}
}
