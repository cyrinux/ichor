package ichorgo

import (
	"time"

	"github.com/cyrinux/ichor/go/internal/upstream"
)

// demoClusterUpdates is the demo cluster's updates: one of each case the screen shows,
// answered offline.
func demoClusterUpdates(now time.Time) clusterUpdates {
	workload := func(kind, ns, name, container string) []updateWorkloadRef {
		return []updateWorkloadRef{{Kind: kind, Namespace: ns, Name: name, Containers: []string{container}}}
	}
	cand := func(current, patch, minor, major, source string) upstream.Candidate {
		return upstream.Candidate{Current: current, Patch: patch, Minor: minor, Major: major, Shape: "x.y.z", Source: source}
	}
	flux := &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: "demo", Name: "podinfo"}
	argo := &updateOwner{Tool: "argocd", Kind: "Application", Namespace: "argocd", Name: "hello-ichor"}

	podinfo := &updateImage{
		Repo: "ghcr.io/stefanprodan/podinfo", Tag: "6.7.0", Workloads: workload("Deployment", "demo", "podinfo", "podinfo"), Owner: flux,
		Candidate: cand("6.7.0", "6.7.1", "6.9.2", "", "ghcr.io"),
		Edit:      &updateEdit{Op: "path", Hint: updateRenderedHint, ValueFiles: []string{}},
	}
	nginx := &updateImage{
		Repo: "nginx", Tag: "1.27.4-alpine", Workloads: workload("Deployment", "demo", "hello-ichor", "nginx"), Owner: argo,
		Candidate: upstream.Candidate{Current: "1.27.4-alpine", Patch: "1.27.5-alpine", Shape: "x.y.z-alpine", Source: "Docker Hub"},
	}
	nginx.Edit = &updateEdit{Op: "image", Kind: "Deployment", Name: "hello-ichor", Container: "nginx", Image: "nginx:1.27.5-alpine"}

	coredns := &updateImage{
		Repo: "registry.k8s.io/coredns/coredns", Tag: "v1.12.0", Workloads: workload("Deployment", "kube-system", "coredns", "coredns"),
		Owner: &updateOwner{Tool: "helm", Kind: "Release", Namespace: "kube-system", Name: "coredns"}, Candidate: cand("v1.12.0", "v1.12.4", "", "", "registry.k8s.io"),
	}
	coredns.Edit = &updateEdit{Op: "path", Hint: updateRenderedHint}

	return clusterUpdates{
		At: now.UnixMilli(),
		Images: []*updateImage{
			podinfo, nginx, coredns,
			{
				Repo: "ghcr.io/example-org/worker", Tag: "2.3.0", Workloads: workload("Deployment", "demo", "worker", "worker"), Owner: argo,
				Candidate: cand("2.3.0", "2.3.1", "", "", "ghcr.io"), AutomatedBy: "argocd-image-updater",
			},
			{
				Repo: "postgres", Tag: "17", Workloads: workload("StatefulSet", "demo", "postgres", "postgres"),
				Candidate: upstream.Candidate{Current: "17", Reason: "rate limited by registry-1.docker.io, retry after 3600 s"},
			},
			{
				Repo: "busybox", Tag: "latest", Workloads: workload("Deployment", "demo", "worker", "sidecar"),
				Candidate: upstream.Candidate{Current: "latest", Reason: upstream.ReasonNotVersion},
			},
		},
		Charts: []*updateChart{
			{
				Name: "ingress-nginx", Version: "4.12.1", RepoURL: "https://kubernetes.github.io/ingress-nginx",
				Owner:     &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: "ingress-nginx", Name: "ingress-nginx"},
				Candidate: cand("4.12.1", "4.12.4", "4.13.3", "", "kubernetes.github.io"),
				Edit:      &updateEdit{Op: "path", Path: "spec.chart.spec.version", Value: "4.12.4"},
			},
			{
				Name: "podinfo", Version: "6.7.0", RepoURL: "oci://ghcr.io/stefanprodan/charts", Owner: flux, Constraint: ">=6.0.0",
				Candidate: cand("6.7.0", "", "", "", "ghcr.io"), AutomatedBy: "flux-semver",
			},
		},
		Skipped: []updateSkipped{},
		Lookups: updateLookups{Queried: 8, Failed: 1},
	}
}
