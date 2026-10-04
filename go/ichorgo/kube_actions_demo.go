package ichorgo

import (
	"fmt"
	"strings"
	"time"
)

// demoDeploymentRevisions is a Deployment with three kept revisions, the newest current.
func demoDeploymentRevisions() map[string][]deploymentRevision {
	now := time.Now()

	return map[string][]deploymentRevision{"revisions": {
		{Revision: 7, ReplicaSet: "frontend-7d9c6", Created: now.Add(-2 * time.Hour).UnixMilli(), Images: []string{"ghcr.io/example/frontend:2.4.1"}, Replicas: 3, Current: true},
		{Revision: 6, ReplicaSet: "frontend-5b8f4", Created: now.Add(-50 * time.Hour).UnixMilli(), Images: []string{"ghcr.io/example/frontend:2.4.0"}, ChangeCause: "bump to 2.4.0"},
		{Revision: 5, ReplicaSet: "frontend-6c2a1", Created: now.Add(-9 * 24 * time.Hour).UnixMilli(), Images: []string{"ghcr.io/example/frontend:2.3.7"}},
	}}
}

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
