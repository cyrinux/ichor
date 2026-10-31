package ichorgo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The fleet row's top finding: the first problem in a fixed order (nodes, etcd, then the issue
// tracks), worded from the snapshot's keys and values. A Talos snapshot keys its nodes (and
// its storage and sensor issues) by endpoint, so the subjects use the hostnames the values
// carry and never a key's node part.

const (
	fleetCritical = "critical"
	fleetWarning  = "warning"
)

// The tracks a finding can come from.
const (
	fleetTrackNodes    = "nodes"
	fleetTrackEtcd     = "etcd"
	fleetTrackReach    = "reach"
	fleetTrackAm       = "alertmanager"
	fleetTrackKubespan = "kubespan"
	fleetTrackCheckup  = "checkup"
	fleetTrackData     = "data"
	fleetTrackGitOps   = "gitops"
	fleetTrackStorage  = "storage"
	fleetTrackSensor   = "sensor"
	fleetTrackPlugin   = "plugin"
)

// fleetTrack is one opt-in issue track of a snapshot.
type fleetTrack struct {
	name    string
	checked bool
	issues  map[string]string
	subject func(key, value string) (subject, detail string)
}

// fleetTracksOf is the snapshot's issue tracks in the order their findings rank.
func fleetTracksOf(e fleetEntry) []fleetTrack {
	return []fleetTrack{
		{fleetTrackAm, e.AmChecked, e.AmIssues, fleetAmSubject},
		{fleetTrackKubespan, e.KubespanChecked, e.KubespanIssues, fleetKubespanSubject},
		{fleetTrackCheckup, e.CheckupChecked, e.CheckupIssues, fleetCheckupSubject},
		{fleetTrackData, e.DataChecked, e.DataIssues, fleetDataSubject},
		{fleetTrackGitOps, e.GitopsChecked, e.GitopsIssues, fleetGitOpsSubject},
		{fleetTrackStorage, e.StorageChecked, e.StorageIssues, fleetStorageSubject},
		{fleetTrackSensor, e.SensorChecked, e.SensorIssues, fleetSensorSubject},
		{fleetTrackPlugin, e.PluginChecked, e.PluginIssues, fleetPluginSubject},
	}
}

// fleetSeverity is an issue value's severity: its prefix up to the first "|" or ":" (a
// plugin's JSON value: its "severity" field); anything but critical is a warning.
func fleetSeverity(value string) string {
	severity, _ := fleetSplitSeverity(value)

	if strings.HasPrefix(value, "{") {
		var v struct {
			Severity string `json:"severity"`
		}
		if json.Unmarshal([]byte(value), &v) == nil {
			severity = v.Severity
		}
	}

	if severity == fleetCritical {
		return fleetCritical
	}

	return fleetWarning
}

// fleetSplitSeverity cuts value at its first "|" or ":" (Android writes "severity|…", iOS's
// GitOps track "severity:…").
func fleetSplitSeverity(value string) (severity, rest string) {
	i := strings.IndexAny(value, "|:")
	if i < 0 {
		return value, ""
	}

	return value[:i], value[i+1:]
}

func fleetTopFinding(e fleetEntry, row fleetRow) *fleetFinding {
	if f := fleetNodeFinding(e, row); f != nil {
		return f
	}

	if row.EtcdAlarms > 0 {
		return &fleetFinding{Track: fleetTrackEtcd, Severity: fleetCritical, Subject: fleetEtcdSubject(e.EtcdAlarms)}
	}

	for _, severity := range []string{fleetCritical, fleetWarning} {
		for _, t := range fleetTracksOf(e) {
			if f := fleetTrackFinding(t, severity); f != nil {
				return f
			}
		}
	}

	return nil
}

