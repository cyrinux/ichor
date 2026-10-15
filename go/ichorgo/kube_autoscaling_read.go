package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// autoscalingRaw is what the reads brought back, before the rows are built. Each group's
// goroutine writes its own fields only.
type autoscalingRaw struct {
	hpas   []hpaObject
	hpaErr error

	kedaVersion   string
	scaledObjects []scaledObjectObject
	scaledJobs    []scaledJobObject
	kedaErr       error

	vpaVersion  string
	vpas        []vpaObject
	vpaRequests map[string]podRequests // "namespace/Kind/name" -> container -> requests
	vpaErr      error

	karpenterVersion string
	nodePools        []nodePoolObject
	nodeClaims       []nodeClaimObject
	karpenterErr     error
}

// readAutoscaling lists every autoscaler the cluster serves, in parallel, and builds the
// report. Every group fails soft: a 403 on one list is that group's error, not a failed call.
func readAutoscaling(ctx context.Context, k *kubeClient) (autoscalingReport, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return autoscalingReport{}, err
	}

	raw := autoscalingRaw{
		kedaVersion:      groups[groupKEDA],
		vpaVersion:       groups[groupVPA],
		karpenterVersion: groups[groupKarpenter],
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		var list kubeList[hpaObject]
		raw.hpaErr = getList(ctx, k, "/apis/autoscaling/v2/horizontalpodautoscalers", &list)
		raw.hpas = list.Items
	})
	if raw.kedaVersion != "" {
		wg.Go(func() { raw.scaledObjects, raw.scaledJobs, raw.kedaErr = readKEDA(ctx, k, raw.kedaVersion) })
	}
	if raw.vpaVersion != "" {
		wg.Go(func() { raw.vpas, raw.vpaRequests, raw.vpaErr = readVPAs(ctx, k, raw.vpaVersion) })
	}
	if raw.karpenterVersion != "" {
		wg.Go(func() { raw.nodePools, raw.nodeClaims, raw.karpenterErr = readKarpenter(ctx, k, raw.karpenterVersion) })
	}

	wg.Wait()

	return buildAutoscaling(raw, time.Now()), nil
}

// buildAutoscaling turns the raw objects into the report: rows, states, the workload index,
// problems first and each list capped at autoscalingRowsMax.
func buildAutoscaling(raw autoscalingRaw, now time.Time) autoscalingReport {
	r := autoscalingReport{
		HPA:       hpaGroup{autoscalingGroupState: autoscalingGroupState{Installed: true, Version: "v2", Error: sectionError(raw.hpaErr)}},
		KEDA:      kedaGroup{autoscalingGroupState: groupState(raw.kedaVersion, raw.kedaErr)},
		VPA:       vpaGroup{autoscalingGroupState: groupState(raw.vpaVersion, raw.vpaErr)},
		Karpenter: karpenterGroup{autoscalingGroupState: groupState(raw.karpenterVersion, raw.karpenterErr)},
	}

	r.HPA.Items = mapRows(raw.hpas, hpaRowOf)
	r.KEDA.ScaledObjects = scaledObjectRows(raw.scaledObjects, r.HPA.Items)
	r.KEDA.ScaledJobs = mapRows(raw.scaledJobs, scaledJobRowOf)
	r.VPA.Items = vpaRows(raw.vpas, raw.vpaRequests)
	r.Karpenter.NodePools, r.Karpenter.NodeClaims = karpenterRows(raw.nodePools, raw.nodeClaims, now)
	r.Workloads = autoscaledWorkloadRows(r.HPA.Items, r.KEDA.ScaledObjects, r.VPA.Items)

	sortAutoscaling(&r)

	var t1, t2, t3, t4, t5, t6 bool
	r.HPA.Items, t1 = clipRows(r.HPA.Items)
	r.KEDA.ScaledObjects, t2 = clipRows(r.KEDA.ScaledObjects)
	r.KEDA.ScaledJobs, t3 = clipRows(r.KEDA.ScaledJobs)
	r.VPA.Items, t4 = clipRows(r.VPA.Items)
	r.Karpenter.NodePools, t5 = clipRows(r.Karpenter.NodePools)
	r.Karpenter.NodeClaims, t6 = clipRows(r.Karpenter.NodeClaims)
	r.HPA.Truncated, r.KEDA.Truncated, r.VPA.Truncated, r.Karpenter.Truncated = t1, t2 || t3, t4, t5 || t6
	r.Workloads, r.WorkloadsTruncated = clipRows(r.Workloads)

	return r
}

