package ichorgo

import (
	"fmt"
	"strings"
	"time"
)

// demoExplanation is the report of a demo subject, from the demo's pods, alerts and events: the
// CrashLooping worker is in all three.
func demoExplanation(kind string, s explainSubject, now time.Time) (explainData, error) {
	data := explainData{At: now, Kind: kind}

	switch kind {
	case explainPod:
		pod, ok := demoPod(s.Namespace, s.Name)
		if !ok {
			return data, fmt.Errorf("pods %q not found", s.Name)
		}

		data.Pod = &explainPodState{kubePod: pod, Phase: demoPodPhase(pod), ContainerStates: demoContainerStates(pod)}
		data.Events = demoEvents(pod.Namespace, "Pod", pod.Name, now).Events

		if !pod.Healthy {
			previous := pod.Restarts > 0
			data.LogFrom = "container " + demoContainerName(pod)

			if previous {
				data.LogFrom += ", previous run"
			}

			data.Log = explainLogLinesOf(demoPodLog(pod.Name, previous))
		}
	case explainAlert:
		for _, a := range demoAMAlerts(now) {
			if a.Fingerprint == s.Fingerprint {
				data.Alert = &a
			}
		}

		if data.Alert == nil {
			return data, errExplainAlertGone
		}

		if ns, objKind, name := alertObject(data.Alert.Labels); ns != "" && name != "" {
			data.Events = demoEvents(ns, objKind, name, now).Events
		}
	default:
		data.Finding = &s

		if ns, objKind, name := findingObject(s.Finding); ns != "" && name != "" {
			data.Events = demoEvents(ns, objKind, name, now).Events
		}
	}

	return data, nil
}

func demoPod(namespace, name string) (kubePod, bool) {
	for _, p := range demoPods() {
		if p.Namespace == namespace && p.Name == name {
			return p, true
		}
	}

	return kubePod{}, false
}

func demoPodPhase(p kubePod) string {
	if p.Status == "ImagePullBackOff" {
		return "Pending"
	}

	return "Running"
}

// demoContainerName is the demo pod's one container, named after its workload.
func demoContainerName(p kubePod) string {
	if len(p.ContainerNames) > 0 {
		return p.ContainerNames[0]
	}

	_, owner, _ := cutOwner(p.Owner)

	return owner
}

func demoContainerStates(p kubePod) []string {
	name := demoContainerName(p)

	switch {
	case p.Status == "CrashLoopBackOff":
		return []string{fmt.Sprintf("%s: waiting CrashLoopBackOff (back-off 5m0s restarting failed container), %d restarts, last ended Error (exit 2)", name, p.Restarts)}
	case !p.Healthy:
		return []string{name + ": waiting " + p.Status}
	default:
		return []string{name + ": running"}
	}
}

// cutOwner splits "ReplicaSet/worker-6f4b8" into its kind and its workload's name ("worker").
func cutOwner(owner string) (kind, workload string, ok bool) {
	kind, name, ok := strings.Cut(owner, "/")
	if kind == "ReplicaSet" {
		if i := strings.LastIndexByte(name, '-'); i > 0 {
			name = name[:i]
		}
	}

	return kind, name, ok
}
