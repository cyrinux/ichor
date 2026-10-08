package ichorgo

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// How a condition, a phase or a whole object reads at a glance.
const (
	toneOK   = "ok"
	toneWarn = "warn"
	toneBad  = "bad"
	toneNone = "none"
)

var toneRank = map[string]int{toneNone: 0, toneOK: 1, toneWarn: 2, toneBad: 3}

// Conditions whose True is bad news: a node under pressure, a Deployment whose ReplicaSet
// failed, a Flux object stalled. Types ending as in negativeSuffixes are read the same.
var negativeConditions = map[string]bool{
	"Degraded": true, "Stalled": true, "Failed": true, "ReplicaFailure": true,
	"NetworkUnavailable": true, "Unschedulable": true,
}

var negativeSuffixes = []string{"Pressure", "Failed", "Failure", "Error", "Unavailable", "Degraded"}

// Conditions whose True means something is under way, not wrong: cert-manager issuing, a
// Flux object reconciling, a Job suspended.
var busyConditions = map[string]bool{
	"Reconciling": true, "Issuing": true, "Suspended": true, "Terminating": true, "Drifted": true,
}

// Reasons that make a False condition normal: a completed pod is no longer Ready.
var benignFalseReasons = map[string]bool{"PodCompleted": true}

// conditionTone reads one condition: Unknown is a warning; True is good unless the type
// names a problem (bad) or work in progress (warning); False is the opposite.
func conditionTone(c summaryCondition) string {
	negative := negativeConditions[c.Type] || slices.ContainsFunc(negativeSuffixes, func(s string) bool { return strings.HasSuffix(c.Type, s) })
	busy := busyConditions[c.Type]

	switch c.Status {
	case "True":
		switch {
		case negative:
			return toneBad
		case busy:
			return toneWarn
		default:
			return toneOK
		}
	case "False":
		if negative || busy || benignFalseReasons[c.Reason] {
			return toneOK
		}

		return toneBad
	case "Unknown":
		return toneWarn
	default:
		return toneNone
	}
}

var (
	okPhases   = []string{"Running", "Succeeded", "Active", "Bound", "Available", "Ready", "Healthy", "Complete", "Completed"}
	warnPhases = []string{"Pending", "Terminating", "Unknown", "Released", "Progressing"}
	badPhases  = []string{"Failed", "Lost", "Error", "Degraded"}
)

func phaseTone(phase string) string {
	switch {
	case slices.Contains(okPhases, phase):
		return toneOK
	case slices.Contains(warnPhases, phase):
		return toneWarn
	case slices.Contains(badPhases, phase):
		return toneBad
	default:
		return toneNone
	}
}

// objectHealth is the worst condition's tone and what it says ("Ready: CrashLoopBackOff"),
// the phase's without conditions, and at least a warning while a deletion is pending.
func objectHealth(conditions []summaryCondition, phase string, deleting bool) (string, string) {
	health, reason := toneNone, ""

	for _, c := range conditions {
		if toneRank[c.Tone] > toneRank[health] {
			health, reason = c.Tone, c.Type
			if c.Reason != "" {
				reason += ": " + c.Reason
			}
		}
	}

	if health == toneNone && phase != "" {
		health, reason = phaseTone(phase), phase
	}

	if deleting && toneRank[health] < toneRank[toneWarn] {
		health, reason = toneWarn, "Terminating"
	}

	return health, reason
}

// readConditions maps status.conditions, worst first then by type; entries without a type
// are left out.
func readConditions(status map[string]any) []summaryCondition {
	raw, _ := status["conditions"].([]any)
	out := make([]summaryCondition, 0, len(raw))

	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok || stringAt(m, "type") == "" {
			continue
		}

		message := strings.TrimSpace(stringAt(m, "message"))
		if len(message) > eventMessageLimit {
			message = strings.ToValidUTF8(message[:eventMessageLimit], "") + "…"
		}

		c := summaryCondition{
			Type: stringAt(m, "type"), Status: stringAt(m, "status"), Reason: stringAt(m, "reason"), Message: message,
			LastTransition: timeMilli(cmp.Or(stringAt(m, "lastTransitionTime"), stringAt(m, "lastUpdateTime"), stringAt(m, "lastProbeTime"))),
		}
		c.Tone = conditionTone(c)
		out = append(out, c)
	}

	sortConditions(out)

	return out
}

// sortConditions puts the worst first, then sorts by type.
func sortConditions(conditions []summaryCondition) {
	slices.SortStableFunc(conditions, func(a, b summaryCondition) int {
		return cmp.Or(toneRank[b.Tone]-toneRank[a.Tone], cmp.Compare(a.Type, b.Type))
	})
}

