package talosmobile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestGarageProxyHealth(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/name": "garage"}
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/storage/services": `{"items":[
			{"metadata":{"name":"garage"},"spec":{"selector":{"app.kubernetes.io/name":"garage"},"ports":[{"name":"s3-api","port":3900}]}},
			{"metadata":{"name":"other-admin"},"spec":{"selector":{"app":"other"},"ports":[{"name":"admin","port":3903}]}},
			{"metadata":{"name":"garage-metrics"},"spec":{"selector":{"app.kubernetes.io/name":"garage"},"ports":[{"name":"metrics","port":3903}]}}]}`,
		"GET /api/v1/namespaces/storage/services/garage-metrics:3903/proxy/health": "Garage is fully operational",
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	g := garageGroupFor("storage", "garage", fakePod("storage", "garage-x", "n1", true, labels, "garage", "dxflrs/garage:v2.3.0"))

	if status, msg := garageProxyHealth(context.Background(), k, g); status != garageHealthy || msg != "Garage is fully operational" {
		t.Fatalf("got %s %q", status, msg)
	}

	other := garageGroupFor("nowhere", "garage", fakePod("nowhere", "garage-y", "n1", true, labels, "garage", "dxflrs/garage:v2.3.0"))
	if status, _ := garageProxyHealth(context.Background(), k, other); status != garageUnknown {
		t.Fatalf("missing services: got %s", status)
	}
}

func TestCompactDurationAndExecErrorText(t *testing.T) {
	for secs, want := range map[int64]string{5: "5s", 120: "2m", 7200: "2h", 595830: "6d"} {
		if got := compactDuration(secs); got != want {
			t.Errorf("%d: got %s, want %s", secs, got, want)
		}
	}

	if msg := (&kubeExecError{ExitCode: 2, Message: "boom"}).Error(); msg != "command exited with code 2: boom" {
		t.Error(msg)
	}

	if msg := (&kubeExecError{ExitCode: -1, Message: "denied"}).Error(); msg != "exec failed: denied" {
		t.Error(msg)
	}
}

func TestGarageVerdictIgnoresNodesWithoutRole(t *testing.T) {
	inst := garageInstance{Status: garageHealthy, StorageNodes: 2, StorageNodesUp: 2, Partitions: 256, PartitionsQuorum: 256, PartitionsAllOk: 256,
		Nodes: []garageNode{{Hostname: "old", Up: false}, {Hostname: "a", Up: true, Storage: true}, {Hostname: "b", Up: true, Storage: true}}}

	if status, msg := garageVerdict(inst); status != garageHealthy || msg != "" {
		t.Fatalf("a role-less node away degraded the cluster: %s %q", status, msg)
	}
}

func TestReadGaragePodListingError(t *testing.T) {
	if g := readGarage(context.Background(), nil, nil, nil, errExecRefused, false); g != nil {
		t.Fatalf("unhinted listing error reported: %+v", g)
	}

	if g := readGarage(context.Background(), nil, nil, nil, errExecRefused, true); g == nil || g.Error == "" {
		t.Fatalf("hinted listing error lost: %+v", g)
	}
}

func TestGarageNodeDetailFallsBackToTags(t *testing.T) {
	n := garageNode{Zone: "zone-9", Tags: []string{"host-9"}, LastSeenSecs: -1}
	if got := garageNodeDetail(n); got != " (zone zone-9, tags host-9)" {
		t.Fatalf("got %q", got)
	}

	if got := garageNodeDetail(garageNode{LastSeenSecs: -1}); got != "" {
		t.Fatalf("empty node: %q", got)
	}
}

func TestKubeDataServicesMasked(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	enableMask(t, "orders-db")

	out, err := KubeDataServices(cfg, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "orders-db") {
		t.Fatal("masked word left in the output")
	}

	var res dataServices
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.CNPG == nil {
		t.Fatalf("masking broke the JSON: %v", err)
	}
}
