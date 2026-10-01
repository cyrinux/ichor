package talosmobile

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// The report is plain text: the user reads it before it is sent, and the model reads the
// same thing. Healthy things take one line, problems get the detail.

const (
	// Disks fuller than this are flagged; a full /var or EPHEMERAL stops the kubelet and etcd.
	diskAlertPercent = 85
	// Far above what a broken cluster of a few dozen nodes produces, and still a size every
	// model and the share sheet take.
	maxReportBytes = 256 << 10
)

func renderDiagnosis(d diagnosisData) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Talos cluster report, collected %s through the Talos API", d.At.UTC().Format("2006-01-02 15:04 MST"))

	if len(d.Roles) > 0 {
		fmt.Fprintf(&b, " (client role: %s)", strings.Join(d.Roles, ", "))
	}

	b.WriteString("\n\n")
	renderNodeList(&b, d.Nodes)

	for _, n := range d.Nodes {
		if n.Reachable {
			renderNode(&b, n, d.At)
		}
	}

	renderEtcd(&b, d)
	renderEvents(&b, d)

	// Log lines are raw bytes from the nodes: the report must be text all the way to the UI
	// (an invalid string does not cross to Swift at all).
	// The tags are neutralized here rather than when sending: what is shown is what is sent.
	report := neutralizeTags(strings.ToValidUTF8(strings.TrimRight(b.String(), "\n"), "\uFFFD"))
	if len(report) > maxReportBytes {
		report = clipText(report, maxReportBytes) + "\n[The report was cut here: it is too large to send whole.]"
	}

	return report + "\n"
}

// clipText cuts s to at most limit bytes, on a character boundary, marking the cut.
func clipText(s string, limit int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if len(s) <= limit {
		return s
	}

	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}

	return s[:limit] + "…"
}

func renderNodeList(b *strings.Builder, nodes []nodeDiagnosis) {
	var ready, unreachable int

	for _, n := range nodes {
		switch {
		case !n.Reachable:
			unreachable++
		case n.Ready:
			ready++
		}
	}

	fmt.Fprintf(b, "NODES (%d: %d ready, %d not ready, %d unreachable)\n",
		len(nodes), ready, len(nodes)-ready-unreachable, unreachable)

	for _, n := range nodes {
		if !n.Reachable {
			fmt.Fprintf(b, "- %s: UNREACHABLE, %s\n", n.Node, n.Error)

			continue
		}

		state := "NOT READY"
		if n.Ready {
			state = "ready"
		}

		fmt.Fprintf(b, "- %s: %s, Talos %s, stage %s, %s\n", nodeLabel(n.nodeOverview), n.Role, n.Version, n.Stage, state)

		for _, c := range n.UnmetConditions {
			fmt.Fprintf(b, "    unmet condition %s: %s\n", c.Name, c.Reason)
		}

		if n.Error != "" {
			fmt.Fprintf(b, "    could not read: %s\n", n.Error)
		}
	}

	b.WriteString("\n")
}

// nodeLabel is "hostname [address]", or the address alone when the hostname is unknown.
func nodeLabel(n nodeOverview) string {
	if n.Hostname == "" || n.Hostname == n.Node {
		return n.Node
	}

	return fmt.Sprintf("%s [%s]", n.Hostname, n.Node)
}

func renderNode(b *strings.Builder, n nodeDiagnosis, at time.Time) {
	fmt.Fprintf(b, "NODE %s\n", nodeLabel(n.nodeOverview))

	switch k := n.Kube; {
	case k == nil:
		b.WriteString("  kubernetes node: no status (not registered with the API server, or it cannot be reached)\n")
	case !k.Ready:
		fmt.Fprintf(b, "  kubernetes node %s: NOT READY%s\n", k.Name, cordoned(k))
	default:
		fmt.Fprintf(b, "  kubernetes node %s: ready%s\n", k.Name, cordoned(k))
	}

	if r := n.Resources; r != nil {
		renderResources(b, r, at)
	}

	if c := n.Clock; c != nil {
		if c.Error != "" {
			fmt.Fprintf(b, "  clock: could not compare with NTP: %s\n", c.Error)
		} else {
			fmt.Fprintf(b, "  clock: %+d ms from its NTP server\n", c.OffsetMs)
		}
	}

	renderServices(b, n, at)

	for _, p := range n.StaticPods {
		state := "ready"
		if !p.Ready {
			state = "NOT READY"
		}

		line := fmt.Sprintf("  static pod %s: %s, %s, %d restarts", p.Name, p.Phase, state, p.Restarts)
		if p.Waiting != "" {
			line += ", waiting: " + clipText(p.Waiting, diagnosisMaxLineLen)
		}

		b.WriteString(line + "\n")
	}

	fmt.Fprintf(b, "  kubernetes containers running: %d\n", n.Running)

	if len(n.IdlePods) > 0 {
		b.WriteString("  pods without a running container (crash loop, still starting, or a finished job):\n")

		for _, p := range n.IdlePods {
			fmt.Fprintf(b, "    - %s\n", p)
		}
	}

	for _, l := range n.Logs {
		if l.Err != "" {
			fmt.Fprintf(b, "  %s log: could not read: %s\n", l.Service, l.Err)

			continue
		}

		fmt.Fprintf(b, "  %s log, last %d lines:\n", l.Service, len(l.Lines))

		for _, line := range l.Lines {
			fmt.Fprintf(b, "    | %s\n", line)
		}
	}

	b.WriteString("\n")
}

func cordoned(k *kubeNodeState) string {
	if k.Unschedulable {
		return ", cordoned (unschedulable)"
	}

	return ""
}

