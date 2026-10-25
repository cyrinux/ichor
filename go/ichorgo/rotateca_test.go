package ichorgo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/crypto/x509"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	secretsres "github.com/siderolabs/talos/pkg/machinery/resources/secrets"
)

func TestComputeCARotationPlanBlockers(t *testing.T) {
	in := caRotationInput{context: "c", nodes: []caRotationNode{
		{Node: "10.0.0.1", Hostname: "cp-1", Role: "controlplane", Version: "v1.14.2", Reachable: true},
		{Node: "10.0.0.2", Hostname: "cp-2", Role: roleUnknown, Problem: "does not answer: every node must be reachable, or it would stop trusting this phone"},
		{Node: "10.0.0.3", Hostname: "w-1", Role: "worker", Version: "v1.6.7", Reachable: true, Problem: "runs Talos v1.6.7: the rotation needs 1.7 or newer on every node"},
	}, discovered: true, ca: &caRotationCA{Fingerprint: "abcd"}}

	plan := computeCARotationPlan(in)

	if plan.Ready {
		t.Fatal("a plan with problems must not be ready")
	}

	if len(plan.Blockers) != 2 || !strings.HasPrefix(plan.Blockers[0], "cp-2 (10.0.0.2) does not answer") || !strings.Contains(plan.Blockers[1], "w-1 (10.0.0.3) runs Talos v1.6.7") {
		t.Fatalf("blockers = %q", plan.Blockers)
	}

	// Discovery answered: no warning about a partial list; the two standing warnings stay.
	if len(plan.Warnings) != 2 {
		t.Fatalf("warnings = %q", plan.Warnings)
	}
}

func TestComputeCARotationPlanReadyAndWarnings(t *testing.T) {
	in := caRotationInput{context: "c", nodes: []caRotationNode{
		{Node: "10.0.0.1", Hostname: "cp-1", Role: "controlplane", Version: "v1.14.2", Reachable: true, AcceptedCAs: 1},
		{Node: "10.0.0.3", Hostname: "w-1", Role: "worker", Version: "v1.14.2", Reachable: true},
	}, ca: &caRotationCA{Fingerprint: "abcd"}}

	plan := computeCARotationPlan(in)

	if !plan.Ready || len(plan.Blockers) != 0 {
		t.Fatalf("ready = %v, blockers = %q", plan.Ready, plan.Blockers)
	}

	joined := strings.Join(plan.Warnings, "\n")
	for _, want := range []string{"only the 2 node(s) this talosconfig names", "other talosconfig", "secrets bundle", "1 CA(s) accepted besides"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings miss %q:\n%s", want, joined)
		}
	}
}

func TestComputeCARotationPlanNothingToRotate(t *testing.T) {
	plan := computeCARotationPlan(caRotationInput{context: "c", caErr: "the cluster's CA cannot be read: permission denied"})

	if plan.Ready || len(plan.Blockers) != 2 {
		t.Fatalf("blockers = %q", plan.Blockers)
	}

	if plan.Nodes == nil {
		t.Fatal("nodes must encode as an empty array")
	}
}

func TestTalosCARotationPlanDemo(t *testing.T) {
	out, err := TalosCARotationPlan(demoConfigForTest(t), "")
	if err != nil {
		t.Fatal(err)
	}

	var plan caRotationPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	if plan.Ready || len(plan.Nodes) != 5 || plan.CA == nil || !strings.Contains(strings.Join(plan.Blockers, " "), "demo") {
		t.Fatalf("demo plan = %+v", plan)
	}
}

func TestTalosCARotationPlanOmni(t *testing.T) {
	yaml := omniTalosconfigYAML("user@example.com", "https://omni.example.com", "")

	if _, err := TalosCARotationPlan(yaml, ""); !errors.Is(err, errOmniRotateCA) {
		t.Fatalf("err = %v", err)
	}
}

func TestTalosCARotationPlanLive(t *testing.T) {
	f, cfg, _ := caCluster(t)

	out, err := TalosCARotationPlan(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}

	var plan caRotationPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	if !plan.Ready || !plan.Discovered || len(plan.Nodes) != 3 {
		t.Fatalf("plan = %+v", plan)
	}

	if plan.CA == nil || len(plan.CA.Fingerprint) != caFingerprintLength || plan.CA.NotAfter <= plan.CA.NotBefore {
		t.Fatalf("ca = %+v", plan.CA)
	}

	for _, n := range plan.Nodes {
		if !n.Reachable || n.Hostname == "" || n.Version != "v1.14.2" || n.Problem != "" || n.AcceptedCAs != 0 {
			t.Errorf("node = %+v", n)
		}
	}

	if roles := []string{plan.Nodes[0].Role, plan.Nodes[1].Role, plan.Nodes[2].Role}; roles[0] != "controlplane" || roles[2] != "worker" {
		t.Errorf("roles = %v", roles)
	}

	if len(f.called("ApplyConfiguration")) != 0 {
		t.Fatal("the plan must not change anything")
	}
}

