package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// explainWorkloadLabels are the alert labels that name a workload whose events tell more.
var explainWorkloadLabels = []string{"deployment", "statefulset", "daemonset", "job_name", "cronjob"}

// errExplainAlertGone is an alert Alertmanager no longer holds: resolved since the list was read.
var errExplainAlertGone = errors.New("the alert is no longer in Alertmanager: it was resolved or expired, refresh the list")

func collectExplanation(ctx context.Context, target kubeTarget, kind string, s explainSubject) (explainData, error) {
	switch kind {
	case explainPod:
		return withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (explainData, error) {
			return collectExplainPod(ctx, k, s.Namespace, s.Name)
		})
	case explainAlert:
		return collectExplainAlert(ctx, target, s)
	default:
		data := explainData{At: amNow(), Kind: explainFinding, Finding: &s}
		ns, objKind, name := findingObject(s.Finding)

		if ns == "" || name == "" {
			return data, nil
		}

		return withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (explainData, error) {
			data.Events, data.EventsNote = explainEventsOf(ctx, k, ns, objKind, name)

			return data, nil
		})
	}
}

// collectExplainPod reads the pod, then its events and the log of the container in trouble at once.
// Only the pod itself is required: without it the report would be about nothing.
func collectExplainPod(ctx context.Context, k *kubeClient, namespace, name string) (explainData, error) {
	data := explainData{At: amNow(), Kind: explainPod}

	var obj podObject
	if err := k.get(ctx, podPath(namespace, name), &obj); err != nil {
		return data, kubeError(err)
	}

	data.Pod = &explainPodState{kubePod: mapPod(obj), Phase: obj.Status.Phase, ContainerStates: containerLines(obj)}

	container, previous := troubledContainer(obj)

	var wg sync.WaitGroup

	wg.Go(func() { data.Events, data.EventsNote = explainEventsOf(ctx, k, namespace, "Pod", name) })

	if container != "" {
		wg.Go(func() {
			data.Log, data.LogFrom, data.LogNote = explainLog(ctx, k, namespace, name, container, previous)
		})
	}

	wg.Wait()

	return data, nil
}

// troubledContainer is the first container that is not running, and whether its previous run
// is the one to read (it restarted): the log that says why. "" when every container runs.
func troubledContainer(obj podObject) (string, bool) {
	statuses := slices.Concat(obj.Status.InitContainerStatuses, obj.Status.ContainerStatuses)

	for _, cs := range statuses {
		if cs.Ready || cs.State.Waiting == nil && cs.State.Terminated == nil {
			continue
		}

		return cs.Name, cs.RestartCount > 0 && cs.LastState.Terminated != nil
	}

	return "", false
}

// containerLines says each container's state, as `kubectl describe pod` does in many more lines.
func containerLines(obj podObject) []string {
	var lines []string

	for _, cs := range slices.Concat(obj.Status.InitContainerStatuses, obj.Status.ContainerStatuses) {
		line := cs.Name + ": " + containerStateText(cs.State)

		if cs.RestartCount > 0 {
			line += fmt.Sprintf(", %d restarts", cs.RestartCount)
		}

		if t := cs.LastState.Terminated; t != nil {
			line += fmt.Sprintf(", last ended %s (exit %d)", terminatedReason(t.Reason, t.Signal, t.ExitCode), t.ExitCode)
		}

		lines = append(lines, clipUTF8(line, diagnosisMaxLineLen))
	}

	return lines
}

func containerStateText(s containerState) string {
	switch {
	case s.Waiting != nil && s.Waiting.Message != "":
		return "waiting " + s.Waiting.Reason + " (" + strings.TrimSpace(s.Waiting.Message) + ")"
	case s.Waiting != nil:
		return "waiting " + s.Waiting.Reason
	case s.Terminated != nil:
		return fmt.Sprintf("terminated %s (exit %d)", terminatedReason(s.Terminated.Reason, s.Terminated.Signal, s.Terminated.ExitCode), s.Terminated.ExitCode)
	default:
		return "running"
	}
}

