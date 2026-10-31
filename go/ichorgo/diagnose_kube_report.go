package ichorgo

import (
	"fmt"
	"strings"
	"time"
)

const (
	// maxKubeReportBytes keeps the report of a kubeconfig cluster to what a model reads whole.
	maxKubeReportBytes = 32 << 10

	kubeWorkloadsTitle = "WORKLOADS NOT READY"
	kubePodsTitle      = "PODS IN TROUBLE"
	talosEventsTitle   = "RECENT WARNING AND ERROR EVENTS"
)

// renderKubeDiagnosis writes the report of a kubeconfig cluster. The words of the report itself
// stay plain: anything name-shaped in it is hidden when it is anonymized (diagnose_kube_names.go).
func renderKubeDiagnosis(d kubeDiagnosisData) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Kubernetes cluster report, collected %s through the Kubernetes API", d.At.UTC().Format("2006-01-02 15:04 MST"))

	if d.Version != "" {
		fmt.Fprintf(&b, " (API server %s)", d.Version)
	}

	b.WriteString("\n\n")
	renderKubeNodes(&b, d)
	renderKubeTrouble(&b, &d)
	renderKubeEvents(&b, d)
	renderKubeFindings(&b, d)
	renderKubePlugins(&b, d)
	renderKubeHelm(&b, d)
	renderGitOps(&b, d.GitOps)

	return closeReport(b.String(), maxKubeReportBytes)
}