// fleetTrackFinding is the first issue of t with severity, keys sorted; nil when t was not
// checked this time or has none.
func fleetTrackFinding(t fleetTrack, severity string) *fleetFinding {
	if !t.checked {
		return nil
	}

	keys := make([]string, 0, len(t.issues))
	for k := range t.issues {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	for _, k := range keys {
		if fleetSeverity(t.issues[k]) != severity {
			continue
		}

		subject, detail := t.subject(k, t.issues[k])

		return &fleetFinding{Track: t.name, Severity: severity, Subject: subject, Detail: detail}
	}

	return nil
}

// fleetNodeFinding: no node answered checks in a row (reach), else the unreachable nodes,
// else the nodes not ready.
func fleetNodeFinding(e fleetEntry, row fleetRow) *fleetFinding {
	switch {
	case row.Nodes > 0 && row.Unreachable == row.Nodes && e.Failures >= 1:
		return &fleetFinding{Track: fleetTrackReach, Severity: fleetCritical, Subject: fleetReachSubject(e.Failures)}
	case row.Unreachable > 0:
		return &fleetFinding{Track: fleetTrackNodes, Severity: fleetCritical, Subject: fleetNodesSubject(e, "unreachable", row.Unreachable, row.Nodes)}
	case row.NotReady > 0:
		return &fleetFinding{Track: fleetTrackNodes, Severity: fleetWarning, Subject: fleetNodesSubject(e, "notReady", row.NotReady, row.Nodes)}
	default:
		return nil
	}
}

func fleetReachSubject(failures int) string {
	if failures == 1 {
		return "no node answered the last check"
	}

	return fmt.Sprintf("no node answered the last %d checks", failures)
}

// fleetNodesSubject: "node-b unreachable" for one node with a hostname, else "2 of 3 nodes
// unreachable" (never the node's key: on Talos it is its endpoint).
func fleetNodesSubject(e fleetEntry, health string, count, total int) string {
	words := map[string]string{"unreachable": "unreachable", "notReady": "not ready"}[health]

	if count == 1 {
		for _, n := range e.Nodes {
			if fleetHealth(n.Health) == health && strings.TrimSpace(n.Hostname) != "" {
				return strings.TrimSpace(n.Hostname) + " " + words
			}
		}
	}

	return fmt.Sprintf("%d of %d nodes %s", count, total, words)
}

// fleetEtcdSubject names the alarms ("NOSPACE"), from "member:alarm" or a bare alarm.
func fleetEtcdSubject(alarms []string) string {
	var names []string

	for _, a := range alarms {
		name := strings.TrimSpace(a[strings.LastIndex(a, ":")+1:])
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	slices.Sort(names)

	if len(names) == 0 {
		return "etcd alarm"
	}

	return strings.Join(names, ", ")
}

// fleetAmSubject: "alertname · where" from "severity|alertname|where".
func fleetAmSubject(key, value string) (string, string) {
	_, rest := fleetSplitSeverity(value)
	alertname, where, _ := strings.Cut(rest, "|")

	return fleetJoin(" · ", alertname, where), ""
}

// fleetKubespanSubject: "host-a ↔ host-b" from "severity|reason|since|host-a|host-b", the reason as
// the detail; a value too short to carry the hosts falls back to the key's two node ids.
func fleetKubespanSubject(key, value string) (string, string) {
	parts := strings.Split(value, "|")
	if len(parts) >= 5 {
		return fleetJoin(" ↔ ", parts[3], parts[4]), parts[1]
	}

	return strings.ReplaceAll(key, "|", " ↔ "), ""
}

// fleetCheckupSubject: "kind subject" from "section|kind|subject".
func fleetCheckupSubject(key, _ string) (string, string) {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) < 3 {
		return key, ""
	}

	return fleetJoin(" ", parts[1], parts[2]), ""
}

// fleetDataSubject: "system label" from "system|label".
func fleetDataSubject(key, _ string) (string, string) {
	system, label, _ := strings.Cut(key, "|")

	return fleetJoin(" ", system, label), ""
}

// fleetGitOpsSubject: "ns/name (reason)" from "argocd|ns/name" or "flux|Kind ns/name", the
// tool as the detail.
func fleetGitOpsSubject(key, value string) (string, string) {
	tool, app, ok := strings.Cut(key, "|")
	if !ok {
		return key, ""
	}

	if tool == "flux" {
		if _, name, found := strings.Cut(app, " "); found {
			app = name
		}
	}

	if _, reason := fleetSplitSeverity(value); reason != "" {
		app += " (" + reason + ")"
	}

	return app, tool
}

// fleetStorageSubject: "hostname volume" from the value "severity|fill|hostname|volume|…" or
// "severity|smart|hostname|device|…".
func fleetStorageSubject(key, value string) (string, string) {
	return fleetHostSubject(key, value, func(parts []string) string { return parts[3] }), ""
}

// fleetSensorSubject: "hostname hot" or "hostname throttled" from "severity|hot|hostname|…".
func fleetSensorSubject(key, value string) (string, string) {
	return fleetHostSubject(key, value, func(parts []string) string { return parts[1] }), ""
}

// fleetHostSubject is the value's hostname (its third part) and what pick takes from it; a
// value too short to carry one falls back to the key without its node part.
func fleetHostSubject(key, value string, pick func([]string) string) string {
	parts := strings.Split(value, "|")
	if len(parts) >= 4 && strings.TrimSpace(parts[2]) != "" {
		return fleetJoin(" ", parts[2], pick(parts))
	}

	_, rest, _ := strings.Cut(key, "|")

	return strings.ReplaceAll(rest, "|", " ")
}

// fleetPluginSubject is the plugin finding's key as is.
func fleetPluginSubject(key, _ string) (string, string) {
	return key, ""
}

// fleetJoin joins the non-empty trimmed parts with sep.
func fleetJoin(sep string, parts ...string) string {
	var kept []string

	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}

	return strings.Join(kept, sep)
}
