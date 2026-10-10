package ichorgo

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

type historySinceResult struct {
	From           int64                 `json:"from"`
	To             int64                 `json:"to"`
	Records        int                   `json:"records"`
	NodesRecovered []historyNodeOutage   `json:"nodesRecovered"`
	NodesDown      []historyNodeOutage   `json:"nodesDown"`
	AlertsResolved []historyAlertSpan    `json:"alertsResolved"`
	AlertsOpen     []historyAlertSpan    `json:"alertsOpen"`
	Upgrades       []historyNodeUpgrades `json:"upgrades"`
}

// historyNodeOutage is a stretch where a node was not ready; UpAt is absent while it lasts.
type historyNodeOutage struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	State    string `json:"state"` // the worst seen: unreachable, else notReady
	DownAt   int64  `json:"downAt"`
	UpAt     *int64 `json:"upAt,omitempty"`
}

type historyNodeUpgrades struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	From     string `json:"from"`
	To       string `json:"to"`
	At       int64  `json:"at"`
}

// historySinceSummary walks every reachable record (an outage or alert may have started
// before lastLooked) and keeps what changed after it.
func historySinceSummary(records []historyRecord, lastLooked int64) historySinceResult {
	result := historySinceResult{
		From:           lastLooked,
		To:             lastLooked,
		NodesRecovered: []historyNodeOutage{},
		NodesDown:      []historyNodeOutage{},
		AlertsResolved: []historyAlertSpan{},
		AlertsOpen:     []historyAlertSpan{},
		Upgrades:       []historyNodeUpgrades{},
	}

	down := map[string]*historyNodeOutage{}
	versions := map[string]string{}

	var latest []historyNode

	for _, rec := range records {
		if rec.At > lastLooked {
			result.Records++
			result.To = max(result.To, rec.At)
		}

		if !rec.reachable() {
			continue
		}

		latest = rec.Nodes

		for _, n := range rec.Nodes {
			if prev := versions[n.Node]; prev != "" && n.Version != "" && prev != n.Version && rec.At > lastLooked {
				result.Upgrades = append(result.Upgrades, historyNodeUpgrades{n.Node, n.Hostname, prev, n.Version, rec.At})
			}

			if n.Version != "" {
				versions[n.Node] = n.Version
			}

			trackOutage(&result, down, n, rec.At, lastLooked)
		}
	}

	for _, n := range latest {
		if o := down[n.Node]; o != nil {
			result.NodesDown = append(result.NodesDown, *o)
		}
	}

	for _, a := range historyAlertSpans(records) {
		switch {
		case a.ClosedAt == nil:
			result.AlertsOpen = append(result.AlertsOpen, a)
		case *a.ClosedAt > lastLooked:
			result.AlertsResolved = append(result.AlertsResolved, a)
		}
	}

	slices.SortFunc(result.NodesDown, func(a, b historyNodeOutage) int { return cmp.Compare(a.Node, b.Node) })

	return result
}

func trackOutage(result *historySinceResult, down map[string]*historyNodeOutage, n historyNode, at, lastLooked int64) {
	o := down[n.Node]

	switch {
	case n.Health == historyReady && o != nil:
		if at > lastLooked {
			o.UpAt = &at
			result.NodesRecovered = append(result.NodesRecovered, *o)
		}

		delete(down, n.Node)
	case n.Health != historyReady && o == nil:
		down[n.Node] = &historyNodeOutage{Node: n.Node, Hostname: n.Hostname, State: n.Health, DownAt: at}
	case n.Health == historyUnreachable && o != nil:
		o.State = historyUnreachable
	}
}

// historyExportResult is the anonymised ring of the support bundle.
type historyExportResult struct {
	Format      int             `json:"format"`
	ResetAt     int64           `json:"resetAt,omitempty"`
	ResetReason string          `json:"resetReason,omitempty"`
	Records     []historyRecord `json:"records"`
}

// historyAnonymizer hands out stable placeholders: the same value always gets the same one.
type historyAnonymizer struct {
	names map[string]map[string]string
	count map[string]int
}

func (a *historyAnonymizer) placeholder(kind, value string) string {
	if value == "" {
		return ""
	}

	if a.names == nil {
		a.names, a.count = map[string]map[string]string{}, map[string]int{}
	}

	m := a.names[kind]
	if m == nil {
		m = map[string]string{}
		a.names[kind] = m
	}

	if p, ok := m[value]; ok {
		return p
	}

	a.count[kind]++
	p := kind + "-" + strconv.Itoa(a.count[kind])
	m[value] = p

	return p
}

// historySystemVolume tells Talos's own volume names (EPHEMERAL, STATE, IMAGECACHE...),
// which say nothing about the user and are kept.
func historySystemVolume(name string) bool {
	return name != "" && strings.ToUpper(name) == name && !strings.ContainsAny(name, "/.:")
}

func exportHistory(ring historyRing) historyExportResult {
	var anon historyAnonymizer

	out := historyExportResult{
		Format: historyVersion, ResetAt: ring.meta.ResetAt, ResetReason: ring.meta.ResetReason,
		Records: make([]historyRecord, 0, len(ring.records)),
	}

	// A node's address and its hostname are the same machine: one placeholder for both.
	node := func(n historyNode) string {
		p := anon.placeholder("node", n.Node)
		if n.Hostname != "" {
			anon.names["node"][n.Hostname] = p
		}

		return p
	}

	for _, rec := range ring.records {
		r := historyRecord{At: rec.At, Reachable: rec.Reachable}

		for _, n := range rec.Nodes {
			p := node(n)
			r.Nodes = append(r.Nodes, historyNode{Node: p, Hostname: p, Health: n.Health, Version: n.Version, MemUsedPercent: n.MemUsedPercent})
		}

		for _, v := range rec.Volumes {
			name := v.Name
			if !historySystemVolume(name) {
				name = anon.placeholder("volume", name)
			}

			p := anon.placeholder("node", v.Node)
			r.Volumes = append(r.Volumes, historyVolume{Key: p + "|" + name, Name: name, Node: p, UsedPercent: v.UsedPercent})
		}

		for _, al := range rec.Alerts {
			r.Alerts = append(r.Alerts, historyAlert{Key: anon.placeholder("alert", al.Key), Track: al.Track, Severity: al.Severity})
		}

		out.Records = append(out.Records, r)
	}

	return out
}
