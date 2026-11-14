package ichorgo

import (
	"fmt"
	"strings"
	"time"
)

// demoPodLog is a few lines of a container's log; the previous run ends with a crash.
func demoPodLog(pod string, previous bool) string {
	start := time.Now().Add(-10 * time.Minute)

	var b strings.Builder

	// Two lines as logrus writes them with colours forced on: the apps draw them coloured.
	lines := []string{
		"starting " + pod, "listening on :8080", "connected to the database",
		"\x1b[36mINFO\x1b[0m[0042] request done    \x1b[36mclient_ip\x1b[0m=10.0.0.7 \x1b[36mstatus\x1b[0m=200",
		"\x1b[33mWARN\x1b[0m[0043] slow query       \x1b[33mtook\x1b[0m=2.1s",
	}
	// Steady traffic, the same few messages with another address or duration each time: what
	// the Top messages view folds into a handful of rows.
	for i := range 18 {
		lines = append(lines, fmt.Sprintf("GET /orders/%d from 10.0.%d.%d took %dms", 1000+i*7, i%3, 10+i, 12+i*3))
		if i%6 == 5 {
			lines = append(lines, fmt.Sprintf("slow query on orders took %d.%ds", 1+i/6, i%10))
		}
	}
	if previous {
		lines = append(lines, "panic: runtime error: invalid memory address or nil pointer dereference", "exit status 2")
	}

	for i, l := range lines {
		fmt.Fprintf(&b, "%s %s\n", start.Add(time.Duration(i)*time.Second).UTC().Format(time.RFC3339), l)
	}

	return b.String()
}

// demoWorkloadLogs is an hour and a half of the demo pods of a workload, a line every few
// minutes from each, a few of them timing out.
func demoWorkloadLogs(wk workloadKind, namespace, name string, match func(string) bool, sinceMinutes int) workloadLogs {
	messages := []string{
		`level=info msg="request done" status=200`,
		`level=info msg="connected to the database"`,
		`level=warn msg="slow query" took=2.1s`,
		`level=error msg="upstream timeout" after=30s`,
	}

	now := time.Now()

	var reads []podLogText

	for _, pod := range demoSelectedPods("", func(p kubePod) bool {
		return p.Namespace == namespace && ownedByWorkload(p.Owner, wk.kind, name)
	}).Pods {
		var b strings.Builder

		// Each pod its own minutes, so that their lines interleave.
		for i, minutes := 0, 90+len(reads); minutes > 0; i, minutes = i+1, minutes-7 {
			if sinceMinutes == 0 || minutes <= sinceMinutes {
				at := now.Add(-time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339Nano)
				fmt.Fprintf(&b, "%s %s\n", at, messages[i%len(messages)])
			}
		}

		container := ""
		if len(pod.ContainerNames) > 0 {
			container = pod.ContainerNames[0]
		}

		reads = append(reads, podLogText{name: pod.Name, container: container, log: b.String()})
	}

	return mergeWorkloadLogs(reads, match, sinceMinutes)
}
