package ichorgo

import "time"

// demoFlux is the Flux of the built-in demo cluster, in every state the app shows: a
// Kustomization applying a new revision, a HelmRelease whose upgrade failed, one waiting on
// a dependency, a suspended one, and a Git source that cannot be fetched.
func demoFlux(now time.Time) fluxStatus {
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	const rev = "main@sha1:4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"
	const oldRev = "main@sha1:91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e"

	gitRepo := &fluxRef{Kind: "GitRepository", Namespace: "flux-system", Name: "flux-system"}
	root := &fluxRef{Kind: "Kustomization", Namespace: "flux-system", Name: "flux-system"}
	infra := &fluxRef{Kind: "Kustomization", Namespace: "flux-system", Name: "infra-controllers"}
	gitURL := "ssh://git@git.example.com/homelab/fleet.git"

	ready := func(reason, message string, ago time.Duration) []fluxCondition {
		return []fluxCondition{{Type: "Ready", Status: "True", Reason: reason, Message: message, At: ms(ago)}}
	}
	res := func(group, kind, ns, name string) fluxResource {
		return fluxResource{Group: group, Kind: kind, Namespace: ns, Name: name}
	}
	ks := func(name, path string, level string, deps ...string) fluxApp {
		return fluxApp{
			Kind: "Kustomization", Namespace: "flux-system", Name: name, Level: level, Ready: "True", Reason: "ReconciliationSucceeded",
			Message: "Applied revision: " + rev, Source: gitRepo, SourceURL: gitURL, Path: path, Interval: "10m", Prune: true,
			Revision: rev, AttemptedRevision: rev, DependsOn: append([]string{}, deps...), Conditions: ready("ReconciliationSucceeded", "Applied revision: "+rev, 2*time.Hour),
			History: []fluxHistory{}, UnhealthyPods: []kubePod{}, ReconciledAt: ms(2 * time.Hour),
		}
	}
	hr := func(ns, name, chart, version string, src *fluxRef, url string, level string, hist ...fluxHistory) fluxApp {
		return fluxApp{
			Kind: "HelmRelease", Namespace: ns, Name: name, Level: level, Icon: chart, Ready: "True", Reason: "UpgradeSucceeded",
			Message: "Helm upgrade succeeded for release " + ns + "/" + name + " with chart " + chart + "@" + version,
			Owner: infra, Source: src, SourceURL: url, Chart: chart, ChartVersion: version, Interval: "30m",
			Revision: version, AttemptedRevision: version, DependsOn: []string{}, Resources: []fluxResource{}, History: hist,
			Conditions: ready("UpgradeSucceeded", "Helm upgrade succeeded", 5*24*time.Hour), UnhealthyPods: []kubePod{}, ReconciledAt: ms(5 * 24 * time.Hour),
		}
	}
	hist := func(v int, chart, app, status string, ago time.Duration) fluxHistory {
		return fluxHistory{Version: v, ChartVersion: chart, AppVersion: app, Status: status, DeployedAt: ms(ago)}
	}

	rootKs := ks("flux-system", "./clusters/homelab", healthOK)
	rootKs.Icon = "flux"
	rootKs.Resources = []fluxResource{
		res("", "Namespace", "", "flux-system"),
		res("apps", "Deployment", "flux-system", "source-controller"),
		res("apps", "Deployment", "flux-system", "kustomize-controller"),
		res("apps", "Deployment", "flux-system", "helm-controller"),
		res("source.toolkit.fluxcd.io", "GitRepository", "flux-system", "flux-system"),
		res("kustomize.toolkit.fluxcd.io", "Kustomization", "flux-system", "flux-system"),
		res("kustomize.toolkit.fluxcd.io", "Kustomization", "flux-system", "infra-controllers"),
		res("kustomize.toolkit.fluxcd.io", "Kustomization", "flux-system", "apps"),
	}

	infraKs := ks("infra-controllers", "./infrastructure/controllers", healthWarning)
	infraKs.Owner = root
	infraKs.Reconciling, infraKs.Ready, infraKs.Reason = true, "Unknown", "Progressing"
	infraKs.Message = "Reconciliation in progress"
	infraKs.Revision = oldRev
	infraKs.Conditions = []fluxCondition{
		{Type: "Reconciling", Status: "True", Reason: "Progressing", Message: "Running health checks for revision " + rev + " with a timeout of 5m0s", At: ms(40 * time.Second)},
		{Type: "Ready", Status: "Unknown", Reason: "Progressing", Message: "Reconciliation in progress", At: ms(40 * time.Second)},
	}
	infraKs.Resources = []fluxResource{
		res("", "Namespace", "", "monitoring"),
		res("", "Namespace", "", "ingress-nginx"),
		res("source.toolkit.fluxcd.io", "HelmRepository", "flux-system", "prometheus-community"),
		res("source.toolkit.fluxcd.io", "HelmRepository", "flux-system", "ingress-nginx"),
		res("helm.toolkit.fluxcd.io", "HelmRelease", "monitoring", "kube-prometheus-stack"),
		res("helm.toolkit.fluxcd.io", "HelmRelease", "ingress-nginx", "ingress-nginx"),
	}

	appsKs := ks("apps", "./apps/homelab", healthCritical, "flux-system/infra-controllers")
	appsKs.Owner = root
	appsKs.Ready, appsKs.Reason = "False", "HealthCheckFailed"
	appsKs.Message = "health check failed after 5m0s: failed early due to stalled resources: [Deployment/demo/worker status: 'Failed']"
	appsKs.Conditions = []fluxCondition{
		{Type: "Ready", Status: "False", Reason: "HealthCheckFailed", Message: appsKs.Message, At: ms(25 * time.Minute)},
		{Type: "Healthy", Status: "False", Reason: "HealthCheckFailed", Message: "Deployment/demo/worker status: 'Failed'", At: ms(25 * time.Minute)},
	}
	appsKs.Resources = []fluxResource{
		res("", "ConfigMap", "demo", "worker-config"),
		res("", "Service", "demo", "worker"),
		res("apps", "Deployment", "demo", "worker"),
		res("", "Service", "demo", "hello-ichor"),
		res("apps", "Deployment", "demo", "hello-ichor"),
	}

	for _, p := range demoPods() {
		if !p.Healthy && p.Namespace == "demo" {
			appsKs.UnhealthyPods = append(appsKs.UnhealthyPods, p)
		}
	}

	promRepo := &fluxRef{Kind: "HelmRepository", Namespace: "flux-system", Name: "prometheus-community"}
	nginxRepo := &fluxRef{Kind: "HelmRepository", Namespace: "flux-system", Name: "ingress-nginx"}
	podinfoRepo := &fluxRef{Kind: "OCIRepository", Namespace: "flux-system", Name: "podinfo"}

	nginx := hr("ingress-nginx", "ingress-nginx", "ingress-nginx", "4.13.3", nginxRepo, "https://kubernetes.github.io/ingress-nginx", healthCritical,
		hist(4, "4.13.3", "1.13.3", "failed", 12*time.Minute), hist(3, "4.12.1", "1.12.1", "deployed", 30*24*time.Hour), hist(2, "4.11.3", "1.11.3", "superseded", 70*24*time.Hour))
	nginx.Ready, nginx.Reason, nginx.Stalled, nginx.Failures = "False", "RetriesExceeded", true, 3
	nginx.Revision = "4.12.1"
	nginx.Message = "Failed to upgrade after 3 attempt(s): context deadline exceeded"
	nginx.Conditions = []fluxCondition{
		{Type: "Stalled", Status: "True", Reason: "RetriesExceeded", Message: "Failed to upgrade after 3 attempt(s)", At: ms(12 * time.Minute)},
		{Type: "Ready", Status: "False", Reason: "RetriesExceeded", Message: nginx.Message, At: ms(12 * time.Minute)},
		{Type: "Released", Status: "False", Reason: "UpgradeFailed", Message: "Helm upgrade failed for release ingress-nginx/ingress-nginx with chart ingress-nginx@4.13.3: context deadline exceeded", At: ms(12 * time.Minute)},
	}

	prom := hr("monitoring", "kube-prometheus-stack", "kube-prometheus-stack", "77.x", promRepo, "https://prometheus-community.github.io/helm-charts", healthWarning,
		hist(9, "77.5.0", "v0.85.0", "deployed", 9*24*time.Hour))
	prom.Icon = "prometheus"
	prom.Revision, prom.AttemptedRevision = "77.5.0", "77.6.1"
	prom.Ready, prom.Reason, prom.Reconciling = "Unknown", "Progressing", true
	prom.Message = "Running 'upgrade' action with timeout of 10m0s"
	prom.Conditions = []fluxCondition{
		{Type: "Reconciling", Status: "True", Reason: "Progressing", Message: "Running 'upgrade' action with timeout of 10m0s", At: ms(70 * time.Second)},
		{Type: "Ready", Status: "Unknown", Reason: "Progressing", Message: prom.Message, At: ms(70 * time.Second)},
	}

	podinfo := hr("demo", "podinfo", "", "", podinfoRepo, "oci://ghcr.io/stefanprodan/charts/podinfo", healthOK,
		hist(6, "6.9.2", "6.9.2", "deployed", 3*time.Hour), hist(5, "6.9.1", "6.9.1", "superseded", 8*24*time.Hour))
	podinfo.Chart, podinfo.Icon, podinfo.Revision, podinfo.AttemptedRevision = "podinfo", "", "6.9.2", "6.9.2"
	podinfo.Owner = &fluxRef{Kind: "Kustomization", Namespace: "flux-system", Name: "apps"}

	redis := hr("cache", "redis", "redis", "21.2.x", &fluxRef{Kind: "HelmRepository", Namespace: "flux-system", Name: "bitnami"}, "oci://registry-1.docker.io/bitnamicharts", healthIdle,
		hist(2, "21.2.5", "8.0.2", "deployed", 40*24*time.Hour))
	redis.Suspended, redis.Revision = true, "21.2.5"

	out := mapFlux(nil, nil, nil)
	out.Version = "v2.7.0"
	out.Apps = []fluxApp{appsKs, nginx, infraKs, prom, rootKs, podinfo, redis}
	out.Sources = []fluxSource{
		{Kind: "HelmRepository", Namespace: "flux-system", Name: "ingress-nginx", Level: healthCritical, URL: "https://kubernetes.github.io/ingress-nginx",
			Ready: "False", Reason: "IndexationFailed", Message: "failed to fetch Helm repository index: Get \"https://kubernetes.github.io/ingress-nginx/index.yaml\": dial tcp: i/o timeout",
			Interval: "1h", FetchedAt: ms(26 * time.Hour), Revision: "sha256:7e1a0f4c2b9d", Apps: 1},
		{Kind: "GitRepository", Namespace: "flux-system", Name: "flux-system", Level: healthOK, URL: gitURL, Ref: "main", Revision: rev,
			Ready: "True", Reason: "Succeeded", Message: "stored artifact for revision '" + rev + "'", Interval: "1m", FetchedAt: ms(4 * time.Minute), Apps: 3},
		{Kind: "OCIRepository", Namespace: "flux-system", Name: "podinfo", Level: healthOK, URL: "oci://ghcr.io/stefanprodan/charts/podinfo", Ref: "6.9.x",
			Revision: "6.9.2@sha256:3b1f9c0a8e7d", Ready: "True", Reason: "Succeeded", Message: "stored artifact for digest '6.9.2@sha256:3b1f9c0a8e7d'", Interval: "10m", FetchedAt: ms(3 * time.Hour), Apps: 1},
		{Kind: "HelmRepository", Namespace: "flux-system", Name: "prometheus-community", Level: healthOK, URL: "https://prometheus-community.github.io/helm-charts",
			Revision: "sha256:c0ffee12ab34", Ready: "True", Reason: "Succeeded", Message: "stored artifact: revision 'sha256:c0ffee12ab34'", Interval: "1h", FetchedAt: ms(20 * time.Minute), Apps: 1},
		{Kind: "HelmRepository", Namespace: "flux-system", Name: "bitnami", Level: healthOK, URL: "oci://registry-1.docker.io/bitnamicharts", Interval: "1h", Apps: 1},
	}

	return out
}
