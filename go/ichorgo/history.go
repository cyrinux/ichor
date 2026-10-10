package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The 30-day history ring: the background monitors append one record per cluster and run,
// the history screen and the "since you last looked" card query it, and the support bundle
// exports it anonymised. Go owns the format only: the ring is plain bytes, which each app
// seals with its own encrypted store (the keys live in the platform keystore), one ring per
// cluster fingerprint.

const (
	// historyKeep is how long records are kept.
	historyKeep = 30 * 24 * time.Hour
	// historyFullDetail is how long every record is kept; older ones are thinned to one per
	// hour, plus those where a node's state, a version or the open alerts changed.
	historyFullDetail = 7 * 24 * time.Hour
	// historyMaxBytes caps an encoded ring: the oldest records go first.
	historyMaxBytes = 512 << 10
	// historyMaxPoints caps each series of a query.
	historyMaxPoints = 200
)

// Node health in a record, as the monitors' snapshots hold it.
const (
	historyReady       = "ready"
	historyNotReady    = "notReady"
	historyUnreachable = "unreachable"
)

// Limits on one record: a record is a monitor run, not a free-form document.
const (
	historyMaxNodes   = 500
	historyMaxVolumes = 2000
	historyMaxAlerts  = 500
	historyMaxName    = 253
	historyMaxTitle   = 120
	historyMaxTrack   = 32
	historyClockSkew  = 24 * time.Hour
)

// historyRecord is one monitor run of one cluster (see the contract in HistoryAppend).
type historyRecord struct {
	At int64 `json:"at"`
	// Reachable is false when no node answered (the phone was off the cluster's network):
	// the run says nothing about the cluster and counts as a gap. Absent means true.
	Reachable *bool           `json:"reachable,omitempty"`
	Nodes     []historyNode   `json:"nodes,omitempty"`
	Volumes   []historyVolume `json:"volumes,omitempty"`
	Alerts    []historyAlert  `json:"alerts,omitempty"`
}

type historyNode struct {
	Node           string   `json:"node"`
	Hostname       string   `json:"hostname,omitempty"`
	Health         string   `json:"health"`
	Version        string   `json:"version,omitempty"`
	MemUsedPercent *float64 `json:"memUsedPercent,omitempty"`
}

type historyVolume struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Node        string  `json:"node"`
	UsedPercent float64 `json:"usedPercent"`
}

// historyAlert is an issue open after this run (notified, or present at the baseline).
type historyAlert struct {
	Key      string `json:"key"`
	Track    string `json:"track,omitempty"`
	Severity string `json:"severity,omitempty"`
	Title    string `json:"title,omitempty"`
}

func (r historyRecord) reachable() bool { return r.Reachable == nil || *r.Reachable }

// HistoryAppend adds one record (one monitor run of one cluster, JSON, see the contract)
// to ring and returns the new ring: records older than 30 days are dropped, those older
// than 7 days thinned, and the ring is kept under 512 KiB by dropping the oldest. An empty
// ring starts a new one. A record with the same "at" as one already there replaces it.
//
// A ring written by a newer version fails (the app keeps it as it is and skips the run).
// A ring that cannot be read starts over with this record, and HistoryQuery then reports
// "resetAt" and "resetReason" so the app can log or show it.
func HistoryAppend(ringBytes []byte, recordJSON string, nowMillis int64) (out []byte, err error) {
	defer maskErr(&err)

	rec, err := parseHistoryRecord(recordJSON, nowMillis)
	if err != nil {
		return nil, err
	}

	ring, err := decodeHistoryRing(ringBytes)

	switch {
	case errors.Is(err, errHistoryNewer):
		return nil, err
	case err != nil:
		ring = historyRing{meta: historyMeta{ResetAt: nowMillis, ResetReason: err.Error()}}
	}

	ring.records = insertHistoryRecord(ring.records, rec)
	ring.records = thinHistory(pruneHistory(ring.records, nowMillis), nowMillis)

	return encodeHistoryCapped(ring, historyMaxBytes)
}

// HistoryQuery describes the ring between sinceMillis and nowMillis for the history
// screen: node state intervals and uptime, alerts opened and closed, volume fill and
// memory series (at most 200 points each) and the gaps without data.
func HistoryQuery(ringBytes []byte, sinceMillis, nowMillis int64) (out string, err error) {
	defer maskResult(&out, &err)

	ring, err := decodeHistoryRing(ringBytes)
	if err != nil {
		return "", err
	}

	if nowMillis < sinceMillis {
		return "", fmt.Errorf("history: the end (%d) is before the start (%d)", nowMillis, sinceMillis)
	}

	return toJSON(queryHistory(ring, sinceMillis, nowMillis))
}

