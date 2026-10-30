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

	lines := []string{"starting " + pod, "listening on :8080", "connected to the database"}
	if previous {
		lines = append(lines, "panic: runtime error: invalid memory address or nil pointer dereference", "exit status 2")
	}

	for i, l := range lines {
		fmt.Fprintf(&b, "%s %s\n", start.Add(time.Duration(i)*time.Second).UTC().Format(time.RFC3339), l)
	}

	return b.String()
}
