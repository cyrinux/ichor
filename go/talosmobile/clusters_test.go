package talosmobile

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
	out, err := MergeConfig(mergeStored, clustersOther)
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

func TestMergeConfigUpdatesSameCluster(t *testing.T) {
	out, err := MergeConfig(mergeStored, clustersRenewedLab)
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

func TestMergeConfigSkipsTakenSuffixes(t *testing.T) {
	once, err := MergeConfig(mergeStored, clustersOther)
	if err != nil {
		t.Fatal(err)
	}

	third := strings.ReplaceAll(clustersOther, "b3RoZXItY2E=", "dGhpcmQtY2E=")

	out, err := MergeConfig(once, third)
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)

	if got := strings.Join(sortedContextNames(cfg), ","); got != "lab,lab-1,lab-2,prod,staging" {
		t.Fatalf("contexts = %s", got)
	}

	if cfg.Context != "lab-2" {
		t.Fatalf("current = %q", cfg.Context)
	}
}

func TestMergeConfigErrors(t *testing.T) {
	if _, err := MergeConfig("not: [yaml", clustersOther); err == nil {
		t.Fatal("bad stored config accepted")
	}

	if _, err := MergeConfig(mergeStored, "not: [yaml"); err == nil {
		t.Fatal("bad imported config accepted")
	}

	if _, err := MergeConfig(mergeStored, "context: x\ncontexts: {}\n"); err == nil {
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