// Highlight keys, labelled by the apps.
const (
	highlightReplicas = "replicas"
	highlightSelector = "selector"
	highlightImage    = "image"
	highlightNode     = "node"
	highlightSuspend  = "suspended"
	highlightSchedule = "schedule"
)

// specHighlights are the spec fields most kinds share and a reader looks for first:
// replicas (ready/desired), selector, schedule, suspension, node and container images.
func specHighlights(obj map[string]any) []kubeHighlight {
	spec, status := mapAt(obj, "spec"), mapAt(obj, "status")
	out := []kubeHighlight{}

	if replicas, ok := numberAt(spec, "replicas"); ok {
		value := fmt.Sprint(replicas)
		if ready, ok := numberAt(status, "readyReplicas"); ok {
			value = fmt.Sprintf("%d/%d", ready, replicas)
		} else if _, hasStatus := status["replicas"]; hasStatus {
			value = fmt.Sprintf("0/%d", replicas)
		}

		out = append(out, kubeHighlight{highlightReplicas, value})
	}

	if selector := selectorText(spec["selector"]); selector != "" {
		out = append(out, kubeHighlight{highlightSelector, selector})
	}

	if schedule := stringAt(spec, "schedule"); schedule != "" {
		out = append(out, kubeHighlight{highlightSchedule, schedule})
	}

	if suspend, _ := spec["suspend"].(bool); suspend {
		out = append(out, kubeHighlight{highlightSuspend, "true"})
	}

	if node := stringAt(spec, "nodeName"); node != "" {
		out = append(out, kubeHighlight{highlightNode, node})
	}

	for _, image := range containerImages(spec) {
		out = append(out, kubeHighlight{highlightImage, image})
	}

	return out
}

// selectorText is a label selector as kubectl prints it ("app=web,tier in (a,b)"): a
// matchLabels/matchExpressions one, a Service's plain map, or a string one.
func selectorText(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case map[string]any:
		if _, structured := v["matchLabels"]; !structured {
			if _, expr := v["matchExpressions"]; !expr {
				return labelsText(stringMap(v))
			}
		}

		parts := []string{}
		if labels := labelsText(stringMap(mapAt(v, "matchLabels"))); labels != "" {
			parts = append(parts, labels)
		}

		exprs, _ := v["matchExpressions"].([]any)
		for _, e := range exprs {
			m, _ := e.(map[string]any)
			parts = append(parts, expressionText(m))
		}

		return strings.Join(parts, ",")
	default:
		return ""
	}
}

func expressionText(m map[string]any) string {
	key, op := stringAt(m, "key"), strings.ToLower(stringAt(m, "operator"))

	switch op {
	case "exists":
		return key
	case "doesnotexist":
		return "!" + key
	}

	return key + " " + op + " (" + strings.Join(stringList(m["values"]), ",") + ")"
}

func labelsText(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for _, key := range slices.Sorted(maps.Keys(labels)) {
		parts = append(parts, key+"="+labels[key])
	}

	return strings.Join(parts, ",")
}

// containerImages are the images of the pod spec at spec (a Pod), its template (a
// Deployment, a Job) or a CronJob's job template, without duplicates.
func containerImages(spec map[string]any) []string {
	podSpec := spec
	if _, ok := spec["containers"]; !ok {
		podSpec = mapAt(mapAt(spec, "template"), "spec")
	}

	if _, ok := podSpec["containers"]; !ok {
		podSpec = mapAt(mapAt(mapAt(mapAt(spec, "jobTemplate"), "spec"), "template"), "spec")
	}

	containers, _ := podSpec["containers"].([]any)
	images := []string{}

	for _, c := range containers {
		m, _ := c.(map[string]any)
		if image := stringAt(m, "image"); image != "" && !slices.Contains(images, image) {
			images = append(images, image)
		}
	}

	return images
}

// Readers of decoded JSON: absent or mistyped fields read as zero values.

func mapAt(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)

	return v
}

func stringAt(m map[string]any, key string) string {
	v, _ := m[key].(string)

	return v
}

func numberAt(m map[string]any, key string) (int64, bool) {
	v, ok := m[key].(float64)

	return int64(v), ok
}

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))

	for key, v := range m {
		if s, ok := v.(string); ok {
			out[key] = s
		}
	}

	return out
}

func stringList(raw any) []string {
	items, _ := raw.([]any)
	out := make([]string, 0, len(items))

	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

// timeMilli is an RFC 3339 timestamp in Unix milliseconds, 0 when absent or unreadable.
func timeMilli(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}

	return t.UnixMilli()
}
