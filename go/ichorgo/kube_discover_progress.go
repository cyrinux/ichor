package ichorgo

import (
	"encoding/json"
	"slices"
	"sync"
	"sync/atomic"
)

// Cloud discovery progress: a Google account's discovery reads hundreds of projects, so the
// apps poll DiscoverProgress while DiscoverClusters runs and show how far it got. One
// discovery runs at a time (the import sheet is modal); a second one restarts the counts.

var discoveryProgress discoveryCounts

type discoveryCounts struct {
	running                     atomic.Bool
	projects, scanned, clusters atomic.Int64

	mu sync.Mutex
	// refused is the regions an EKS discovery may not list clusters in.
	refused []string
}

// addRefused notes a region that refused the discovery, once.
func (p *discoveryCounts) addRefused(region string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !slices.Contains(p.refused, region) {
		p.refused = append(p.refused, region)
	}
}

// startDiscoveryProgress resets the counts for a discovery that starts now; the returned func
// marks it done.
func startDiscoveryProgress() func() {
	discoveryProgress.projects.Store(0)
	discoveryProgress.scanned.Store(0)
	discoveryProgress.clusters.Store(0)
	discoveryProgress.mu.Lock()
	discoveryProgress.refused = nil
	discoveryProgress.mu.Unlock()
	discoveryProgress.running.Store(true)

	return func() { discoveryProgress.running.Store(false) }
}

// DiscoverProgress is, as JSON, how far the running DiscoverClusters got:
// {"running":true,"projects":340,"scanned":120,"clusters":3,"refused":["eu-north-1"]}.
// projects is 0 while the projects are still being listed, and for a provider that reads no
// projects; an EKS discovery with IAM Identity Center counts its regions (× roles) as
// projects, and lists in refused the regions it may not read.
func DiscoverProgress() (out string, err error) {
	defer maskResult(&out, &err)

	discoveryProgress.mu.Lock()
	refused := append([]string{}, discoveryProgress.refused...)
	discoveryProgress.mu.Unlock()

	slices.Sort(refused)

	raw, err := json.Marshal(map[string]any{
		"refused":  refused,
		"running":  discoveryProgress.running.Load(),
		"projects": discoveryProgress.projects.Load(),
		"scanned":  discoveryProgress.scanned.Load(),
		"clusters": discoveryProgress.clusters.Load(),
	})

	return string(raw), err
}
