package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// A second cluster: its own CA, one context named like one of mergeStored's.
const clustersOther = `context: lab
contexts:
  lab:
    endpoints: [10.2.0.1]
    ca: b3RoZXItY2E=
    crt: b3RoZXItY3J0
    key: b3RoZXIta2V5
  staging:
    endpoints: [10.3.0.1]
    ca: c3RhZ2luZy1jYQ==
    crt: c3RhZ2luZy1jcnQ=
    key: c3RhZ2luZy1rZXk=
`

// The stored "lab" cluster again (same CA), with a renewed certificate and a new endpoint.
const clustersRenewedLab = `context: lab
contexts:
  lab:
    endpoints: [10.1.0.9]
    ca: bGFiLWNh
    crt: cmVuZXdlZC1jcnQ=
    key: cmVuZXdlZC1rZXk=
`

func mustConfig(t *testing.T, yaml string) *clientconfig.Config {
	t.Helper()

	cfg, err := clientconfig.FromString(yaml)
	if err != nil {
		t.Fatal(err)
	}

	return cfg
}

func TestMergeConfigAddsClusters(t *testing.T) {
	out, err := MergeConfig(mergeStored, clustersOther, "")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab,lab-1,prod,staging" {
		t.Fatalf("contexts = %s", got)
	}

	// Same name, another CA: another cluster, kept next to the stored one.
	if lab := cfg.Contexts["lab"]; lab.Crt != "bGFiLWNydA==" || lab.Endpoints[0] != "10.1.0.1" {
		t.Fatalf("stored lab changed: %+v", lab)
	}

	if added := cfg.Contexts["lab-1"]; added.Crt != "b3RoZXItY3J0" || added.Endpoints[0] != "10.2.0.1" {
		t.Fatalf("imported lab wrong: %+v", added)
	}

	if prod := cfg.Contexts["prod"]; prod.Crt != "b2xkLWNydA==" {
		t.Fatalf("prod changed: %+v", prod)
	}

	// The imported config's current context becomes the current one, under its new name.
	if cfg.Context != "lab-1" {
		t.Fatalf("current = %q", cfg.Context)
	}
}

// The same cluster again (same name and CA): kept next to the stored one unless the user
// chose to replace it.
func TestMergeConfigKeepsSameClusterByDefault(t *testing.T) {
	out, err := MergeConfig(mergeStored, clustersRenewedLab, "")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab,lab-1,prod" {
		t.Fatalf("contexts = %s", got)
	}

	if lab := cfg.Contexts["lab"]; lab.Crt != "bGFiLWNydA==" {
		t.Fatalf("stored lab overwritten: %+v", lab)
	}

	if cfg.Context != "lab-1" {
		t.Fatalf("current = %q", cfg.Context)
	}
}

func TestMergeConfigReplacesSameClusterWhenChosen(t *testing.T) {
	out, err := MergeConfig(mergeStored, clustersRenewedLab, `[{"index":0,"replace":true}]`)
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab,prod" {
		t.Fatalf("contexts = %s", got)
	}

	if lab := cfg.Contexts["lab"]; lab.Crt != "cmVuZXdlZC1jcnQ=" || lab.Endpoints[0] != "10.1.0.9" {
		t.Fatalf("lab not updated: %+v", lab)
	}

	if cfg.Context != "lab" {
		t.Fatalf("current = %q", cfg.Context)
	}
}

func TestMergeConfigUsesChosenName(t *testing.T) {
	out, err := MergeConfig(mergeStored, clustersOther, `[{"index":0,"name":"  home lab  "}]`)
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "home lab,lab,prod,staging" {
		t.Fatalf("contexts = %s", got)
	}

	if added := cfg.Contexts["home lab"]; added.Crt != "b3RoZXItY3J0" {
		t.Fatalf("imported lab wrong: %+v", added)
	}

	if cfg.Context != "home lab" {
		t.Fatalf("current = %q", cfg.Context)
	}
}

func TestMergeConfigRefusesOverwrites(t *testing.T) {
	// A chosen name held by a stored cluster, or by another context of the imported config.
	for _, name := range []string{"prod", "staging"} {
		if _, err := MergeConfig(mergeStored, clustersOther, `[{"index":0,"name":"`+name+`"}]`); err == nil {
			t.Fatalf("name %q taken, accepted", name)
		}
	}

	// Another cluster (another CA) cannot replace the stored one.
	if _, err := MergeConfig(mergeStored, clustersOther, `[{"index":0,"replace":true}]`); err == nil {
		t.Fatal("replacing another cluster accepted")
	}

	if _, err := MergeConfig(mergeStored, clustersOther, `{`); err == nil {
		t.Fatal("bad choices accepted")
	}
}