// groupState is an optional group's header: not installed when the cluster does not serve it.
func groupState(version string, err error) autoscalingGroupState {
	return autoscalingGroupState{Installed: version != "", Version: version, Error: sectionError(err)}
}

// mapRows maps every object into its row; never nil, so the JSON says [].
func mapRows[O, R any](objs []O, row func(O) R) []R {
	out := make([]R, 0, len(objs))
	for _, o := range objs {
		out = append(out, row(o))
	}

	return out
}

// clipRows keeps the first autoscalingRowsMax rows, and says whether it cut.
func clipRows[T any](rows []T) ([]T, bool) {
	if len(rows) <= autoscalingRowsMax {
		return rows, false
	}

	return rows[:autoscalingRowsMax], true
}

// autoscaleRank orders the states: problems first, the healthy last.
func autoscaleRank(state string) int {
	switch state {
	case autoscaleNotReady:
		return 0
	case autoscaleAtMax:
		return 1
	case autoscaleLimited:
		return 2
	case autoscaleFallback:
		return 3
	case autoscalePaused:
		return 4
	default:
		return 5
	}
}

// byStateThenName is the order of every namespaced list: problems first, then namespace, name.
func byStateThenName(stateA, nsA, nameA, stateB, nsB, nameB string) int {
	return cmp.Or(
		cmp.Compare(autoscaleRank(stateA), autoscaleRank(stateB)),
		strings.Compare(nsA, nsB),
		strings.Compare(nameA, nameB),
	)
}

func sortAutoscaling(r *autoscalingReport) {
	slices.SortFunc(r.HPA.Items, func(a, b hpaRow) int {
		return byStateThenName(a.State, a.Namespace, a.Name, b.State, b.Namespace, b.Name)
	})
	slices.SortFunc(r.KEDA.ScaledObjects, func(a, b scaledObjectRow) int {
		return byStateThenName(a.State, a.Namespace, a.Name, b.State, b.Namespace, b.Name)
	})
	slices.SortFunc(r.KEDA.ScaledJobs, func(a, b scaledJobRow) int {
		return byStateThenName(a.State, a.Namespace, a.Name, b.State, b.Namespace, b.Name)
	})
	slices.SortFunc(r.VPA.Items, func(a, b vpaRow) int {
		return byStateThenName(a.State, a.Namespace, a.Name, b.State, b.Namespace, b.Name)
	})
	slices.SortFunc(r.Karpenter.NodePools, nodePoolLess)
	slices.SortFunc(r.Karpenter.NodeClaims, nodeClaimLess)
	slices.SortFunc(r.Workloads, func(a, b autoscaledWorkload) int {
		return cmp.Or(
			byStateThenName(a.State, a.Namespace, a.Kind+"/"+a.Name, b.State, b.Namespace, b.Kind+"/"+b.Name),
			strings.Compare(a.By, b.By),
		)
	})
}

