package ichorgo

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func capiCheckupFrom(t *testing.T, f *fakeKubeAPI, version string) checkupSection {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	groups := map[string]string{groupClusterAPI: version, groupCAPIControlPlane: "v1alpha3", groupCAPIBootstrap: "v1alpha3", groupSidero: "v1alpha2"}

	return checkupClusterAPI(context.Background(), k, checkupInput{now: capiNow, groups: groups})
}

func TestCheckupClusterAPI(t *testing.T) {
	s := capiCheckupFrom(t, newFakeKubeAPI(t, capiAnswers(t, "v1beta2")), "v1beta2")
	if s.ID != checkClusterAPI || s.Status != healthCritical || s.Error != "" {
		t.Fatalf("section %+v", s)
	}

	got := map[string]bool{}
	for _, k := range findingKinds(s) {
		got[k] = true
	}

	for _, want := range []string{
		"capiClusterFailed:critical:old-lab",
		"capiMachineFailed:critical:old-lab-cp",
		"sideroServerOff:critical:srv-b2",
		"capiMachineStuck:warning:staging-cp-stuck",
		"sideroServerUnaccepted:warning:srv-b1",
	} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, findingKinds(s))
		}
	}

	for _, f := range s.Findings {
		switch f.Name {
		case "staging-cp-fresh":
			t.Errorf("a machine pending for five minutes is a finding: %+v", f)
		case "staging-workers":
			t.Errorf("a deployment still scaling up is a finding: %+v", f)
		case "staging-cp-stuck":
			if f.Extra != "/Provisioning" && !strings.HasSuffix(f.Extra, "/Provisioning") || f.Since == 0 {
				t.Errorf("stuck machine %+v", f)
			}
		}
	}

	if want := 2 + 6 + 2 + 4; s.Checked != want {
		t.Errorf("checked %d, want %d", s.Checked, want)
	}
}

func TestCheckupClusterAPIAbsent(t *testing.T) {
	s := checkupClusterAPI(context.Background(), nil, checkupInput{now: capiNow, groups: map[string]string{"cilium.io": "v2"}})
	if s.Status != checkAbsent || s.Findings == nil {
		t.Fatalf("absent %+v", s)
	}
}

// A forbidden listing is the section's error; what the other lists show still counts.
func TestCheckupClusterAPIForbidden(t *testing.T) {
	f := newFakeKubeAPI(t, capiAnswers(t, "v1beta2"))
	f.answerWith("GET /apis/metal.sidero.dev/v1alpha2/servers", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"listing denied"}`)

	s := capiCheckupFrom(t, f, "v1beta2")
	if !strings.Contains(s.Error, "listing denied") || s.Status != healthCritical {
		t.Fatalf("section %+v", s)
	}

	for _, f := range s.Findings {
		if f.Kind == findSideroServerOff || f.Kind == findSideroServerUnaccepted {
			t.Errorf("a server from a refused list: %+v", f)
		}
	}
}

func TestCheckupClusterProvisioningPastTheGrace(t *testing.T) {
	now := capiNow
	clusters := []capiCluster{
		{Namespace: "fleet", Name: "new", Phase: capiPhaseProvisioning, CreatedAt: now.Add(-10 * time.Minute).UnixMilli()},
		{Namespace: "fleet", Name: "slow", Phase: capiPhaseProvisioning, CreatedAt: now.Add(-2 * time.Hour).UnixMilli(),
			Conditions: []capiCondition{{Type: "InfrastructureReady", Status: "False", Reason: "WaitingForServer", Message: "no server in class any"}}},
		{Namespace: "fleet", Name: "fine", Phase: "Provisioned", CreatedAt: now.Add(-48 * time.Hour).UnixMilli()},
	}

	got := capiClusterFindings(clusters, now)
	if len(got) != 1 || got[0].Name != "slow" || got[0].Kind != findCapiClusterProvisioning || got[0].Reason != "InfrastructureReady: WaitingForServer" ||
		got[0].Message != "no server in class any" || got[0].Since == 0 {
		t.Fatalf("findings %+v", got)
	}
}

func TestCapiFailureOfAConditionWithoutReason(t *testing.T) {
	reason, message := capiFailureOf("", "", []capiCondition{{Type: "Ready", Status: "False", Message: "waiting"}})
	if reason != "Ready" || message != "waiting" {
		t.Fatalf("%q %q", reason, message)
	}
}

func TestCheckupWorkersShort(t *testing.T) {
	got := capiWorkersFindings([]capiMachineDeployment{
		{Namespace: "fleet", Name: "short", Cluster: "prod", Phase: "Running", Desired: 4, Available: 3},
		{Namespace: "fleet", Name: "scaling", Phase: "ScalingUp", Desired: 4, Available: 2},
		{Namespace: "fleet", Name: "full", Phase: "Running", Desired: 2, Available: 2},
		{Namespace: "fleet", Name: "zero", Phase: "Running"},
	})
	if len(got) != 1 || got[0].Name != "short" || got[0].Value != 3 || got[0].Limit != 4 || got[0].Extra != "prod" {
		t.Fatalf("findings %+v", got)
	}
}

func TestCheckupUnacceptedServersAreCapped(t *testing.T) {
	servers := []sideroServer{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		servers = append(servers, sideroServer{Name: name})
	}

	got := sideroServerFindings(servers)

	names := []string{}
	for _, f := range got {
		names = append(names, f.Name)
	}

	if !reflect.DeepEqual(names, []string{"a", "b", "c", "d", "e", ""}) || got[5].Count != 2 {
		t.Fatalf("findings %+v", got)
	}
}