// explainLog reads the container's last lines without their colours; a refusal is a note.
func explainLog(ctx context.Context, k *kubeClient, namespace, pod, container string, previous bool) ([]string, string, string) {
	from := "container " + container
	if previous {
		from += ", previous run"
	}

	query := podLogQuery(container, previous, explainLogLines, 0)

	text, err := k.getText(ctx, podPath(namespace, pod)+"/log?"+query.Encode())

	switch {
	case isForbidden(err):
		return nil, from, "logs not readable: the credentials may not read pods/log"
	case err != nil:
		return nil, from, "logs could not be read: " + clipUTF8(kubeError(err).Error(), diagnosisMaxLineLen)
	}

	return explainLogLinesOf(text), from, ""
}

// explainLogLinesOf is the log's last lines, without ANSI escapes and each clipped.
func explainLogLinesOf(text string) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > explainLogLines {
		lines = lines[len(lines)-explainLogLines:]
	}

	out := make([]string, 0, len(lines))

	for _, l := range lines {
		plain, _ := stripANSI(strings.TrimRight(l, "\r"))
		out = append(out, clipUTF8(plain, diagnosisMaxLineLen))
	}

	if len(out) == 1 && out[0] == "" {
		return nil
	}

	return out
}

// explainEventsOf reads the newest events of one object (kind "": name and what it owns).
func explainEventsOf(ctx context.Context, k *kubeClient, namespace, kind, name string) ([]kubeEvent, string) {
	list, err := readEvents(ctx, k, namespace, kind, name)
	if err != nil {
		return nil, sectionNote("events", err)
	}

	// The field selector already keeps this object's alone; an API server that ignores it must not
	// put another object's events in the report.
	events := slices.DeleteFunc(list.Events, func(e kubeEvent) bool { return kind != "" && (e.Kind != kind || e.Name != name) })
	if len(events) > explainEvents {
		events = events[:explainEvents]
	}

	return events, ""
}

// collectExplainAlert finds the alert by its fingerprint among every alert Alertmanager holds
// (its API has no read of one alert), then the events of the pod or workload its labels name.
func collectExplainAlert(ctx context.Context, target kubeTarget, s explainSubject) (explainData, error) {
	data := explainData{At: amNow(), Kind: explainAlert}

	src, err := parsePromSource(s.sourceJSON())
	if err != nil {
		return data, err
	}

	params := url.Values{"active": {"true"}, "unprocessed": {"true"}, "silenced": {"true"}, "inhibited": {"true"}}

	body, err := amCall(target, src, http.MethodGet, "/api/v2/alerts?"+params.Encode(), nil)
	if err != nil {
		return data, err
	}

	alert, err := findAMAlert(body, s.Fingerprint)
	if err != nil {
		return data, fmt.Errorf("%s: %w", src.label(), err)
	}

	data.Alert = &alert

	ns, kind, name := alertObject(alert.Labels)
	if ns == "" || name == "" {
		return data, nil
	}

	events, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (explainData, error) {
		var e explainData
		e.Events, e.EventsNote = explainEventsOf(ctx, k, ns, kind, name)

		return e, nil
	})
	if err != nil {
		data.EventsNote = sectionNote("events", err)
	} else {
		data.Events, data.EventsNote = events.Events, events.EventsNote
	}

	return data, nil
}

func findAMAlert(body []byte, fingerprint string) (amAlert, error) {
	// Not through parseAMAlerts: it keeps the first amMaxAlerts, and this one may be past them.
	var raw []amAPIAlert
	if err := json.Unmarshal(body, &raw); err != nil {
		return amAlert{}, fmt.Errorf("not an Alertmanager answer: %w", err)
	}

	for _, a := range raw {
		if a.Fingerprint == fingerprint {
			return a.toAlert(), nil
		}
	}

	return amAlert{}, errExplainAlertGone
}

// alertObject is the object whose events explain an alert: its pod, else the workload it names.
func alertObject(labels map[string]string) (namespace, kind, name string) {
	namespace = labels["namespace"]

	if pod := labels["pod"]; pod != "" {
		return namespace, "Pod", pod
	}

	for _, label := range explainWorkloadLabels {
		if name := labels[label]; name != "" {
			return namespace, "", name
		}
	}

	return namespace, "", ""
}
