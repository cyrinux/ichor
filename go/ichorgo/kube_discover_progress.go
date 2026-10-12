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

// discoveryFoundMax caps the cluster names DiscoverProgress lists; clusters counts them all.
const discoveryFoundMax = 20

type discoveryCounts struct {
	running                     atomic.Bool
	projects, scanned, clusters atomic.Int64

	mu sync.Mutex
	// refused is the regions an EKS discovery may not list clusters in.
	refused []string
	// current is the projects (or EKS regions) being read right now.
	current []string
	// found is the names of the clusters found so far, in order, at most discoveryFoundMax.
	found []string
}

// addRefused notes a region that refused the discovery, once.
func (p *discoveryCounts) addRefused(region string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !slices.Contains(p.refused, region) {
		p.refused = append(p.refused, region)
	}
}

// reading notes a project or region being read; the returned func notes it read.
func (p *discoveryCounts) reading(what string) func() {
	p.mu.Lock()
	p.current = append(p.current, what)
	p.mu.Unlock()

	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()

		if i := slices.Index(p.current, what); i >= 0 {
			p.current = slices.Delete(p.current, i, i+1)
		}
	}
}

// addFound notes the names of clusters just found, once each.
func (p *discoveryCounts) addFound(names ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, name := range names {
		if len(p.found) < discoveryFoundMax && !slices.Contains(p.found, name) {
			p.found = append(p.found, name)
		}
	}
}

// startDiscoveryProgress resets the counts for a discovery that starts now; the returned func
// marks it done.
func startDiscoveryProgress() func() {
	discoveryProgress.projects.Store(0)
	discoveryProgress.scanned.Store(0)
	discoveryProgress.clusters.Store(0)
	discoveryProgress.mu.Lock()
	discoveryProgress.refused, discoveryProgress.current, discoveryProgress.found = nil, nil, nil
	discoveryProgress.mu.Unlock()
	discoveryProgress.running.Store(true)

	return func() { discoveryProgress.running.Store(false) }
}

// DiscoverProgress is, as JSON, how far the running DiscoverClusters got:
// {"running":true,"projects":340,"scanned":120,"clusters":3,"refused":["eu-north-1"],
// "current":["project-a","project-b"],"found":["web.europe-west1.gke"]}.
// projects is 0 while the projects are still being listed, and for a provider that reads no
// projects; an EKS discovery with IAM Identity Center counts its regions (× roles) as
// projects, lists in refused the regions it may not read, and in current the regions being
// read. found names the first clusters found (clusters counts them all).
func DiscoverProgress() (out string, err error) {
	defer maskResult(&out, &err)

	discoveryProgress.mu.Lock()
	refused := append([]string{}, discoveryProgress.refused...)
	current := append([]string{}, discoveryProgress.current...)
	found := append([]string{}, discoveryProgress.found...)
	discoveryProgress.mu.Unlock()

	slices.Sort(refused)
	slices.Sort(current)
	current = slices.Compact(current)

	raw, err := json.Marshal(map[string]any{
		"refused":  refused,
		"current":  current,
		"found":    found,
		"running":  discoveryProgress.running.Load(),
		"projects": discoveryProgress.projects.Load(),
		"scanned":  discoveryProgress.scanned.Load(),
		"clusters": discoveryProgress.clusters.Load(),
	})

	return string(raw), err
}