func renderResources(b *strings.Builder, r *nodeResources, at time.Time) {
	if r.MemTotal > 0 {
		fmt.Fprintf(b, "  memory: %s available of %s\n", formatSize(r.MemAvailable), formatSize(r.MemTotal))
	}

	if r.CPUCount > 0 {
		fmt.Fprintf(b, "  load: %.2f %.2f %.2f on %d CPUs\n", r.Load1, r.Load5, r.Load15, r.CPUCount)
	}

	if r.BootTime > 0 {
		fmt.Fprintf(b, "  up: %s\n", formatAge(at.Sub(time.Unix(int64(r.BootTime), 0))))
	}

	for _, m := range r.Mounts {
		if m.Size == 0 {
			continue
		}

		used := 100 - int(m.Available*100/m.Size)

		flag := ""
		if used >= diskAlertPercent {
			flag = " (NEARLY FULL)"
		}

		fmt.Fprintf(b, "  disk %s: %d%% used, %s free of %s%s\n", m.MountedOn, used, formatSize(m.Available), formatSize(m.Size), flag)
	}
}

func renderServices(b *strings.Builder, n nodeDiagnosis, at time.Time) {
	if n.ServicesErr != "" {
		fmt.Fprintf(b, "  services: could not list: %s\n", n.ServicesErr)

		return
	}

	var fine []string

	for _, s := range n.Services {
		if !serviceTroubled(s) {
			fine = append(fine, s.ID)
		}
	}

	if len(fine) > 0 {
		fmt.Fprintf(b, "  services fine: %s\n", strings.Join(fine, ", "))
	}

	for _, s := range n.Services {
		if !serviceTroubled(s) {
			continue
		}

		line := fmt.Sprintf("  service %s: state %s, health %s", s.ID, s.State, s.Health)

		if s.LastChange > 0 {
			line += " for " + formatAge(at.Sub(time.Unix(s.LastChange, 0)))
		}

		if s.Message != "" {
			line += ", health check says: " + clipText(s.Message, diagnosisMaxLineLen)
		}

		if s.LastEvent != "" {
			line += ", last event: " + clipText(s.LastEvent, diagnosisMaxLineLen)
		}

		b.WriteString(line + "\n")
	}
}

func renderEtcd(b *strings.Builder, d diagnosisData) {
	b.WriteString("ETCD\n")

	e := d.Etcd
	if e == nil {
		fmt.Fprintf(b, "  not checked: %s\n\n", d.EtcdNote)

		return
	}

	if e.Error != "" {
		fmt.Fprintf(b, "  %s\n", e.Error)
	}

	for _, m := range e.Members {
		learner := ""
		if m.IsLearner {
			learner = ", learner"
		}

		fmt.Fprintf(b, "  member %s: %s%s\n", m.ID, m.Hostname, learner)
	}

	if e.LeaderID == "" {
		b.WriteString("  NO LEADER\n")
	}

	for _, s := range e.Statuses {
		if s.Error != "" {
			fmt.Fprintf(b, "  status on %s: FAILED, %s\n", s.Node, s.Error)

			continue
		}

		role := "follower"

		switch {
		case s.IsLeader:
			role = "leader"
		case s.IsLearner:
			role = "learner"
		}

		fmt.Fprintf(b, "  status on %s: member %s, %s, database %s (%s in use), raft index %d, term %d\n",
			s.Node, s.MemberID, role, formatSize(uint64(max(s.DbSize, 0))), formatSize(uint64(max(s.DbSizeInUse, 0))),
			s.RaftIndex, s.RaftTerm)

		for _, err := range s.Errors {
			fmt.Fprintf(b, "    error: %s\n", err)
		}
	}

	if len(e.Alarms) == 0 {
		b.WriteString("  alarms: none\n")
	}

	for _, a := range e.Alarms {
		fmt.Fprintf(b, "  ALARM %s on member %s\n", a.Alarm, a.MemberID)
	}

	b.WriteString("\n")
}

func renderEvents(b *strings.Builder, d diagnosisData) {
	events := d.Events

	switch {
	case len(events) > 0:
		fmt.Fprintf(b, "RECENT WARNING AND ERROR EVENTS (%d, oldest first)\n", len(events))
	case d.EventsNote != "":
		b.WriteString("RECENT WARNING AND ERROR EVENTS\n")
	default:
		b.WriteString("RECENT WARNING AND ERROR EVENTS\n  none\n")
	}

	if d.EventsNote != "" {
		fmt.Fprintf(b, "  not complete: %s\n", d.EventsNote)
	}

	for _, ev := range events {
		line := fmt.Sprintf("  %s %s %s: %s %s %s", time.UnixMilli(ev.At).UTC().Format("2006-01-02 15:04:05"),
			ev.Node, ev.Severity, ev.Kind, ev.Subject, ev.Action)

		line = strings.TrimRight(line, " ")
		if ev.Message != "" {
			line += ": " + clipText(ev.Message, diagnosisMaxLineLen)
		}

		b.WriteString(line + "\n")
	}
}

func formatSize(n uint64) string {
	const unit = 1024

	switch {
	case n >= unit*unit*unit:
		return fmt.Sprintf("%.1f GiB", float64(n)/(unit*unit*unit))
	case n >= unit*unit:
		return fmt.Sprintf("%.0f MiB", float64(n)/(unit*unit))
	default:
		return fmt.Sprintf("%d KiB", n/unit)
	}
}

// formatAge gives one unit, enough to tell "just restarted" from "up for weeks".
func formatAge(d time.Duration) string {
	switch {
	case d < 0:
		return "0 s"
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
