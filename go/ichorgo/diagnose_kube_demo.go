package ichorgo

import (
	"cmp"
	"slices"
	"time"
)

// demoDiagnosis is the demo cluster's report data: its workloads, pods, events and checkup, and
// one node under memory pressure. No network, and only the demo's invented names.
func demoDiagnosis(now time.Time, locale string) kubeDiagnosisData {
	overview := demoEKSNodes()

	d := kubeDiagnosisData{At: now, Version: overview.ServerVersion, Nodes: slices.Clone(overview.Nodes)}
	if len(d.Nodes) > 0 {
		last := len(d.Nodes) - 1
		d.Nodes[last].Pressure = []string{"MemoryPressure"}
	}

	pods := slices.DeleteFunc(demoPods(), func(p kubePod) bool { return !podInTrouble(p) })

	events := slices.DeleteFunc(demoEvents("demo", "", "", now).Events, func(e kubeEvent) bool { return e.Type != "Warning" })
	slices.SortStableFunc(events, func(a, b kubeEvent) int { return cmp.Compare(b.Last, a.Last) })

	var workloads []idleWorkload

	for _, w := range demoKubeWorkloads() {
		if w.Desired == 0 || w.Ready >= w.Desired {
			continue
		}

		idle := idleWorkload{
			inventoryWorkload: inventoryWorkload{Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, Desired: w.Desired},
			images:            w.Images, ready: w.Ready,
		}
		if w.Name == "mealie" {
			idle.Reason = demoNotStartingReason
		}

		workloads = append(workloads, idle)
	}

	d.setPods(pods, events)
	d.setWorkloads(workloads, events)
	d.setEvents(events)
	d.applyCheckup(demoCheckup(now, locale))

	argo, flux := demoArgoCD(now), demoFlux(now)
	d.GitOps = &gitopsState{Argo: &argo, Flux: &flux}

	return d
}
