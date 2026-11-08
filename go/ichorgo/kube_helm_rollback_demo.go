package ichorgo

import (
	"cmp"

	"github.com/cyrinux/ichor/go/kube/helm"
)

// demoHelmRollbackPlan is a rollback of a demo release: one Deployment and one ConfigMap
// back (replaced with the force option), one ServiceMonitor gone, a pre-rollback Job hook,
// and the revisions of the demo history beyond opts.HistoryMax pruned.
func demoHelmRollbackPlan(namespace, name string, revision int, opts helm.Options) helm.Plan {
	detail := demoHelmRelease(namespace, name)
	from := detail.Revision
	to := cmp.Or(revision, from-1)

	update := helm.ChangeUpdate
	if opts.Force {
		update = helm.ChangeReplace
	}

	return helm.Plan{
		Namespace: namespace, Name: name, From: from, To: to,
		FromChart: detail.Chart + "-" + detail.ChartVersion, ToChart: detail.Chart + "-" + demoPreviousVersion(detail.ChartVersion),
		FromAppVersion: detail.AppVersion, ToAppVersion: detail.AppVersion,
		Changes: []helm.Change{
			{Action: update, Kind: "ConfigMap", Namespace: namespace, Name: name},
			{Action: update, Kind: "Deployment", Namespace: namespace, Name: name},
			{Action: helm.ChangeDelete, Kind: "ServiceMonitor", Namespace: namespace, Name: name},
		},
		Hooks: []helm.Hook{{
			Name: name + "-migrate", Kind: "Job", Event: helm.HookPreRollback, Weight: -5,
			DeletePolicies: []string{helm.HookBeforeCreation, helm.HookSucceeded},
		}},
		Unchanged: 6, Blockers: []string{},
		Force: opts.Force, HistoryMax: opts.HistoryMax, Prune: helm.PruneRevisions(detail.History, opts.HistoryMax),
		Workloads: len(demoHelmRollbackWorkloads(namespace, name)),
	}
}
