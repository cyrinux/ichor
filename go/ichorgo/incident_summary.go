package ichorgo

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The incident summary: a Markdown page about one cluster over a window, to paste in a chat
// or a ticket. It reads only what the phone holds (the history ring the app unsealed, and
// the action audit log), never the network. Anonymised, it names the nodes node-1,
// node-2... (as HistoryExport does), the cluster "cluster", and passes the free text (alert
// titles, action parameters and errors) through a mask of its own, so that the shared text
// cannot be mapped back with the screen's mask (see Diagnosis.ownMask).

// incidentMaxBytes bounds the summary: the oldest timeline rows go first.
const incidentMaxBytes = 64 << 10

// IncidentSummary is the Markdown incident summary of contextName between sinceMillis and
// nowMillis: what is wrong at the last record, the timeline (node states, alerts, Talos
// versions), the actions taken from the phone, the gaps in the phone's polling and a footer
// with the window, times in UTC. ringBytes is the cluster's history ring (as HistoryQuery
// takes it); an empty ring gives a summary that says there is no recorded history. With
// anonymize, no hostname, node address, context name or secret is left in the text.
func IncidentSummary(contextName string, ringBytes []byte, sinceMillis, nowMillis int64, anonymize bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = privacy.unmaskContext(strings.TrimSpace(contextName))

	if contextName == "" {
		return "", errors.New("incident summary: no cluster given")
	}

	if nowMillis < sinceMillis {
		return "", fmt.Errorf("incident summary: the end (%d) is before the start (%d)", nowMillis, sinceMillis)
	}

	ring, err := decodeHistoryRing(ringBytes)
	if err != nil {
		return "", err
	}

	names := newIncidentNames(contextName, ring, anonymize)
	view := buildIncidentView(names, ring, auditEntries(contextName, ""), sinceMillis, nowMillis)

	return renderIncidentBounded(view, incidentMaxBytes), nil
}

// incidentEvent is one timeline row.
type incidentEvent struct {
	at   int64
	text string
}

// incidentView is what the summary says, names already anonymised when asked.
type incidentView struct {
	cluster    string
	since, now int64
	generated  int64
	// hasHistory is false when no record is at or before now.
	hasHistory bool
	lastAt     int64
	// lastReached is false when the last record reached no node.
	lastReached   bool
	lastReachedAt int64 // the last record that reached a node, 0 if none
	wrongNodes    []string
	openAlerts    []string
	timeline      []incidentEvent // oldest first
	actions       []auditEntry    // oldest first, cells anonymised
	gaps          []historySpan
}

func buildIncidentView(names *incidentNames, ring historyRing, entries []auditEntry, since, now int64) incidentView {
	records := recordsUpTo(ring.records, now)

	v := incidentView{
		cluster: names.cluster, since: since, now: now, generated: auditNow().UnixMilli(),
		hasHistory: len(records) > 0,
		actions:    incidentActions(names, entries, since, now),
	}

	if !v.hasHistory {
		return v
	}

	last := records[len(records)-1]
	v.lastAt, v.lastReached = last.At, last.reachable()

	for _, rec := range slices.Backward(records) {
		if rec.reachable() {
			v.lastReachedAt = rec.At
			v.wrongNodes = incidentWrongNodes(names, rec)

			break
		}
	}

	v.openAlerts = incidentOpenAlerts(names, historyAlertSpans(records))

	q := queryHistory(historyRing{records: records}, since, now)
	v.gaps = q.Gaps
	v.timeline = incidentTimeline(names, q, historySinceSummary(records, since), since, now)

	return v
}

// recordsUpTo is the records at or before now (the ring is oldest first).
func recordsUpTo(records []historyRecord, now int64) []historyRecord {
	end, _ := slices.BinarySearchFunc(records, now+1, func(r historyRecord, t int64) int { return cmp.Compare(r.At, t) })

	return records[:end]
}

func incidentWrongNodes(names *incidentNames, rec historyRecord) []string {
	var out []string

	for _, n := range rec.Nodes {
		if n.Health != historyReady {
			out = append(out, names.node(n.Node, n.Hostname)+" is "+incidentHealth(n.Health))
		}
	}

	return out
}

// incidentSeverityRank orders the open alerts: the worst first.
func incidentSeverityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}

func incidentOpenAlerts(names *incidentNames, spans []historyAlertSpan) []string {
	open := slices.DeleteFunc(slices.Clone(spans), func(a historyAlertSpan) bool { return a.ClosedAt != nil })

	slices.SortStableFunc(open, func(a, b historyAlertSpan) int {
		return cmp.Or(cmp.Compare(incidentSeverityRank(a.Severity), incidentSeverityRank(b.Severity)), cmp.Compare(a.OpenedAt, b.OpenedAt))
	})

	out := make([]string, 0, len(open))

	for _, a := range open {
		out = append(out, fmt.Sprintf("**%s**: %s (open since %s)", incidentSeverity(a.Severity), names.alert(a), incidentTime(a.OpenedAt)))
	}

	return out
}

func incidentTimeline(names *incidentNames, q historyQueryResult, since historySinceResult, from, to int64) []incidentEvent {
	var events []incidentEvent

	add := func(at int64, text string) { events = append(events, incidentEvent{at, text}) }

	for _, tl := range q.Nodes {
		label := names.node(tl.Node, tl.Hostname)

		for i, iv := range tl.Intervals {
			switch {
			case i == 0 && iv.State != historyReady:
				add(iv.From, label+" was "+incidentHealth(iv.State))
			case i > 0 && iv.State != tl.Intervals[i-1].State:
				add(iv.From, label+": "+incidentHealth(tl.Intervals[i-1].State)+" → "+incidentHealth(iv.State))
			}
		}
	}

	for _, a := range q.Alerts {
		if a.OpenedAt >= from {
			add(a.OpenedAt, fmt.Sprintf("alert opened (%s): %s", incidentSeverity(a.Severity), names.alert(a)))
		}

		if a.ClosedAt != nil && *a.ClosedAt >= from && *a.ClosedAt <= to {
			add(*a.ClosedAt, "alert closed: "+names.alert(a))
		}
	}

	for _, u := range since.Upgrades {
		if u.At <= to {
			add(u.At, names.node(u.Node, u.Hostname)+": Talos "+u.From+" → "+u.To)
		}
	}

	slices.SortStableFunc(events, func(a, b incidentEvent) int { return cmp.Compare(a.at, b.at) })

	return events
}

// incidentActions is the audit entries in the window, oldest first, their free text
// redacted and anonymised.
func incidentActions(names *incidentNames, entries []auditEntry, since, now int64) []auditEntry {
	var out []auditEntry

	for _, e := range slices.Backward(entries) {
		if e.At < since || e.At > now {
			continue
		}

		out = append(out, auditEntry{
			At: e.At, Action: e.Action, Outcome: e.Outcome, Demo: e.Demo,
			Object: names.text(auditTarget(e)),
			Params: names.text(redactSecrets(e.Params)),
			Error:  names.text(redactSecrets(e.Error)),
		})
	}

	return out
}

func incidentHealth(state string) string {
	switch state {
	case historyNotReady:
		return "not ready"
	case historyUnreachable:
		return "unreachable"
	default:
		return state
	}
}

func incidentSeverity(severity string) string {
	if severity == "" {
		return "alert"
	}

	return strings.ToLower(severity)
}
