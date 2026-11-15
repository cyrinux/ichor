package ichorgo

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// explainAnnotations are the alert annotations worth sending, in this order, with the word the
// report uses: one the anonymized report's sweep reads as a word, not as a name ("runbook_url").
var explainAnnotations = [][2]string{
	{"summary", "summary"}, {"description", "description"}, {"message", "message"},
	{"runbook_url", "runbook"}, {"runbook", "runbook"},
}

// renderExplanation writes the report: the subject first, then its events, then the log tail.
func renderExplanation(d explainData) string {
	var b strings.Builder

	switch {
	case d.Pod != nil:
		renderExplainPod(&b, d.Pod)
	case d.Alert != nil:
		renderExplainAlert(&b, d.Alert)
	case d.Finding != nil:
		renderExplainFinding(&b, d.Finding)
	}

	fmt.Fprintf(&b, "\nCollected %s through the Kubernetes API.\n\n", d.At.UTC().Format("2006-01-02 15:04 MST"))

	if d.Pod != nil || d.EventsNote != "" || len(d.Events) > 0 {
		renderExplainEvents(&b, d)
	}

	if d.LogFrom != "" {
		renderExplainLog(&b, d)
	}

	return closeReport(b.String(), explainMaxReport)
}

func renderExplainPod(b *strings.Builder, p *explainPodState) {
	fmt.Fprintf(b, "Pod %s/%s\n", p.Namespace, p.Name)
	fmt.Fprintf(b, "  status %s, phase %s, %d of %d ready, %d restarts", p.Status, p.Phase, p.Ready, p.kubePod.Containers, p.Restarts)

	if p.Node != "" {
		fmt.Fprintf(b, ", on %s", p.Node)
	}

	b.WriteString("\n")

	if p.Owner != "" {
		fmt.Fprintf(b, "  owner %s\n", p.Owner)
	}

	if len(p.Images) > 0 {
		fmt.Fprintf(b, "  images %s\n", strings.Join(p.Images, ", "))
	}

	for _, c := range p.ContainerStates {
		fmt.Fprintf(b, "  %s\n", c)
	}

	if p.Waiting != "" && len(p.ContainerStates) == 0 {
		fmt.Fprintf(b, "  waiting: %s\n", clipUTF8(p.Waiting, diagnosisMaxLineLen))
	}

	if pull := p.Pull; pull != nil {
		fmt.Fprintf(b, "  image pull problem: %s, image %s, registry %s", pull.Kind, pull.Image, pull.Registry)

		if len(pull.Secrets) > 0 {
			fmt.Fprintf(b, ", pull secrets %s", strings.Join(pull.Secrets, ", "))
		}

		b.WriteString("\n")
	}
}

func renderExplainAlert(b *strings.Builder, a *amAlert) {
	fmt.Fprintf(b, "Alert %s (%s), %s", a.Alertname, a.Severity, a.State)

	if a.StartsAt > 0 {
		fmt.Fprintf(b, " since %s", time.UnixMilli(a.StartsAt).UTC().Format("2006-01-02 15:04 MST"))
	}

	b.WriteString("\n")

	if len(a.SilencedBy) > 0 {
		b.WriteString("  silenced\n")
	}

	if len(a.InhibitedBy) > 0 {
		b.WriteString("  inhibited by another alert\n")
	}

	for _, annotation := range explainAnnotations {
		if v := strings.TrimSpace(a.Annotations[annotation[0]]); v != "" {
			fmt.Fprintf(b, "  %s: %s\n", annotation[1], clipUTF8(v, diagnosisMaxLineLen*2))
		}
	}

	var labels []string

	for _, key := range slices.Sorted(maps.Keys(a.Labels)) {
		if key != "alertname" && key != "severity" {
			// "key: value", not "key=value": anonymized, only the value becomes a placeholder.
			labels = append(labels, key+": "+a.Labels[key])
		}
	}

	if len(labels) > 0 {
		fmt.Fprintf(b, "  labels: %s\n", clipUTF8(strings.Join(labels, "; "), diagnosisMaxLineLen*2))
	}
}

func renderExplainFinding(b *strings.Builder, s *explainSubject) {
	if text := strings.TrimSpace(s.Text); text != "" {
		fmt.Fprintf(b, "Checkup finding: %s\n", clipUTF8(text, diagnosisMaxLineLen))
	} else {
		b.WriteString("Checkup finding\n")
	}

	b.WriteString("  " + kubeFindingLine(*s.Finding) + "\n")

	if f := s.Finding; f.Title != "" || f.Detail != "" {
		fmt.Fprintf(b, "  from the plugin %s: %s %s\n", f.PluginName, f.Title, clipUTF8(f.Detail, diagnosisMaxLineLen))
	}

	if fix := strings.TrimSpace(s.Fix); fix != "" {
		fmt.Fprintf(b, "  suggested fix: %s\n", clipUTF8(fix, diagnosisMaxLineLen))
	}
}

func renderExplainEvents(b *strings.Builder, d explainData) {
	fmt.Fprintf(b, "EVENTS (%d, newest first)\n", len(d.Events))

	switch {
	case d.EventsNote != "":
		fmt.Fprintf(b, "  %s\n", d.EventsNote)
	case len(d.Events) == 0:
		b.WriteString("  none\n")
	}

	for _, e := range d.Events {
		object := e.Kind + " " + e.Name
		fmt.Fprintf(b, "  %s %s %s %s, %d times: %s\n", time.UnixMilli(e.Last).UTC().Format("2006-01-02 15:04:05"),
			e.Type, object, e.Reason, e.Count, clipUTF8(e.Message, diagnosisMaxLineLen))
	}

	b.WriteString("\n")
}

func renderExplainLog(b *strings.Builder, d explainData) {
	if d.LogNote != "" {
		fmt.Fprintf(b, "LOG of %s\n  %s\n", d.LogFrom, d.LogNote)

		return
	}

	fmt.Fprintf(b, "LOG of %s (last %d lines)\n", d.LogFrom, len(d.Log))

	if len(d.Log) == 0 {
		b.WriteString("  empty\n")
	}

	for _, l := range d.Log {
		b.WriteString("  " + l + "\n")
	}
}
