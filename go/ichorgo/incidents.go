package ichorgo

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
)

const incidentLimit = 600
const incidentDetailLimit = 2048

type incidentEntry struct {
	ID             string       `json:"id"`
	At             int64        `json:"at"`
	Node           string       `json:"node"`
	Kind           string       `json:"kind"`
	Subject        string       `json:"subject"`
	Detail         string       `json:"detail"`
	Severity       string       `json:"severity"`
	Metrics        *bottlenecks `json:"metrics,omitempty"`
	MetricsOmitted int          `json:"metricsOmitted,omitempty"`
}
type incidentDocument struct {
	Scope     string             `json:"scope"`
	StartedAt int64              `json:"startedAt"`
	UpdatedAt int64              `json:"updatedAt"`
	Dropped   int                `json:"dropped"`
	Entries   []incidentEntry    `json:"entries"`
	Last      clusterObservation `json:"last"`
}

// UpdateIncident builds a bounded evidence timeline. Events retain their node clock;
// sampled changes use phone time. Their ordering is correlation, not proven causation.
// Call with an empty previousJSON for a new session, then persist every result locally.
func UpdateIncident(previousJSON, observationJSON, eventsJSON string) (out string, err error) {
	defer maskResult(&out, &err)
	var doc incidentDocument
	if previousJSON != "" {
		if err := json.Unmarshal([]byte(previousJSON), &doc); err != nil {
			return "", err
		}
	}
	var observation clusterObservation
	if err := json.Unmarshal([]byte(observationJSON), &observation); err != nil {
		return "", err
	}
	if doc.Scope != "" && doc.Scope != observation.Scope {
		return "", fmt.Errorf("recording belongs to another cluster")
	}
	if observation.At <= 0 || observation.Scope == "" {
		return "", fmt.Errorf("invalid observation")
	}
	if doc.StartedAt == 0 {
		doc.StartedAt = observation.At
		doc.Scope = observation.Scope
		doc.Entries = []incidentEntry{}
	}
	if doc.UpdatedAt > observation.At {
		return "", fmt.Errorf("observation is older than the recording")
	}
	seen := map[string]bool{}
	for _, e := range doc.Entries {
		seen[e.ID] = true
	}
	add := func(e incidentEntry) {
		if seen[e.ID] {
			return
		}
		seen[e.ID] = true
		e.Detail = clipUTF8(e.Detail, incidentDetailLimit)
		doc.Entries = append(doc.Entries, e)
	}
	previous := map[string]observedNode{}
	for _, n := range doc.Last.Nodes {
		previous[n.Status.Node] = n
	}
	for _, n := range observation.Nodes {
		node := n.Status.Node
		old, known := previous[node]
		entry := func(kind, subject, detail, severity string) {
			add(incidentEntry{ID: fmt.Sprintf("sample/%d/%s/%s/%s", observation.At, node, kind, subject), At: observation.At, Node: node, Kind: kind, Subject: subject, Detail: detail, Severity: severity})
		}
		status := func(n nodeOverview) string {
			raw, _ := toJSON(struct {
				Reachable  bool             `json:"reachable"`
				Ready      bool             `json:"ready"`
				Stage      string           `json:"stage"`
				Error      string           `json:"error"`
				Conditions []unmetCondition `json:"conditions"`
			}{n.Reachable, n.Ready, n.Stage, n.Error, n.UnmetConditions})
			return raw
		}
		if !known || status(old.Status) != status(n.Status) {
			severity := "info"
			if !n.Status.Ready || !n.Status.Reachable {
				severity = "warning"
			}
			entry("status", n.Status.Hostname, status(n.Status), severity)
		}
		for section, problem := range n.Errors {
			if !known || old.Errors[section] != problem {
				entry("error", section, problem, "warning")
			}
		}
		for section, problem := range old.Errors {
			if problem != "" && n.Errors[section] == "" && n.Status.Reachable {
				entry("recovered", section, "{}", "info")
			}
		}
		if n.Errors["links"] == "" && n.Status.Reachable {
			for link, state := range n.Links {
				if known && old.Errors["links"] == "" && old.Status.Reachable && old.Links[link] != state {
					raw, _ := toJSON(map[string]string{"before": old.Links[link], "after": state})
					entry("link", link, raw, "info")
				}
			}
		}
		if known && n.Errors["links"] == "" && old.Errors["links"] == "" && n.Status.Reachable && old.Status.Reachable {
			for link, before := range old.Links {
				if _, exists := n.Links[link]; !exists {
					raw, _ := toJSON(map[string]string{"before": before, "after": "absent"})
					entry("link", link, raw, "warning")
				}
			}
		}
		if n.Errors["services"] == "" && n.Status.Reachable {
			before := map[string]serviceInfo{}
			for _, s := range old.Services {
				before[s.ID] = s
			}
			if known && old.Errors["services"] == "" && old.Status.Reachable {
				currentServices := map[string]bool{}
				for _, s := range n.Services {
					currentServices[s.ID] = true
				}
				for _, s := range old.Services {
					if !currentServices[s.ID] {
						entry("service", s.ID, `{"state":"absent"}`, "warning")
					}
				}
			}
			for _, s := range n.Services {
				p, exists := before[s.ID]
				if !exists || p.State != s.State || p.Health != s.Health {
					raw, _ := toJSON(s)
					severity := "info"
					if s.Health == "unhealthy" {
						severity = "warning"
					}
					entry("service", s.ID, raw, severity)
				}
			}
		}
		if n.Stats != nil {
			if old.Stats != nil {
				rates := calculateBottlenecks(*old.Stats, *n.Stats)
				raw, _ := toJSON(struct {
					Counters *nodeStats  `json:"counters"`
					Rates    bottlenecks `json:"rates"`
				}{n.Stats, rates})
				// Keep a bounded, independently decodable presentation payload even when
				// the raw counter detail is truncated. Never turn missing rates into zeros.
				compact, omitted := incidentMetrics(rates)
				if n.Stats.At <= old.Stats.At || (old.Stats.BootTime != 0 && n.Stats.BootTime != 0 && old.Stats.BootTime != n.Stats.BootTime) {
					compact.Errors["sample"] = "Rates unavailable: samples overlap or the node restarted. Wait for two consecutive samples."
				}
				add(incidentEntry{ID: fmt.Sprintf("sample/%d/%s/metrics/%s", observation.At, node, n.Status.Hostname), At: observation.At, Node: node, Kind: "metrics", Subject: n.Status.Hostname, Detail: raw, Severity: "info", Metrics: &compact, MetricsOmitted: omitted})
			}
		}
	}
	var events []nodeEvent
	if eventsJSON != "" {
		if err := json.Unmarshal([]byte(eventsJSON), &events); err != nil {
			return "", err
		}
	}
	for _, e := range events {
		add(incidentEntry{ID: "event/" + e.Node + "/" + e.ID, At: e.At, Node: e.Node, Kind: "event", Subject: e.Kind + "/" + e.Subject + "/" + e.Action, Detail: e.Message, Severity: e.Severity})
	}
	sort.SliceStable(doc.Entries, func(i, j int) bool {
		if doc.Entries[i].At == doc.Entries[j].At {
			return doc.Entries[i].ID < doc.Entries[j].ID
		}
		return doc.Entries[i].At < doc.Entries[j].At
	})
	if len(doc.Entries) > incidentLimit {
		doc.Dropped += len(doc.Entries) - incidentLimit
		doc.Entries = append([]incidentEntry{}, doc.Entries[len(doc.Entries)-incidentLimit:]...)
	}
	doc.Last = observation
	doc.UpdatedAt = observation.At
	return toJSON(doc)
}

func incidentMetrics(rates bottlenecks) (bottlenecks, int) {
	const limit = 8
	omitted := 0
	clip := func(devices []deviceRate) []deviceRate {
		if len(devices) > limit {
			omitted += len(devices) - limit
			devices = devices[:limit]
		}
		out := append([]deviceRate{}, devices...)
		for i := range out {
			out[i].Name = clipUTF8(out[i].Name, 128)
		}
		return out
	}
	rates.Network = clip(rates.Network)
	rates.Disks = clip(rates.Disks)
	errors := map[string]string{}
	for i, key := range slices.Sorted(maps.Keys(rates.Errors)) {
		if i >= limit {
			omitted++
			continue
		}
		errors[clipUTF8(key, 128)] = clipUTF8(rates.Errors[key], 256)
	}
	rates.Errors = errors
	return rates, omitted
}

// clipUTF8 cuts s to n bytes plus an ellipsis, dropping a rune split by the cut.
