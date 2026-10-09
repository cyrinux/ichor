package ichorgo

import "cmp"

// demoHelmRollbackPlan is a rollback of a demo release: one Deployment and one ConfigMap
// back, one ServiceMonitor gone.
func demoHelmRollbackPlan(namespace, name string, revision int) helmRollbackPlan {
	detail := demoHelmRelease(namespace, name)
	from := detail.Revision
	to := cmp.Or(revision, from-1)

	return helmRollbackPlan{
		Namespace: namespace, Name: name, From: from, To: to,
		FromChart: detail.Chart + "-" + detail.ChartVersion, ToChart: detail.Chart + "-" + demoPreviousVersion(detail.ChartVersion),
		FromAppVersion: detail.AppVersion, ToAppVersion: detail.AppVersion,
		Changes: []helmChange{
			{Action: helmChangeUpdate, Kind: "ConfigMap", Namespace: namespace, Name: name},
			{Action: helmChangeUpdate, Kind: "Deployment", Namespace: namespace, Name: name},
			{Action: helmChangeDelete, Kind: "ServiceMonitor", Namespace: namespace, Name: name},
		},
		Unchanged: 6, Blockers: []string{},
	}
}
