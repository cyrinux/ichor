package ichorgo

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Fleet view: the apps keep one background-monitor snapshot per cluster. FleetSummary turns
// them into one ranked list, worst first, with a tone, the counts and the one thing that is
// wrong per cluster, so the rule is written (and tested) once instead of once per app. It
// calls no cluster and keeps no state, like SearchSnapshots.

// fleetStaleAfterMillis is how old a check may be before its row is "unknown": the widgets'
// WIDGET_STALE_AFTER_MS (Android) and glanceStaleAfter (iOS).
const fleetStaleAfterMillis = 60 * 60 * 1000

// The tones of a row, worst first.
const (
	fleetBad     = "bad"
	fleetWarn    = "warn"
	fleetUnknown = "unknown"
	fleetOK      = "ok"
)

// fleetTones is every tone, worst first: the order of the rows and of the worst tone.
var fleetTones = []string{fleetBad, fleetWarn, fleetUnknown, fleetOK}

// fleetNode is one node of a snapshot; health in either app's spelling.
type fleetNode struct {
	Hostname string `json:"hostname"`
	Health   string `json:"health"`
}

// fleetEntry is one cluster's last background check as the apps build it from their stored
// snapshot (Android ClusterSnapshot + Reach, iOS ClusterSnapshot + Reachability).
type fleetEntry struct {
	Key         string               `json:"key"`
	Name        string               `json:"name"`
	Context     string               `json:"context"`
	Kube        bool                 `json:"kube"`
	Tags        []string             `json:"tags"`
	TakenAt     int64                `json:"takenAt"`
	Failures    int                  `json:"failures"`
	Nodes       map[string]fleetNode `json:"nodes"`
	EtcdChecked bool                 `json:"etcdChecked"`
	EtcdAlarms  []string             `json:"etcdAlarms"`

	AmChecked      bool              `json:"amChecked"`
	AmIssues       map[string]string `json:"amIssues"`
	CheckupChecked bool              `json:"checkupChecked"`
	CheckupIssues  map[string]string `json:"checkupIssues"`
	DataChecked    bool              `json:"dataChecked"`
	DataIssues     map[string]string `json:"dataIssues"`
	GitopsChecked  bool              `json:"gitopsChecked"`
	GitopsIssues   map[string]string `json:"gitopsIssues"`
	StorageChecked bool              `json:"storageChecked"`
	StorageIssues  map[string]string `json:"storageIssues"`
	SensorChecked  bool              `json:"sensorChecked"`
	SensorIssues   map[string]string `json:"sensorIssues"`
	PluginChecked  bool              `json:"pluginChecked"`
	PluginIssues   map[string]string `json:"pluginIssues"`
}

// fleetFinding is the one thing a row says is wrong. Detail is the GitOps tool ("argocd",
// "flux") and empty elsewhere.
type fleetFinding struct {
	Track    string `json:"track"`
	Severity string `json:"severity"`
	Subject  string `json:"subject"`
	Detail   string `json:"detail,omitempty"`
}

type fleetRow struct {
	Key           string        `json:"key"`
	Name          string        `json:"name"`
	Context       string        `json:"context"`
	Kube          bool          `json:"kube"`
	Tags          []string      `json:"tags"`
	Tone          string        `json:"tone"`
	Stale         bool          `json:"stale"`
	CheckedAt     int64         `json:"checkedAt"`
	Nodes         int           `json:"nodes"`
	Ready         int           `json:"ready"`
	NotReady      int           `json:"notReady"`
	Unreachable   int           `json:"unreachable"`
	EtcdAlarms    int           `json:"etcdAlarms"`
	Critical      int           `json:"critical"`
	Warnings      int           `json:"warnings"`
	TracksChecked []string      `json:"tracksChecked"`
	TopFinding    *fleetFinding `json:"topFinding"`
}

type fleetCounts struct {
	Bad     int `json:"bad"`
	Warn    int `json:"warn"`
	Unknown int `json:"unknown"`
	OK      int `json:"ok"`
}

type fleetGroup struct {
	Tag    string      `json:"tag"`
	Counts fleetCounts `json:"counts"`
	Keys   []string    `json:"keys"`
}

type fleetSummary struct {
	Clusters []fleetRow   `json:"clusters"`
	Counts   fleetCounts  `json:"counts"`
	Worst    string       `json:"worst"`
	Groups   []fleetGroup `json:"groups"`
}

