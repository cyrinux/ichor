package ichorgo

import (
	"context"
	"strconv"
	"strings"
	"sync"
)

// KEDA's pause annotations: paused stops scaling where it is, paused-replicas first scales
// the target to that count.
const (
	kedaPausedAnnotation         = "autoscaling.keda.sh/paused"
	kedaPausedReplicasAnnotation = "autoscaling.keda.sh/paused-replicas"
	// kedaDefaultMax is spec.maxReplicaCount when unset (ScaledObject and ScaledJob alike).
	kedaDefaultMax = 100
)

// kedaSummaryKeys are the trigger metadata keys a summary may show, in order of preference:
// what the user recognises a trigger by. Nothing else is read, so a secret never shows.
var kedaSummaryKeys = []string{"query", "topic", "queueName", "listName", "stream", "metricName", "value", "desiredReplicas"}

type kedaTriggerObject struct {
	Type     string            `json:"type"`
	Metadata map[string]string `json:"metadata"`
}

type kedaObjectMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Annotations map[string]string `json:"annotations"`
}

type scaledObjectObject struct {
	Metadata kedaObjectMeta `json:"metadata"`
	Spec     struct {
		ScaleTargetRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"scaleTargetRef"`
		MinReplicaCount *int32              `json:"minReplicaCount"`
		MaxReplicaCount *int32              `json:"maxReplicaCount"`
		Triggers        []kedaTriggerObject `json:"triggers"`
		Fallback        *struct {
			Replicas int32 `json:"replicas"`
		} `json:"fallback"`
	} `json:"spec"`
	Status struct {
		HPAName    string         `json:"hpaName"`
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

type scaledJobObject struct {
	Metadata kedaObjectMeta `json:"metadata"`
	Spec     struct {
		MaxReplicaCount *int32              `json:"maxReplicaCount"`
		Triggers        []kedaTriggerObject `json:"triggers"`
	} `json:"spec"`
	Status struct {
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

// readKEDA lists the ScaledObjects and ScaledJobs of every namespace; a failed list keeps
// the other one.
func readKEDA(ctx context.Context, k *kubeClient, version string) ([]scaledObjectObject, []scaledJobObject, error) {
	var (
		objects        kubeList[scaledObjectObject]
		jobs           kubeList[scaledJobObject]
		objErr, jobErr error
		wg             sync.WaitGroup
	)

	base := "/apis/" + groupKEDA + "/" + version
	wg.Go(func() { objErr = getList(ctx, k, base+"/scaledobjects", &objects) })
	wg.Go(func() { jobErr = getList(ctx, k, base+"/scaledjobs", &jobs) })
	wg.Wait()

	return objects.Items, jobs.Items, firstErr(objErr, jobErr)
}

// scaledObjectRows maps the ScaledObjects, taking current and desired from the HPA each owns.
func scaledObjectRows(objs []scaledObjectObject, hpas []hpaRow) []scaledObjectRow {
	owned := map[string]hpaRow{}
	for _, h := range hpas {
		if h.ManagedBy == autoscaledByKEDA {
			owned[h.Namespace+"/"+h.ScaledObject] = h
		}
	}

	out := make([]scaledObjectRow, 0, len(objs))
	for _, o := range objs {
		out = append(out, scaledObjectRowOf(o, owned[o.Metadata.Namespace+"/"+o.Metadata.Name]))
	}

	return out
}

// scaledObjectRowOf maps one ScaledObject; hpa is the HPA it owns (zero when none: KEDA
// removes it while the object is paused).
func scaledObjectRowOf(o scaledObjectObject, hpa hpaRow) scaledObjectRow {
	row := scaledObjectRow{
		Namespace: o.Metadata.Namespace,
		Name:      o.Metadata.Name,
		Target:    autoscaleTarget{Kind: o.Spec.ScaleTargetRef.Kind, Name: o.Spec.ScaleTargetRef.Name},
		Min:       0,
		Max:       kedaDefaultMax,
		Current:   hpa.Current, // 0 while paused without a count: KEDA removed its HPA
		Desired:   hpa.Desired,
		Triggers:  kedaTriggers(o.Spec.Triggers),
		HPAName:   o.Status.HPAName,
	}
	if row.Target.Kind == "" {
		row.Target.Kind = "Deployment"
	}
	if o.Spec.MinReplicaCount != nil {
		row.Min = int(*o.Spec.MinReplicaCount)
	}
	if o.Spec.MaxReplicaCount != nil {
		row.Max = int(*o.Spec.MaxReplicaCount)
	}
	if o.Spec.Fallback != nil {
		row.FallbackReplicas = int(o.Spec.Fallback.Replicas)
	}
	if row.HPAName == "" {
		row.HPAName = hpa.Name
	}

	row.Paused, row.PausedReplicas = kedaPaused(o.Metadata.Annotations)
	row.Paused = row.Paused || o.Status.Conditions.is("Paused") // KEDA 2.12+ says so too
	// KEDA removes its HPA while paused: a count it was paused at is the replicas it runs.
	if row.PausedReplicas != nil && hpa.Name == "" {
		row.Current, row.Desired = *row.PausedReplicas, *row.PausedReplicas
	}

	conds := o.Status.Conditions
	row.Ready, row.Active, row.Fallback = conds.is("Ready"), conds.is("Active"), conds.is("Fallback")
	row.Message = kedaMessage(conds)
	row.State = scaledObjectState(row, hpa)

	return row
}

// kedaPaused reads the pause annotations the way KEDA does: paused-replicas pauses whatever
// its value (with a count when it is one); paused pauses unless it parses as false.
func kedaPaused(annotations map[string]string) (bool, *int) {
	if v, ok := annotations[kedaPausedReplicasAnnotation]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
			return true, &n
		}

		return true, nil
	}

	v, ok := annotations[kedaPausedAnnotation]
	if !ok {
		return false, nil
	}

	paused, err := strconv.ParseBool(strings.TrimSpace(v))

	return err != nil || paused, nil
}

// kedaMessage is the condition worth reading: why it is not ready, else why it fell back.
// Active False only means idle (at its minimum), so it says nothing.
func kedaMessage(conds kubeConditions) string {
	if c := conds.get("Ready"); c.Status == "False" {
		return c.Message
	}

	if c := conds.get("Fallback"); c.Status == "True" {
		return c.Message
	}

	return ""
}

// scaledObjectState is the row's state: paused wins (it is a choice), then the problems.
func scaledObjectState(row scaledObjectRow, hpa hpaRow) string {
	switch {
	case row.Paused:
		return autoscalePaused
	case !row.Ready:
		return autoscaleNotReady
	case row.Fallback:
		return autoscaleFallback
	case hpa.AtMax:
		return autoscaleAtMax
	case hpa.Conditions.ScalingLimited:
		return autoscaleLimited
	default:
		return autoscaleOK
	}
}

func scaledJobRowOf(o scaledJobObject) scaledJobRow {
	row := scaledJobRow{
		Namespace:       o.Metadata.Namespace,
		Name:            o.Metadata.Name,
		Triggers:        kedaTriggers(o.Spec.Triggers),
		MaxReplicaCount: kedaDefaultMax,
		Ready:           o.Status.Conditions.is("Ready"),
		Message:         kedaMessage(o.Status.Conditions),
	}
	if o.Spec.MaxReplicaCount != nil {
		row.MaxReplicaCount = int(*o.Spec.MaxReplicaCount)
	}

	row.Paused, _ = kedaPaused(o.Metadata.Annotations)
	row.Paused = row.Paused || o.Status.Conditions.is("Paused")

	switch {
	case row.Paused:
		row.State = autoscalePaused
	case !row.Ready:
		row.State = autoscaleNotReady
	default:
		row.State = autoscaleOK
	}

	return row
}

// kedaTriggers maps the triggers with their summary.
func kedaTriggers(triggers []kedaTriggerObject) []kedaTrigger {
	out := make([]kedaTrigger, 0, len(triggers))
	for _, t := range triggers {
		out = append(out, kedaTrigger{Type: t.Type, Summary: clipUTF8(kedaTriggerSummary(t), kedaTriggerSummaryMax)})
	}

	return out
}

// kedaTriggerSummary is a cron trigger's window ("0 8 * * 1-5 → 0 20 * * 1-5 Europe/Paris"),
// else the first allow-listed metadata value.
func kedaTriggerSummary(t kedaTriggerObject) string {
	if t.Type == "cron" {
		s := strings.TrimSpace(t.Metadata["start"] + " → " + t.Metadata["end"])
		if tz := t.Metadata["timezone"]; tz != "" {
			s += " " + tz
		}

		return s
	}

	for _, key := range kedaSummaryKeys {
		if v := strings.TrimSpace(t.Metadata[key]); v != "" {
			return v
		}
	}

	return ""
}