func TestMergeConfigSuffixAvoidsImportedNames(t *testing.T) {
	// The imported config has both "lab" (clashing) and "lab-1": lab goes in as lab-2.
	added := strings.Replace(clustersOther, "  staging:", "  lab-1:", 1)

	out, err := MergeConfig(mergeStored, added, "")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab,lab-1,lab-2,prod" {
		t.Fatalf("contexts = %s", got)
	}

	if crt := cfg.Contexts["lab-2"].Crt; crt != "b3RoZXItY3J0" {
		t.Fatalf("lab-2 = %s", crt)
	}
}

func TestImportConflicts(t *testing.T) {
	out, err := ImportConflicts(mergeStored, clustersOther)
	if err != nil {
		t.Fatal(err)
	}

	// lab clashes with another cluster; staging is new.
	if out != `[{"index":0,"suggested":"lab-1"}]` {
		t.Fatalf("conflicts = %s", out)
	}

	out, err = ImportConflicts(mergeStored, clustersRenewedLab)
	if err != nil {
		t.Fatal(err)
	}

	if out != `[{"index":0,"suggested":"lab-1","sameAs":"lab"}]` {
		t.Fatalf("conflicts = %s", out)
	}

	out, err = ImportConflicts(mergeStored, strings.ReplaceAll(clustersOther, "lab", "dev"))
	if err != nil {
		t.Fatal(err)
	}

	if out != `[]` {
		t.Fatalf("conflicts = %s", out)
	}

	if _, err := ImportConflicts(mergeStored, "not: [yaml"); err == nil {
		t.Fatal("bad imported config accepted")
	}
}

func TestMergeConfigSkipsTakenSuffixes(t *testing.T) {
	once, err := MergeConfig(mergeStored, clustersOther, "")
	if err != nil {
		t.Fatal(err)
	}

	third := strings.ReplaceAll(clustersOther, "b3RoZXItY2E=", "dGhpcmQtY2E=")

	out, err := MergeConfig(once, third, "")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	// Its staging is the stored one again: without a choice it is kept next to it too.
	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab,lab-1,lab-2,prod,staging,staging-1" {
		t.Fatalf("contexts = %s", got)
	}

	if cfg.Context != "lab-2" {
		t.Fatalf("current = %q", cfg.Context)
	}
}

func TestMergeConfigReplacesSuffixedCluster(t *testing.T) {
	once, err := MergeConfig(mergeStored, clustersOther, "")
	if err != nil {
		t.Fatal(err)
	}

	// The other "lab" again, renewed: it went in as lab-1, so lab-1 is what it may replace.
	renewed := strings.ReplaceAll(clustersOther, "b3RoZXItY3J0", "cmVuZXdlZA==")

	conflicts, err := ImportConflicts(once, renewed)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(conflicts, `"sameAs":"lab-1"`) {
		t.Fatalf("conflicts = %s", conflicts)
	}

	out, err := MergeConfig(once, renewed, `[{"index":0,"replace":true},{"index":1,"replace":true}]`)
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab,lab-1,prod,staging" {
		t.Fatalf("contexts = %s", got)
	}

	if crt := cfg.Contexts["lab-1"].Crt; crt != "cmVuZXdlZA==" {
		t.Fatalf("lab-1 not updated: %s", crt)
	}

	if crt := cfg.Contexts["lab"].Crt; crt != "bGFiLWNydA==" {
		t.Fatalf("stored lab changed: %s", crt)
	}
}

// Clusters added over time, screenshot mode on: no two contexts may be shown under the
// same name, else an action on one would reach the other.
func TestMaskedContextNamesStayDistinctAcrossConfigs(t *testing.T) {
	SetPrivacyMask(true, "")
	defer SetPrivacyMask(false, "")

	// A cluster masked as "homelab", then a real one named "homelab" (a generic name).
	first := strings.ReplaceAll(mergeGenerated, "prod", "mycluster")
	second := strings.ReplaceAll(mergeGenerated, "prod", "homelab")
	// And the reverse: a real generic name first, then a cluster whose fake would be it.
	third := strings.ReplaceAll(mergeGenerated, "prod", "othercluster")

	shown := map[string]string{}

	for _, cfg := range []string{first, second, third} {
		real := mustConfig(t, cfg).Context

		privacy.learnConfig(cfg)
		privacy.mu.Lock()
		fake := privacy.contexts[real]
		privacy.mu.Unlock()

		if other, dup := shown[fake]; dup {
			t.Fatalf("%s and %s are both shown as %s", other, real, fake)
		}

		shown[fake] = real

		if back := privacy.unmaskContext(fake); back != real {
			t.Fatalf("%s maps back to %s, want %s", fake, back, real)
		}
	}
}

