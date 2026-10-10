package ichorgo

import (
	"encoding/json"
	"fmt"
	"time"
)

// The demo inventory's live events (StartKubeEvents), as events.k8s.io/v1 objects.

// demoCrashPod is the demo's crash-looping pod, as demoEvents names it.
const demoCrashPod = "worker-6f4b8-uvwxy"

// demoEventSpec is one demo Event object.
type demoEventSpec struct {
	uid, apiVersion, kind, name, eventType, reason, note, controller string
	count                                                            int
	first, last                                                      time.Time
}

func (d demoEventSpec) raw() json.RawMessage {
	var e eventsV1Object

	e.Metadata.UID, e.Metadata.Namespace, e.Metadata.Name = d.uid, "demo", d.name+"."+d.uid
	e.Metadata.CreationTimestamp = d.first
	e.Regarding.APIVersion, e.Regarding.Kind, e.Regarding.Namespace, e.Regarding.Name = d.apiVersion, d.kind, "demo", d.name
	e.Type, e.Reason, e.Note, e.ReportingController = d.eventType, d.reason, d.note, d.controller
	e.EventTime = &d.first

	if d.count > 1 {
		last := d.last
		e.Series = &struct {
			Count            int        `json:"count"`
			LastObservedTime *time.Time `json:"lastObservedTime"`
		}{d.count, &last}
	}

	data, _ := json.Marshal(e) //nolint:errcheck // plain values

	return data
}

// demoLiveEvents is what the demo stream lists first.
func demoLiveEvents(now time.Time) []json.RawMessage {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	specs := []demoEventSpec{
		{"demo-pulled", "v1", "Pod", demoCrashPod, "Normal", "Pulled", `Container image "busybox:1.37" already present on machine`, "kubelet", 15, ago(72 * time.Hour), ago(6 * time.Minute)},
		{"demo-created", "v1", "Pod", demoCrashPod, "Normal", "Created", "Created container: worker", "kubelet", 15, ago(72 * time.Hour), ago(6 * time.Minute)},
		{"demo-scaled", "apps/v1", "Deployment", "worker", "Normal", "ScalingReplicaSet", "Scaled up replica set worker-6f4b8 from 0 to 2", "deployment-controller", 1, ago(72 * time.Hour), ago(72 * time.Hour)},
		{"demo-probe", "v1", "Pod", "web-7d9c4-abcde", "Warning", "Unhealthy", "Readiness probe failed: HTTP probe failed with statuscode: 503", "kubelet", 3, ago(20 * time.Minute), ago(4 * time.Minute)},
	}

	items := []json.RawMessage{demoBackOff(now, now, 0)}
	for _, s := range specs {
		items = append(items, s.raw())
	}

	return items
}

// demoBackOff is the crash-looping pod's BackOff after tick ticks: the same object, its count
// up by one each time.
func demoBackOff(start, at time.Time, tick int) json.RawMessage {
	return demoEventSpec{
		"demo-backoff", "v1", "Pod", demoCrashPod, "Warning", "BackOff", "Back-off restarting failed container worker in pod " + demoCrashPod, "kubelet",
		212 + tick, start.Add(-72 * time.Hour), at,
	}.raw()
}

// demoNewEvent is the n-th new event of the demo stream: a rollout of web, step by step.
func demoNewEvent(at time.Time, n int) json.RawMessage {
	pod := fmt.Sprintf("web-7d9c4-%05d", n)
	steps := []demoEventSpec{
		{"", "v1", "Pod", pod, "Normal", "Scheduled", "Successfully assigned demo/" + pod + " to a worker node", "default-scheduler", 1, at, at},
		{"", "v1", "Pod", pod, "Normal", "Pulling", `Pulling image "nginx:1.29"`, "kubelet", 1, at, at},
		{"", "v1", "Pod", "web-7d9c4-abcde", "Warning", "Unhealthy", "Readiness probe failed: HTTP probe failed with statuscode: 503", "kubelet", 1, at, at},
		{"", "apps/v1", "Deployment", "web", "Normal", "ScalingReplicaSet", fmt.Sprintf("Scaled up replica set web-7d9c4 to %d", 2+n%3), "deployment-controller", 1, at, at},
	}

	s := steps[n%len(steps)]
	s.uid = fmt.Sprintf("demo-live-%d", n)

	return s.raw()
}
