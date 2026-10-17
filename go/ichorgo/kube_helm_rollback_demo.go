package ichorgo

import "cmp"

// demoHelmRollbackPlan is a rollback of a demo release: one Deployment and one ConfigMap
// back (replaced with the force option), one ServiceMonitor gone, and the revisions of the
// demo history beyond opts.HistoryMax pruned.
func demoHelmRollbackPlan(namespace, name string, revision int, opts helmRollbackOptions) helmRollbackPlan {
	detail := demoHelmRelease(namespace, name)
	from := detail.Revision
	to := cmp.Or(revision, from-1)

	update := helmChangeUpdate
	if opts.Force {
		update = helmChangeReplace
	}

	refs := make([]helmSecretRef, 0, len(detail.History))
	for _, h := range detail.History {
		refs = append(refs, helmSecretRef{namespace: namespace, name: name, revision: h.Revision})
	}

	return helmRollbackPlan{
		Namespace: namespace, Name: name, From: from, To: to,
		FromChart: detail.Chart + "-" + detail.ChartVersion, ToChart: detail.Chart + "-" + demoPreviousVersion(detail.ChartVersion),
		FromAppVersion: detail.AppVersion, ToAppVersion: detail.AppVersion,
		Changes: []helmChange{
			{Action: update, Kind: "ConfigMap", Namespace: namespace, Name: name},
			{Action: update, Kind: "Deployment", Namespace: namespace, Name: name},
			{Action: helmChangeDelete, Kind: "ServiceMonitor", Namespace: namespace, Name: name},
		},
		Unchanged: 6, Blockers: []string{},
		Force: opts.Force, HistoryMax: opts.HistoryMax, Prune: helmRevisions(helmPruneRefs(refs, opts.HistoryMax)),
		Workloads: len(demoHelmRollbackWorkloads(namespace, name)),
	}
}
