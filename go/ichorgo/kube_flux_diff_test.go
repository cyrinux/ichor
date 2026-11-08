package ichorgo

import (
	"strings"
	"testing"
)

func TestKubeFluxDiffValidates(t *testing.T) {
	if _, err := KubeFluxDiff("", "", "", "HelmRelease", "flux-system", "apps"); err == nil || !strings.Contains(err.Error(), "only a Kustomization") {
		t.Fatalf("got %v", err)
	}

	if _, err := KubeFluxDiff("", "", "", "Kustomization", "flux-system", "../x"); err == nil {
		t.Fatal("bad name accepted")
	}
}

func TestDemoFluxDiff(t *testing.T) {
	d := demoFluxDiff("flux-system", "apps")

	seen := map[string]bool{}
	for _, r := range d.Resources {
		seen[r.Change] = true

		if (r.Change == diffChangeChanged || r.Change == diffChangeCreated || r.Change == diffChangeDeleted) && !strings.HasPrefix(r.Diff, "--- live\n+++ wanted\n@@ ") {
			t.Errorf("%s %s: diff %q", r.Kind, r.Name, r.Diff)
		}
	}

	for _, c := range []string{diffChangeCreated, diffChangeChanged, diffChangeDeleted, diffChangeEncrypted, diffChangeUnchanged} {
		if !seen[c] {
			t.Errorf("the demo shows no %s object", c)
		}
	}
}
