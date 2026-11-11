package ichorgo

import (
	"slices"

	"github.com/cyrinux/ichor/go/kube/helm"
)

// The GitOps roots of the facade contract (CYR-581): the diffs and the Helm reads, so their
// types are walked by TestContractTypesAreTagged and their keys checked by the apps before the
// domains move out of the facade. Each is the demo answer with the fields it leaves empty filled,
// so an omitempty key still shows in the fixture.
//
// The Argo CD sync windows and freezes (kube_argocd_syncwindows.go) are part of the argocd root;
// KubeArgoFreeze and KubeArgoAction return no JSON, and the map is the netmap root.

// contractLiveChange is a "Keep in Git" value: the replica count changed in the cluster.
var contractLiveChange = diffLiveChange{
	Path: []string{"spec", "replicas"}, Label: "spec.replicas", Wanted: "2", Live: "3", Type: "int",
}

// contractFluxDiff is KubeFluxDiff's answer; its changed Deployment carries a live change.
func contractFluxDiff() any {
	return withLiveChange(demoFluxDiff("flux-system", "web"))
}

// contractArgoDiff is KubeArgoDiff's answer, with the same live change.
func contractArgoDiff() any {
	return withLiveChange(demoArgoDiff("argocd", "web"))
}

// withLiveChange moves d's changed Deployment (the second resource) first and gives it a live
// change: the Kotlin test checks the first item of a list only.
func withLiveChange(d gitOpsDiff) gitOpsDiff {
	r := slices.Clone(d.Resources)
	r[0], r[1] = r[1], r[0]
	r[0].LiveChanges = []diffLiveChange{contractLiveChange}
	d.Resources = r

	return d
}

// contractHelmRelease is KubeHelmRelease's answer with its notes and a revision description.
func contractHelmRelease() any {
	d := demoHelmRelease("ingress-nginx", "ingress-nginx")
	d.Notes = "The ingress controller is listening on port 80 and 443."
	d.History[0].Description = "Upgrade complete"

	return d
}

// contractHelmRollbackPlan is KubeHelmRollbackPlan's answer with both options, a Flux owner and a
// blocker, so no list or flag is left at its zero value.
func contractHelmRollbackPlan() any {
	p := demoHelmRollbackPlan("ingress-nginx", "ingress-nginx", 0, helm.Options{Force: true, HistoryMax: 2})
	p.ReleaseNamespace = "ingress-nginx"
	p.FluxOwner = "flux-system/ingress-nginx"
	p.Hooks[0].Error = "jobs.batch \"ingress-nginx-migrate\" is forbidden"
	p.Changes[0].Error = "configmaps \"ingress-nginx\" is forbidden"
	p.Blockers = []string{"the pre-rollback hook Job ingress-nginx-migrate was refused"}

	return p
}

// contractHelmRollbackProgress is what StartHelmRollbackFollow sends OnProgress mid-rollout.
func contractHelmRollbackProgress() any {
	steps := demoHelmRollbackSteps("ingress-nginx", "ingress-nginx")
	p := steps[len(steps)-2]
	p.At = contractNow.UnixMilli()

	return p
}