func TestTalosCARotationPlanUnreachableNode(t *testing.T) {
	_, cfg, _ := caCluster(t)
	cfg = strings.Replace(cfg, "            - "+caWorker+"\n", "            - "+caWorker+"\n            - 192.0.2.99\n", 1)

	out, err := TalosCARotationPlan(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}

	var plan caRotationPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	if plan.Ready || len(plan.Nodes) != 4 || !strings.Contains(strings.Join(plan.Blockers, " "), "192.0.2.99 does not answer") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestFingerprintLineageResolvesAndChains(t *testing.T) {
	resetFingerprintLineage()
	t.Cleanup(resetFingerprintLineage)

	if got := resolveFingerprint("abc"); got != "abc" {
		t.Fatalf("resolve(abc) = %q", got)
	}

	recordFingerprintLineage("second", "first")
	recordFingerprintLineage("third", "second")

	if got := resolveFingerprint("third"); got != "first" {
		t.Fatalf("resolve(third) = %q, want first", got)
	}

	// Recording a hash as its own root is a no-op.
	recordFingerprintLineage("first", "first")

	if got := resolveFingerprint("first"); got != "first" {
		t.Fatalf("resolve(first) = %q", got)
	}
}

func TestFingerprintLineagePersistsAndExports(t *testing.T) {
	dir := withDataDir(t)
	resetFingerprintLineage()
	t.Cleanup(resetFingerprintLineage)

	recordFingerprintLineage("newfp", "oldfp")

	if _, err := os.Stat(filepath.Join(dir, fingerprintLineageFile)); err != nil {
		t.Fatalf("lineage file: %v", err)
	}

	// A fresh process reads it back.
	resetFingerprintLineage()

	if got := resolveFingerprint("newfp"); got != "oldfp" {
		t.Fatalf("after reload resolve(newfp) = %q", got)
	}

	out, err := FingerprintLineageExport()
	if err != nil || out != `{"newfp":"oldfp"}` {
		t.Fatalf("export = %q, %v", out, err)
	}

	if err := FingerprintLineageImport(`{"other":"oldfp","newfp":"ignored","":"x"}`); err != nil {
		t.Fatal(err)
	}

	if got := resolveFingerprint("other"); got != "oldfp" {
		t.Fatalf("resolve(other) = %q", got)
	}

	if got := resolveFingerprint("newfp"); got != "oldfp" {
		t.Fatalf("an import must not override a known entry: %q", got)
	}

	if err := FingerprintLineageImport("not json"); err == nil {
		t.Fatal("bad JSON must be refused")
	}
}

func TestFingerprintLineageKeepsContextFingerprint(t *testing.T) {
	resetFingerprintLineage()
	t.Cleanup(resetFingerprintLineage)

	before := &clientconfig.Context{Endpoints: []string{"10.0.0.1"}, CA: "b2xk"}
	after := &clientconfig.Context{Endpoints: []string{"10.0.0.1"}, CA: "bmV3"}

	if contextFingerprint("c", before) == contextFingerprint("c", after) {
		t.Fatal("different CAs must give different fingerprints before any lineage")
	}

	recordFingerprintLineage(rawContextFingerprint("c", after), contextFingerprint("c", before))

	if contextFingerprint("c", before) != contextFingerprint("c", after) {
		t.Fatal("the lineage must keep the fingerprint across the CA change")
	}
}

func TestAgreeOnIssuingCA(t *testing.T) {
	a := issuingCA{node: caRotationNode{Node: "10.0.0.1", Hostname: "cp-1"}, ca: &caRotationCA{Fingerprint: "aaaa"}, pem: []byte("A")}
	b := issuingCA{node: caRotationNode{Node: "10.0.0.2", Hostname: "cp-2"}, ca: &caRotationCA{Fingerprint: "bbbb"}, pem: []byte("B")}

	if ca, pem, problem := agreeOnIssuingCA([]issuingCA{a, a}); problem != "" || ca.Fingerprint != "aaaa" || string(pem) != "A" {
		t.Fatalf("agreeing control planes: %v %q %q", ca, pem, problem)
	}

	if _, _, problem := agreeOnIssuingCA([]issuingCA{a, b}); !strings.Contains(problem, "different CAs") ||
		!strings.Contains(problem, "cp-1 (10.0.0.1): aaaa") || !strings.Contains(problem, "cp-2 (10.0.0.2): bbbb") {
		t.Fatalf("problem = %q", problem)
	}

	if _, _, problem := agreeOnIssuingCA(nil); problem != errNoControlPlane.Error() {
		t.Fatalf("no control plane: %q", problem)
	}

	unreadable := issuingCA{node: b.node, problem: "the cluster's CA cannot be read: permission denied"}
	if _, _, problem := agreeOnIssuingCA([]issuingCA{a, unreadable}); problem != unreadable.problem {
		t.Fatalf("unreadable: %q", problem)
	}
}

func TestTalosCARotationPlanHalfRotated(t *testing.T) {
	f, cfg, _ := caCluster(t)

	// cp-2 already issues with another CA, as after a rotation stopped during the swap.
	other, err := secrets.NewTalosCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}

	root := secretsres.NewOSRoot(secretsres.OSRootID)
	root.TypedSpec().IssuingCA = x509.NewCertificateAndKeyFromCertificateAuthority(other)
	f.put(caControlPlane2, root)

	out, err := TalosCARotationPlan(cfg, "fake")
	if err != nil {
		t.Fatal(err)
	}

	var plan caRotationPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	if plan.Ready || plan.CA != nil || !strings.Contains(strings.Join(plan.Blockers, " "), "different CAs") {
		t.Fatalf("plan = %+v", plan)
	}
}