// HistorySince summarises what happened after lastLookedMillis, up to the latest record:
// nodes that went down and came back, nodes still down, alerts resolved and still open,
// and Talos upgrades. Every list is empty (never null) when nothing happened.
func HistorySince(ringBytes []byte, lastLookedMillis int64) (out string, err error) {
	defer maskResult(&out, &err)

	ring, err := decodeHistoryRing(ringBytes)
	if err != nil {
		return "", err
	}

	return toJSON(historySinceSummary(ring.records, lastLookedMillis))
}

// HistoryExport returns the whole ring as JSON for the support bundle, anonymised: node
// addresses and hostnames become node-1, node-2..., user volume names volume-1...,
// alert keys alert-1... and alert titles are left out. Timestamps, health, versions and
// percentages are kept.
func HistoryExport(ringBytes []byte) (out string, err error) {
	defer maskResult(&out, &err)

	ring, err := decodeHistoryRing(ringBytes)
	if err != nil {
		return "", err
	}

	return toJSON(exportHistory(ring))
}

func parseHistoryRecord(recordJSON string, nowMillis int64) (historyRecord, error) {
	var rec historyRecord

	if err := json.Unmarshal([]byte(recordJSON), &rec); err != nil {
		return rec, fmt.Errorf("history record: %w", err)
	}

	if rec.At <= 0 {
		return rec, errors.New("history record: \"at\" is missing")
	}

	if rec.At > nowMillis+historyClockSkew.Milliseconds() {
		return rec, fmt.Errorf("history record: \"at\" (%d) is in the future", rec.At)
	}

	if len(rec.Nodes) > historyMaxNodes || len(rec.Volumes) > historyMaxVolumes || len(rec.Alerts) > historyMaxAlerts {
		return rec, fmt.Errorf("history record: too many entries (at most %d nodes, %d volumes, %d alerts)",
			historyMaxNodes, historyMaxVolumes, historyMaxAlerts)
	}

	nodes, err := cleanHistoryNodes(rec.Nodes)
	if err != nil {
		return rec, err
	}

	if rec.Reachable != nil && *rec.Reachable {
		rec.Reachable = nil // the default, left out of the ring
	}

	rec.Nodes = nodes
	rec.Volumes = cleanHistoryVolumes(rec.Volumes)
	rec.Alerts = cleanHistoryAlerts(rec.Alerts)

	return rec, nil
}

func cleanHistoryNodes(in []historyNode) ([]historyNode, error) {
	out := make([]historyNode, 0, len(in))

	for _, n := range in {
		switch n.Health {
		case historyReady, historyNotReady, historyUnreachable:
		default:
			return nil, fmt.Errorf("history record: node health %q is not ready, notReady or unreachable", n.Health)
		}

		if n.Node == "" {
			return nil, errors.New("history record: a node has no \"node\"")
		}

		node := historyNode{
			Node:     clipUTF8(n.Node, historyMaxName),
			Hostname: clipUTF8(n.Hostname, historyMaxName),
			Health:   n.Health,
			Version:  clipUTF8(n.Version, historyMaxTrack),
		}

		if n.MemUsedPercent != nil {
			p := historyPercent(*n.MemUsedPercent)
			node.MemUsedPercent = &p
		}

		out = append(out, node)
	}

	slices.SortFunc(out, func(a, b historyNode) int { return strings.Compare(a.Node, b.Node) })

	return slices.CompactFunc(out, func(a, b historyNode) bool { return a.Node == b.Node }), nil
}

func cleanHistoryVolumes(in []historyVolume) []historyVolume {
	out := make([]historyVolume, 0, len(in))

	for _, v := range in {
		if v.Key == "" {
			continue
		}

		out = append(out, historyVolume{
			Key:         clipUTF8(v.Key, 2*historyMaxName),
			Name:        clipUTF8(v.Name, historyMaxName),
			Node:        clipUTF8(v.Node, historyMaxName),
			UsedPercent: historyPercent(v.UsedPercent),
		})
	}

	slices.SortFunc(out, func(a, b historyVolume) int { return strings.Compare(a.Key, b.Key) })

	return slices.CompactFunc(out, func(a, b historyVolume) bool { return a.Key == b.Key })
}

func cleanHistoryAlerts(in []historyAlert) []historyAlert {
	out := make([]historyAlert, 0, len(in))

	for _, a := range in {
		if a.Key == "" {
			continue
		}

		out = append(out, historyAlert{
			Key:      clipUTF8(a.Key, 2*historyMaxName),
			Track:    clipUTF8(a.Track, historyMaxTrack),
			Severity: clipUTF8(a.Severity, historyMaxTrack),
			Title:    clipUTF8(a.Title, historyMaxTitle),
		})
	}

	slices.SortFunc(out, func(a, b historyAlert) int { return strings.Compare(a.Key, b.Key) })

	return slices.CompactFunc(out, func(a, b historyAlert) bool { return a.Key == b.Key })
}

// historyPercent bounds a percentage to 0-100 with one decimal (smaller ring).
func historyPercent(p float64) float64 {
	p = min(max(p, 0), 100)

	return float64(int64(p*10+0.5)) / 10
}
