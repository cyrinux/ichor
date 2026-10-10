package ichorgo

import (
	"encoding/json"
	"sync/atomic"
)

// Cloud discovery progress: a Google account's discovery reads hundreds of projects, so the
// apps poll DiscoverProgress while DiscoverClusters runs and show how far it got. One
// discovery runs at a time (the import sheet is modal); a second one restarts the counts.

var discoveryProgress struct {
	running                     atomic.Bool
	projects, scanned, clusters atomic.Int64
}

// startDiscoveryProgress resets the counts for a discovery that starts now; the returned func
// marks it done.
func startDiscoveryProgress() func() {
	discoveryProgress.projects.Store(0)
	discoveryProgress.scanned.Store(0)
	discoveryProgress.clusters.Store(0)
	discoveryProgress.running.Store(true)

	return func() { discoveryProgress.running.Store(false) }
}

// DiscoverProgress is, as JSON, how far the running DiscoverClusters got:
// {"running":true,"projects":340,"scanned":120,"clusters":3}. projects is 0 while the
// projects are still being listed, and for a provider that reads no projects.
func DiscoverProgress() (out string, err error) {
	defer maskResult(&out, &err)

	raw, err := json.Marshal(map[string]any{
		"running":  discoveryProgress.running.Load(),
		"projects": discoveryProgress.projects.Load(),
		"scanned":  discoveryProgress.scanned.Load(),
		"clusters": discoveryProgress.clusters.Load(),
	})

	return string(raw), err
}