func TestMergeConfigErrors(t *testing.T) {
	if _, err := MergeConfig("not: [yaml", clustersOther, ""); err == nil {
		t.Fatal("bad stored config accepted")
	}

	if _, err := MergeConfig(mergeStored, "not: [yaml", ""); err == nil {
		t.Fatal("bad imported config accepted")
	}

	if _, err := MergeConfig(mergeStored, "context: x\ncontexts: {}\n", ""); err == nil {
		t.Fatal("imported config without contexts accepted")
	}
}

func TestRemoveContext(t *testing.T) {
	out, err := RemoveContext(mergeStored, "prod")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab" {
		t.Fatalf("contexts = %s", got)
	}

	// The removed context was the current one: the config must not keep a dangling name.
	if cfg.Context != "lab" {
		t.Fatalf("current = %q", cfg.Context)
	}

	if strings.Contains(out, "b2xkLWtleQ==") {
		t.Fatal("removed context's key is still in the config")
	}

	out, err = RemoveContext(mergeStored, "lab")
	if err != nil {
		t.Fatal(err)
	}

	if cfg := mustConfig(t, out); cfg.Context != "prod" || len(cfg.Contexts) != 1 {
		t.Fatalf("current = %q, contexts = %d", cfg.Context, len(cfg.Contexts))
	}
}

func TestRemoveContextErrors(t *testing.T) {
	if _, err := RemoveContext(mergeStored, "missing"); err == nil {
		t.Fatal("missing context accepted")
	}

	if _, err := RemoveContext(mergeGenerated, "prod"); err == nil {
		t.Fatal("removing the last context accepted")
	}

	if _, err := RemoveContext("not: [yaml", "prod"); err == nil {
		t.Fatal("bad config accepted")
	}
}

func TestContextFingerprint(t *testing.T) {
	cfg := mustConfig(t, mergeStored)
	prod := contextFingerprint("prod", cfg.Contexts["prod"])

	if len(prod) != fingerprintLength || strings.Trim(prod, "abcdefghijklmnopqrstuvwxyz") != "" {
		t.Fatalf("fingerprint = %q", prod)
	}

	if prod == contextFingerprint("lab", cfg.Contexts["lab"]) {
		t.Fatal("two clusters share a fingerprint")
	}

	// Same name in another cluster (another CA): another fingerprint.
	if prod == contextFingerprint("prod", cfg.Contexts["lab"]) {
		t.Fatal("fingerprint ignores the CA")
	}

	// A renewed certificate or moved endpoints keep it.
	renewed, err := ReplaceContextCredentials(mergeStored, strings.ReplaceAll(mergeGenerated, "bmV3LWNh", "b2xkLWNh"), "prod")
	if err != nil {
		t.Fatal(err)
	}

	if got := contextFingerprint("prod", mustConfig(t, renewed).Contexts["prod"]); got != prod {
		t.Fatalf("fingerprint changed with the certificate: %q != %q", got, prod)
	}
}

func TestFingerprintSurvivesScreenshotMode(t *testing.T) {
	cfg := testConfig(t, time.Now().Add(time.Hour))

	fingerprints := func() []string {
		t.Helper()

		out, err := ParseConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}

		var summary configSummary
		if err := json.Unmarshal([]byte(out), &summary); err != nil {
			t.Fatal(err)
		}

		got := make([]string, 0, len(summary.Contexts))
		for _, c := range summary.Contexts {
			got = append(got, c.Fingerprint)
		}

		return got
	}

	plain := fingerprints()

	SetPrivacyMask(true, "lab,other")
	defer SetPrivacyMask(false, "")

	if masked := fingerprints(); strings.Join(masked, ",") != strings.Join(plain, ",") || len(plain) != 2 {
		t.Fatalf("fingerprints differ when masked: %v != %v", masked, plain)
	}
}