// FleetSummary ranks the clusters of snapshotsJSON ({"clusters":[fleetEntry…]}) worst first,
// as of nowMillis (unix ms): per cluster a tone (bad, warn, unknown, ok), the node and issue
// counts and the top finding; then the tone counts, the worst tone ("unknown" for an empty
// fleet) and one group per tag (untagged clusters last, under tag ""). takenAt is unix ms, an
// integer (iOS must not send its default Date encoding); 0 means never checked. A node's
// health is READY/ready, NOT_READY/notReady or UNREACHABLE/unreachable (anything else counts
// as unreachable); an issue value's severity is its prefix up to the first "|" or ":"
// (a plugin's JSON value: its "severity"), and anything but "critical" is a warning. Tracks
// not checked this time never count. The names come from the apps already masked in
// screenshot mode and the contexts are handed back as given (the apps switch to them), so the
// result is not masked again (like SearchSnapshots); no endpoint or address is added to it.
func FleetSummary(snapshotsJSON string, nowMillis int64) (out string, err error) {
	defer maskErr(&err)

	var in struct {
		Clusters []fleetEntry `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(snapshotsJSON), &in); err != nil {
		return "", fmt.Errorf("invalid fleet snapshots: %w", err)
	}

	return toJSON(summarizeFleet(in.Clusters, nowMillis))
}

func summarizeFleet(entries []fleetEntry, nowMillis int64) fleetSummary {
	rows := make([]fleetRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, fleetRowOf(e, nowMillis))
	}

	slices.SortStableFunc(rows, compareFleetRows)

	out := fleetSummary{Clusters: rows, Worst: fleetUnknown, Groups: fleetGroups(rows)}
	for _, r := range rows {
		out.Counts = out.Counts.add(r.Tone)
	}

	if len(rows) > 0 {
		out.Worst = rows[0].Tone
	}

	return out
}

func fleetRowOf(e fleetEntry, nowMillis int64) fleetRow {
	row := fleetRow{
		Key: e.Key, Name: e.Name, Context: e.Context, Kube: e.Kube, Tags: fleetTags(e.Tags),
		CheckedAt: e.TakenAt, Stale: e.TakenAt <= 0 || nowMillis-e.TakenAt > fleetStaleAfterMillis,
		TracksChecked: []string{},
	}

	for _, n := range e.Nodes {
		switch fleetHealth(n.Health) {
		case "ready":
			row.Ready++
		case "notReady":
			row.NotReady++
		default:
			row.Unreachable++
		}
	}

	row.Nodes = len(e.Nodes)

	if fleetEtcdChecked(e) {
		row.TracksChecked = append(row.TracksChecked, fleetTrackEtcd)
		row.EtcdAlarms = len(e.EtcdAlarms)
	}

	for _, t := range fleetTracksOf(e) {
		if !t.checked {
			continue
		}

		row.TracksChecked = append(row.TracksChecked, t.name)

		for _, v := range t.issues {
			if fleetSeverity(v) == fleetCritical {
				row.Critical++
			} else {
				row.Warnings++
			}
		}
	}

	row.Tone = fleetToneOf(row, e)
	row.TopFinding = fleetTopFinding(e, row)

	return row
}

// fleetToneOf applies the widgets' rules plus the issue tracks: a stale or never-made check
// is unknown whatever it saw (its counts stay on the row).
func fleetToneOf(row fleetRow, e fleetEntry) string {
	switch {
	case row.Stale:
		return fleetUnknown
	case row.Unreachable > 0, row.EtcdAlarms > 0, row.Critical > 0,
		e.Failures >= 1 && row.Nodes > 0 && row.Unreachable == row.Nodes:
		return fleetBad
	case row.NotReady > 0, row.Warnings > 0:
		return fleetWarn
	default:
		return fleetOK
	}
}

// fleetHealth is a node's health in the iOS spelling; an unknown one is unreachable.
func fleetHealth(h string) string {
	switch h {
	case "READY", "ready":
		return "ready"
	case "NOT_READY", "notReady":
		return "notReady"
	default:
		return "unreachable"
	}
}

// fleetEtcdChecked: the etcd alarms were read this time (a kubeconfig cluster has no etcd).
func fleetEtcdChecked(e fleetEntry) bool {
	return e.EtcdChecked && !e.Kube
}

// fleetTags is a row's tags: trimmed, without empties or repeats, never null.
func fleetTags(tags []string) []string {
	out := []string{}

	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}

	return out
}

func compareFleetRows(a, b fleetRow) int {
	return cmp.Or(
		cmp.Compare(slices.Index(fleetTones, a.Tone), slices.Index(fleetTones, b.Tone)),
		cmp.Compare(b.Critical, a.Critical),
		cmp.Compare(b.Warnings, a.Warnings),
		cmp.Compare(b.Unreachable+b.NotReady, a.Unreachable+a.NotReady),
		cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.Key, b.Key),
	)
}

func (c fleetCounts) add(tone string) fleetCounts {
	switch tone {
	case fleetBad:
		c.Bad++
	case fleetWarn:
		c.Warn++
	case fleetUnknown:
		c.Unknown++
	default:
		c.OK++
	}

	return c
}

// fleetGroups is one group per tag, in the order the ranked rows first name it, with the
// untagged clusters last under tag "" (absent when every cluster has a tag).
func fleetGroups(rows []fleetRow) []fleetGroup {
	groups := []fleetGroup{}
	untagged := fleetGroup{Keys: []string{}}

	for _, r := range rows {
		if len(r.Tags) == 0 {
			untagged.Counts = untagged.Counts.add(r.Tone)
			untagged.Keys = append(untagged.Keys, r.Key)

			continue
		}

		for _, tag := range r.Tags {
			i := slices.IndexFunc(groups, func(g fleetGroup) bool { return g.Tag == tag })
			if i < 0 {
				groups = append(groups, fleetGroup{Tag: tag, Keys: []string{}})
				i = len(groups) - 1
			}

			groups[i].Counts = groups[i].Counts.add(r.Tone)
			groups[i].Keys = append(groups[i].Keys, r.Key)
		}
	}

	if len(untagged.Keys) > 0 {
		groups = append(groups, untagged)
	}

	return groups
}