func renderKubeNodes(b *strings.Builder, d kubeDiagnosisData) {
	if d.NodesNote != "" {
		fmt.Fprintf(b, "NODES\n  %s\n\n", d.NodesNote)

		return
	}

	ready := 0

	for _, n := range d.Nodes {
		if n.Ready {
			ready++
		}
	}

	fmt.Fprintf(b, "NODES (%d: %d ready, %d not ready)\n", len(d.Nodes), ready, len(d.Nodes)-ready)

	for _, n := range d.Nodes {
		state := "NOT READY"
		if n.Ready {
			state = "ready"
		}

		line := "- " + n.Name + ": " + state

		if len(n.Pressure) > 0 {
			line += ", under " + strings.Join(n.Pressure, ", ")
		}

		if n.Cordoned {
			line += ", cordoned (unschedulable)"
		}

		if n.Kubelet != "" {
			line += ", kubelet " + n.Kubelet
		}

		if n.Created > 0 {
			line += ", age " + formatAge(d.At.Sub(time.Unix(n.Created, 0)))
		}

		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
}

// renderKubeTrouble writes the two sections a Talos report shares; nothing without their data.
func renderKubeTrouble(b *strings.Builder, d *kubeDiagnosisData) {
	if d == nil {
		return
	}

	renderKubeWorkloads(b, d)
	renderKubePods(b, d)
}

func renderKubeWorkloads(b *strings.Builder, d *kubeDiagnosisData) {
	fmt.Fprintf(b, "%s (%d)\n", kubeWorkloadsTitle, len(d.Workloads)+d.WorkloadsMore)

	if d.WorkloadsNote != "" {
		fmt.Fprintf(b, "  %s\n", d.WorkloadsNote)
	} else if len(d.Workloads) == 0 {
		b.WriteString("  none\n")
	}

	for _, w := range d.Workloads {
		fmt.Fprintf(b, "  %s %s/%s: %d of %d ready", w.Kind, w.Namespace, w.Name, w.ready, w.Desired)

		if w.Reason != "" {
			fmt.Fprintf(b, ", reason: %s", clipUTF8(w.Reason, diagnosisMaxLineLen))
		}

		if w.Event != "" && w.Event != w.Reason {
			fmt.Fprintf(b, ", last event: %s", clipUTF8(w.Event, diagnosisMaxLineLen))
		}

		b.WriteString("\n")
	}

	if d.WorkloadsMore > 0 {
		fmt.Fprintf(b, "  and %d more\n", d.WorkloadsMore)
	}

	b.WriteString("\n")
}

func renderKubePods(b *strings.Builder, d *kubeDiagnosisData) {
	fmt.Fprintf(b, "%s (%d)\n", kubePodsTitle, len(d.Pods)+d.PodsMore)

	if d.PodsNote != "" {
		fmt.Fprintf(b, "  %s\n", d.PodsNote)
	} else if len(d.Pods) == 0 {
		b.WriteString("  none\n")
	}

	for _, p := range d.Pods {
		fmt.Fprintf(b, "  %s/%s: %s, %d of %d ready, %d restarts", p.Namespace, p.Name, p.Status, p.Ready, p.Containers, p.Restarts)

		if p.Node != "" {
			fmt.Fprintf(b, ", on %s", p.Node)
		}

		if p.LastTermination != "" {
			fmt.Fprintf(b, ", last ended %s", p.LastTermination)
		}

		if p.Reason != "" {
			fmt.Fprintf(b, ", reason: %s", clipUTF8(p.Reason, diagnosisMaxLineLen))
		}

		b.WriteString("\n")
	}

	if d.PodsMore > 0 {
		fmt.Fprintf(b, "  and %d more\n", d.PodsMore)
	}

	b.WriteString("\n")
}

func renderKubeEvents(b *strings.Builder, d kubeDiagnosisData) {
	fmt.Fprintf(b, "WARNING EVENTS (%d, newest first)\n", len(d.Events))

	if d.EventsNote != "" {
		fmt.Fprintf(b, "  %s\n", d.EventsNote)
	} else if len(d.Events) == 0 {
		b.WriteString("  none\n")
	}

	for _, e := range d.Events {
		object := e.Kind + " " + e.Name
		if e.Namespace != "" {
			object = e.Kind + " " + e.Namespace + "/" + e.Name
		}

		fmt.Fprintf(b, "  %s %s %s, %d times: %s\n", time.UnixMilli(e.Last).UTC().Format("2006-01-02 15:04:05"),
			object, e.Reason, e.Count, clipUTF8(e.Message, diagnosisMaxLineLen))
	}

	b.WriteString("\n")
}

func renderKubeFindings(b *strings.Builder, d kubeDiagnosisData) {
	fmt.Fprintf(b, "CHECKUP FINDINGS (%d)\n", len(d.Findings)+d.FindingsMore)

	for _, note := range d.CheckupNotes {
		fmt.Fprintf(b, "  %s\n", note)
	}

	if len(d.Findings) == 0 && len(d.CheckupNotes) == 0 {
		b.WriteString("  none\n")
	}

	for _, f := range d.Findings {
		b.WriteString("  " + kubeFindingLine(f) + "\n")
	}

	if d.FindingsMore > 0 {
		fmt.Fprintf(b, "  and %d more\n", d.FindingsMore)
	}

	b.WriteString("\n")
}

func kubeFindingLine(f checkupFinding) string {
	line := "[" + f.Severity + "] " + f.Kind

	switch {
	case f.Namespace != "" && f.Name != "":
		line += " " + f.Namespace + "/" + f.Name
	case f.Name != "":
		line += " " + f.Name
	}

	var details []string

	if f.Node != "" {
		details = append(details, "on "+f.Node)
	}

	if f.Extra != "" {
		details = append(details, f.Extra)
	}

	if f.Reason != "" {
		details = append(details, f.Reason)
	}

	if f.Value != 0 {
		details = append(details, fmt.Sprintf("value %.0f", f.Value))
	}

	if f.Count != 0 {
		details = append(details, fmt.Sprintf("count %d", f.Count))
	}

	if f.Message != "" {
		details = append(details, clipUTF8(f.Message, diagnosisMaxLineLen))
	}

	if len(details) > 0 {
		line += ": " + strings.Join(details, ", ")
	}

	return line
}

func renderKubePlugins(b *strings.Builder, d kubeDiagnosisData) {
	if len(d.Plugins) == 0 {
		return
	}

	fmt.Fprintf(b, "PLUGIN FINDINGS, CRITICAL (%d)\n", len(d.Plugins))

	for _, f := range d.Plugins {
		line := "  " + f.PluginName + ": " + f.Title
		if f.Detail != "" {
			line += ": " + clipUTF8(f.Detail, diagnosisMaxLineLen)
		}

		if f.Ref != nil {
			line += " (" + strings.TrimPrefix(f.Ref.Namespace+"/"+f.Ref.Name, "/") + ")"
		}

		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
}

func renderKubeHelm(b *strings.Builder, d kubeDiagnosisData) {
	if len(d.Helm) == 0 {
		return
	}

	fmt.Fprintf(b, "HELM RELEASES FAILED (%d)\n", len(d.Helm))

	for _, r := range d.Helm {
		fmt.Fprintf(b, "  %s/%s: revision %d, %s ago\n", r.Namespace, r.Name, r.Revision, formatAge(d.At.Sub(time.UnixMilli(r.Updated))))
	}

	b.WriteString("\n")
}