// hpaObject is a HorizontalPodAutoscaler (autoscaling/v2): what it scales, between which
// bounds, on which metrics, and what it last did.
type hpaObject struct {
	Metadata struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace"`
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		ScaleTargetRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"scaleTargetRef"`
		MinReplicas *int32          `json:"minReplicas"`
		MaxReplicas int32           `json:"maxReplicas"`
		Metrics     []hpaMetricSpec `json:"metrics"`
	} `json:"spec"`
	Status struct {
		CurrentReplicas int32           `json:"currentReplicas"`
		DesiredReplicas int32           `json:"desiredReplicas"`
		LastScaleTime   string          `json:"lastScaleTime"`
		CurrentMetrics  []hpaMetricSpec `json:"currentMetrics"`
		Conditions      kubeConditions  `json:"conditions"`
	} `json:"status"`
}

// hpaMetricSpec is one entry of spec.metrics or status.currentMetrics: the type names the
// one source block that is set.
type hpaMetricSpec struct {
	Type              string           `json:"type"`
	Resource          *hpaMetricSource `json:"resource"`
	ContainerResource *hpaMetricSource `json:"containerResource"`
	Pods              *hpaMetricSource `json:"pods"`
	Object            *hpaMetricSource `json:"object"`
	External          *hpaMetricSource `json:"external"`
}

type hpaMetricSource struct {
	Name      string `json:"name"` // the resource (cpu, memory) of a (Container)Resource metric
	Container string `json:"container"`
	Metric    struct {
		Name string `json:"name"`
	} `json:"metric"`
	Target  hpaMetricValue `json:"target"`
	Current hpaMetricValue `json:"current"`
}

type hpaMetricValue struct {
	Type               string `json:"type"` // a target's: Utilization, AverageValue or Value
	AverageUtilization *int32 `json:"averageUtilization"`
	AverageValue       string `json:"averageValue"`
	Value              string `json:"value"`
}

// source is the block the metric's type names, an empty one when it is missing.
func (m hpaMetricSpec) source() hpaMetricSource {
	var src *hpaMetricSource
	switch m.Type {
	case "Resource":
		src = m.Resource
	case "ContainerResource":
		src = m.ContainerResource
	case "Pods":
		src = m.Pods
	case "Object":
		src = m.Object
	case "External":
		src = m.External
	}

	if src == nil {
		return hpaMetricSource{}
	}

	return *src
}

// name is how the metric reads: the resource ("cpu", "memory of app") or the metric's name.
func (s hpaMetricSource) name() string {
	switch {
	case s.Name != "" && s.Container != "":
		return s.Name + " of " + s.Container
	case s.Name != "":
		return s.Name
	default:
		return s.Metric.Name
	}
}

// text is the value as the user reads it: "63%" for a utilization, else the quantity; "-"
// when none is set (the metrics pipeline is down).
// as is a current value in the unit of the target type: the API fills a Resource metric's
// averageUtilization and averageValue together, and only the one the target uses compares.
func (v hpaMetricValue) as(targetType string) string {
	switch {
	case targetType == "Utilization" && v.AverageUtilization != nil:
		return hpaMetricValue{AverageUtilization: v.AverageUtilization}.text()
	case targetType == "AverageValue" && v.AverageValue != "":
		return v.AverageValue
	case targetType == "Value" && v.Value != "":
		return v.Value
	default:
		return v.text()
	}
}

func (v hpaMetricValue) text() string {
	switch {
	case v.AverageUtilization != nil:
		return fmt.Sprintf("%d%%", *v.AverageUtilization)
	case v.AverageValue != "":
		return v.AverageValue
	case v.Value != "":
		return v.Value
	default:
		return "-"
	}
}

// hpaRowOf maps one HPA into its row.
func hpaRowOf(o hpaObject) hpaRow {
	row := hpaRow{
		Namespace:   o.Metadata.Namespace,
		Name:        o.Metadata.Name,
		Target:      autoscaleTarget{Kind: o.Spec.ScaleTargetRef.Kind, Name: o.Spec.ScaleTargetRef.Name},
		Min:         1, // the API's default
		Max:         int(o.Spec.MaxReplicas),
		Current:     int(o.Status.CurrentReplicas),
		Desired:     int(o.Status.DesiredReplicas),
		LastScaleAt: unixMilli(o.Status.LastScaleTime),
		Metrics:     make([]hpaMetric, 0, len(o.Spec.Metrics)),
	}
	if o.Spec.MinReplicas != nil {
		row.Min = int(*o.Spec.MinReplicas)
	}

	for _, ref := range o.Metadata.OwnerReferences {
		if ref.Kind == "ScaledObject" { // KEDA's own HPA: matched by owner, never by its name
			row.ManagedBy, row.ScaledObject = autoscaledByKEDA, ref.Name
		}
	}

	for _, m := range o.Spec.Metrics {
		row.Metrics = append(row.Metrics, hpaMetricOf(m, o.Status.CurrentMetrics))
	}

	conds := o.Status.Conditions
	row.Conditions = hpaConditions{
		AbleToScale:    conds.is("AbleToScale"),
		ScalingActive:  conds.is("ScalingActive"),
		ScalingLimited: conds.is("ScalingLimited"),
		Message:        hpaMessage(conds),
	}
	row.AtMax = row.Current == row.Max && row.Conditions.ScalingLimited
	row.State = hpaState(row, conds)

	return row
}

// hpaMetricOf pairs one target metric with its current value in the status.
func hpaMetricOf(spec hpaMetricSpec, current []hpaMetricSpec) hpaMetric {
	src := spec.source()
	out := hpaMetric{Type: spec.Type, Name: src.name(), Target: src.Target.text(), Current: "-"}

	for _, c := range current {
		if cur := c.source(); c.Type == spec.Type && cur.name() == out.Name {
			out.Current = cur.Current.as(src.Target.Type)

			break
		}
	}

	return out
}

// hpaMessage is the condition worth reading: why it cannot scale, else why it is limited.
func hpaMessage(conds kubeConditions) string {
	for _, typ := range []string{"AbleToScale", "ScalingActive"} {
		if c := conds.get(typ); c.Status == "False" {
			return c.Message
		}
	}

	if c := conds.get("ScalingLimited"); c.Status == "True" {
		return c.Message
	}

	return ""
}

// hpaState is the row's state: an HPA whose target was scaled to 0 is disabled (paused),
// one that cannot fetch its metrics or scale is not ready.
func hpaState(row hpaRow, conds kubeConditions) string {
	active := conds.get("ScalingActive")

	switch {
	case active.Status == "False" && active.Reason == "ScalingDisabled":
		return autoscalePaused
	case conds.get("AbleToScale").Status == "False" || active.Status == "False":
		return autoscaleNotReady
	case row.AtMax:
		return autoscaleAtMax
	case row.Conditions.ScalingLimited:
		return autoscaleLimited
	default:
		return autoscaleOK
	}
}

// autoscaledWorkloadRows indexes the autoscalers by the workload they drive. A KEDA-owned
// HPA is its ScaledObject's: the index names the ScaledObject only.
func autoscaledWorkloadRows(hpas []hpaRow, scaledObjects []scaledObjectRow, vpas []vpaRow) []autoscaledWorkload {
	out := make([]autoscaledWorkload, 0, len(hpas)+len(scaledObjects)+len(vpas))

	for _, h := range hpas {
		if h.ManagedBy == autoscaledByKEDA {
			continue
		}

		out = append(out, autoscaledWorkload{
			Kind: h.Target.Kind, Namespace: h.Namespace, Name: h.Target.Name, By: autoscaledByHPA, Autoscaler: h.Name,
			Min: h.Min, Max: h.Max, Current: h.Current, Desired: h.Desired, Paused: h.State == autoscalePaused, State: h.State,
		})
	}

	for _, s := range scaledObjects {
		out = append(out, autoscaledWorkload{
			Kind: s.Target.Kind, Namespace: s.Namespace, Name: s.Target.Name, By: autoscaledByKEDA, Autoscaler: s.Name,
			Min: s.Min, Max: s.Max, Current: s.Current, Desired: s.Desired, Paused: s.Paused, State: s.State,
		})
	}

	for _, v := range vpas {
		out = append(out, autoscaledWorkload{
			Kind: v.Target.Kind, Namespace: v.Namespace, Name: v.Target.Name, By: autoscaledByVPA, Autoscaler: v.Name, State: v.State,
		})
	}

	return out
}
