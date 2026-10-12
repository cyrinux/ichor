package ichorgo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type discoverProgressView struct {
	Running                     bool
	Projects, Scanned, Clusters int
	Current, Found, Refused     []string
}

func readDiscoverProgress(t *testing.T) discoverProgressView {
	t.Helper()

	raw, err := DiscoverProgress()
	if err != nil {
		t.Fatal(err)
	}

	var p discoverProgressView
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}

	return p
}

// current is what is being read right now; found is capped, each name said once.
func TestDiscoverProgressCurrentAndFound(t *testing.T) {
	defer startDiscoveryProgress()()

	readA := discoveryProgress.reading("eu-west-3")
	discoveryProgress.reading("eu-north-1")

	if p := readDiscoverProgress(t); !p.Running || !slices.Equal(p.Current, []string{"eu-north-1", "eu-west-3"}) {
		t.Fatalf("reading two regions: %+v", p)
	}

	readA()

	if p := readDiscoverProgress(t); !slices.Equal(p.Current, []string{"eu-north-1"}) {
		t.Fatalf("one region read: %+v", p)
	}

	discoveryProgress.addFound("web.eu-west-3.eks", "web.eu-west-3.eks")

	for i := range 30 {
		discoveryProgress.addFound(fmt.Sprintf("app-%d.eu-west-3.eks", i))
	}

	p := readDiscoverProgress(t)
	if len(p.Found) != discoveryFoundMax || p.Found[0] != "web.eu-west-3.eks" || p.Found[1] != "app-0.eu-west-3.eks" {
		t.Fatalf("found %v", p.Found)
	}

	// A new discovery forgets the last one's.
	startDiscoveryProgress()

	if p := readDiscoverProgress(t); len(p.Current) != 0 || len(p.Found) != 0 {
		t.Fatalf("restarted: %+v", p)
	}
}

// Project and cluster names go through the privacy mask like any result.
func TestDiscoverProgressMasked(t *testing.T) {
	enableMask(t, "acme")

	defer startDiscoveryProgress()()

	discoveryProgress.reading("acme-shop-prod")
	discoveryProgress.addFound("acme-web.europe-west1.gke")

	raw, err := DiscoverProgress()
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(strings.ToLower(raw), "acme") {
		t.Fatalf("not masked: %s", raw)
	}
}
