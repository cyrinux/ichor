package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The health check helper: a short report about a failed cluster health check (its lines,
// node readiness and recent warning/error events), asked with its own instructions. It is
// a *Diagnosis, so masking, asking and revealing work as for the full report.

const (
	// Quick on purpose: the operator is waiting on the health page.
	healthExplainTimeout = 25 * time.Second
	healthExplainLines   = 40
	healthExplainEvents  = 15
)

// demoHealthLines are the lines runHealth streams for the demo cluster.
var demoHealthLines = []string{
	"waiting for etcd to be healthy: OK",
	"waiting for all k8s nodes to report ready: OK",
	"waiting for all control plane components to be ready: OK",
}

// CollectHealthExplanation builds the report the health check helper sends: linesJSON is
// the JSON array of the check's progress lines as shown, failure the message it ended
// with. It reads node readiness and recent events (os:reader calls only, no logs, no etcd,
// no GitOps: the point is speed). Masking works as in CollectDiagnosis.
//
// kubeServer is accepted like CollectDiagnosis's but not used: nothing is read from the
// Kubernetes API.
func CollectHealthExplanation(configYAML, contextName, kubeServer, linesJSON, failure string, anonymize bool) (d *Diagnosis, err error) {
	defer maskErr(&err)

	var lines []string
	if strings.TrimSpace(linesJSON) != "" {
		if err := json.Unmarshal([]byte(linesJSON), &lines); err != nil {
			return nil, fmt.Errorf("health check lines: %w", err)
		}
	}

	contextName = unmaskContext(configYAML, contextName)

	d = &Diagnosis{kind: diagnosisHealth}
	d.screenshotMode, d.screenshotResets = privacy.state()

	var data diagnosisData

	if isTalosDemoContext(configYAML, contextName) {
		data = demoHealthData()

		if len(lines) == 0 {
			lines = demoHealthLines
		}
	} else {
		data, err = withSession(configYAML, contextName, healthExplainTimeout, func(ctx context.Context, s *session) (diagnosisData, error) {
			return collectHealthData(ctx, s), nil
		})
		if err != nil {
			return nil, err
		}
	}

	d.report = renderHealthExplanation(lines, failure, data)
	d.applyMask(configYAML, anonymize, data)

	return d, nil
}

// collectHealthData reads what the helper needs: node overviews and recent problem events.
func collectHealthData(ctx context.Context, s *session) diagnosisData {
	nodes := targetNodes(s.context)
	data := diagnosisData{At: time.Now(), Nodes: make([]nodeDiagnosis, len(nodes))}

	overviews := make([]nodeOverview, len(nodes))
	domains := make([]string, len(nodes))

	forEachNode(nodes, func(i int, node string) {
		p := probeNode(ctx, s.client, node)
		overviews[i], domains[i] = buildNodeOverview(node, p), p.domain
	})

	data.hosts, data.domains = clusterHostEntries(ctx, s.client, overviews), domains

	var reachable []string

	for i, o := range overviews {
		data.Nodes[i] = nodeDiagnosis{nodeOverview: o}

		if o.Reachable {
			reachable = append(reachable, o.Node)
		}
	}

	events, note := recentProblemEvents(ctx, s.client, reachable)
	data.Events, data.EventsNote = latestEvents(events, healthExplainEvents), note

	return data
}

// demoHealthData is the demo cluster's sample: two nodes and one event, no network.
func demoHealthData() diagnosisData {
	nodes := demoNodes()
	cp, worker := nodes[0], nodes[3]
	at := time.Now()

	return diagnosisData{
		At:    at,
		Nodes: []nodeDiagnosis{{nodeOverview: cp}, {nodeOverview: worker}},
		Events: []nodeEvent{{
			Node: worker.Node, At: at.Add(-3 * time.Minute).UnixMilli(), Kind: "service", Subject: "kubelet",
			Action: "waiting", Message: "Health check failed: connection refused", Severity: "warning",
		}},
		hosts: []hostEntry{
			{address: cp.Node, hostname: cp.Hostname, role: cp.Role},
			{address: worker.Node, hostname: worker.Hostname, role: worker.Role},
		},
	}
}

// renderHealthExplanation writes the report: the check's last lines (the failing one
// marked), the nodes, and the recent warning and error events.
func renderHealthExplanation(lines []string, failure string, d diagnosisData) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Talos cluster health check, %s\n\n", d.At.UTC().Format("2006-01-02 15:04 MST"))

	if len(lines) > healthExplainLines {
		fmt.Fprintf(&b, "HEALTH CHECK (last %d of %d lines)\n", healthExplainLines, len(lines))
		lines = lines[len(lines)-healthExplainLines:]
	} else {
		b.WriteString("HEALTH CHECK\n")
	}

	for i, line := range lines {
		line = "  " + clipUTF8(line, diagnosisMaxLineLen)
		if failure != "" && i == len(lines)-1 {
			line += "  ← failed"
		}

		b.WriteString(line + "\n")
	}

	if failure != "" {
		fmt.Fprintf(&b, "Failure: %s\n", clipUTF8(failure, diagnosisMaxLineLen))
	}

	b.WriteString("\n")
	renderNodeList(&b, d.Nodes)
	renderEvents(&b, d)

	return neutralizeTags(strings.ToValidUTF8(strings.TrimRight(b.String(), "\n"), "�")) + "\n"
}
