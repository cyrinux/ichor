package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCollectDataServicesNeedsAdmin(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	target := kubeTarget{cfg, "", ""}

	if s := collectDataServices(context.Background(), target, []string{"os:reader"}); s != nil {
		t.Fatalf("reader got %+v", s)
	}
}

func TestCollectDataServicesDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	s := collectDataServices(context.Background(), kubeTarget{cfg, "", ""}, []string{adminRole})
	if s == nil || s.Note != "" || s.Longhorn == nil || s.CNPG == nil || s.Garage == nil {
		t.Fatalf("demo admin got %+v", s)
	}
}

func TestCollectDataServicesUnreachableLeavesANote(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	target := kubeTarget{f.kubeconfigFor("https://127.0.0.1:1"), "admin@test", ""}

	s := collectDataServices(context.Background(), target, []string{adminRole})
	if s == nil || s.Note == "" || s.Longhorn != nil {
		t.Fatalf("got %+v", s)
	}
}

func sampleDataServices() *dataServicesState {
	return &dataServicesState{dataServices: dataServices{
		Longhorn: &longhornStatus{
			Volumes: []longhornVolume{
				{Name: "pvc-aaaa", PVCNamespace: "db", PVCName: "pg-data", State: "detached", Robustness: "faulted", Health: healthCritical,
					ReplicasDesired: 2, ReplicaNodes: []string{"worker-1", "worker-3"}, ScheduleError: "no node with enough free space"},
				{Name: "pvc-bbbb", PVCNamespace: "web", PVCName: "uploads", State: "attached", Robustness: "healthy", Health: healthOK,
					ReplicasDesired: 2, ReplicasHealthy: 2, ReplicaNodes: []string{"worker-1", "worker-2"}, Node: "worker-1"},
				{Name: "pvc-cccc", State: "detached", Health: healthIdle},
			},
			Nodes:         []longhornNode{{Name: "worker-3", Ready: false}, {Name: "worker-1", Ready: true}},
			BackupTargets: []longhornBackupTarget{{Name: "default", URL: "s3://user:secret@backups.example/lh", Available: false, Message: "cannot reach the bucket"}},
		},
		Garage: &garageStatus{Instances: []garageInstance{{
			Namespace: "storage", Name: "garage", Status: "degraded", Message: "1 storage node down",
			StorageNodes: 3, StorageNodesUp: 2, Partitions: 256, PartitionsAllOk: 128,
			Nodes: []garageNode{{Hostname: "garage-0", KubeNode: "worker-2", Up: true}, {Hostname: "garage-1", KubeNode: "worker-3"}},
		}}},
		CNPG: &cnpgStatus{Clusters: []cnpgCluster{
			{Namespace: "db", Name: "pg", Phase: "Failing over", Health: healthCritical, Reasons: []string{"primary down"}, Instances: 2, ReadyInstances: 1,
				InstancePods: []cnpgPod{{Name: "pg-1", Node: "worker-3"}, {Name: "pg-2", Node: "worker-1", Ready: true}}},
			{Namespace: "db", Name: "fine", Health: healthOK, Instances: 1, ReadyInstances: 1},
		}},
		Velero: &veleroStatus{
			Schedules: []veleroSchedule{{Namespace: "velero", Name: "nightly", Health: healthWarning, Reasons: []string{"stale"}}},
			Locations: []veleroLocation{{Namespace: "velero", Name: "default", Phase: "Unavailable", Message: "bucket not found", Health: healthCritical}},
		},
		CertManager: &certManagerStatus{Error: "forbidden: certificates.cert-manager.io"},
	}}
}

func TestRenderDataServices(t *testing.T) {
	var b strings.Builder
	renderDataServices(&b, sampleDataServices())
	out := b.String()

	for _, want := range []string{
		"DATA SERVICES\n",
		"  Longhorn: 3 volumes, 1 critical, 1 idle\n",
		"    pvc-aaaa (pvc db/pg-data) [critical]: detached, faulted, 0/2 replicas healthy; no node with enough free space; nodes worker-1, worker-3\n",
		"    node worker-3 [critical]: not ready\n",
		"    backup target default [warning]: cannot reach the bucket\n",
		"  Garage: 1 instances, 1 warning\n",
		"    storage/garage [warning]: degraded, 2/3 storage nodes up, 128/256 partitions with all nodes; 1 storage node down; nodes worker-3\n",
		"  CloudNativePG: 2 clusters, 1 critical\n",
		"    db/pg [critical]: Failing over; primary down; 1/2 instances ready; nodes worker-3 (pg-1 not ready), worker-1\n",
		"  Velero: 1 schedules, 1 warning\n",
		"    location velero/default [critical]: Unavailable; bucket not found\n",
		"    velero/nightly [warning]: stale\n",
		"  cert-manager could not be read: forbidden: certificates.cert-manager.io\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}

	// Fine and idle items are counted, not listed; URLs stay out of the report.
	for _, unwanted := range []string{"pvc-bbbb", "pvc-cccc", "db/fine", "secret", "s3://"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("report shows %q:\n%s", unwanted, out)
		}
	}

	b.Reset()
	renderDataServices(&b, &dataServicesState{Note: "Kubernetes API: permission denied"})

	if b.String() != "DATA SERVICES\n  could not be read: Kubernetes API: permission denied\n\n" {
		t.Errorf("note: %q", b.String())
	}

	b.Reset()
	renderDataServices(&b, nil)

	if b.Len() != 0 {
		t.Errorf("nil wrote %q", b.String())
	}
}

func TestRenderDataServicesDemo(t *testing.T) {
	var b strings.Builder
	renderDataServices(&b, &dataServicesState{dataServices: demoDataServices(time.Now())})

	if out := b.String(); !strings.Contains(out, "Longhorn: ") || !strings.Contains(out, "[critical]") || len(out) > 16<<10 {
		t.Errorf("demo section:\n%s", out)
	}
}

func TestRenderDataServicesCapsTheList(t *testing.T) {
	volumes := make([]longhornVolume, 16)
	for i := range volumes {
		volumes[i] = longhornVolume{Name: fmt.Sprintf("pvc-%02d", i), State: "detached", Robustness: "faulted", Health: healthCritical}
	}

	var b strings.Builder
	renderDataServices(&b, &dataServicesState{dataServices: dataServices{Longhorn: &longhornStatus{Volumes: volumes}}})
	out := b.String()

	if got := strings.Count(out, "[critical]"); got != dataServicesMaxItems {
		t.Errorf("%d volumes listed:\n%s", got, out)
	}

	if !strings.Contains(out, "    … and 1 more\n") {
		t.Errorf("no remainder line:\n%s", out)
	}
}

func TestRenderDataServicesKeepsTheNodesOfALongLine(t *testing.T) {
	ceph := &cephStatus{Clusters: []cephCluster{{
		Namespace: "rook-ceph", Name: "rook-ceph", CephHealth: "HEALTH_WARN", Health: healthWarning,
		Checks: []cephCheck{{Name: "PG_DEGRADED", Message: strings.Repeat("degraded data redundancy ", 20)}}, NotReadyNodes: []string{"worker-2"},
	}}}

	var b strings.Builder
	renderDataServices(&b, &dataServicesState{dataServices: dataServices{Ceph: ceph}})

	for _, line := range strings.Split(b.String(), "\n") {
		if strings.Contains(line, "rook-ceph/rook-ceph") && (!strings.HasSuffix(line, "…; nodes worker-2") || len(line) > diagnosisMaxLineLen+8) {
			t.Errorf("line %q", line)
		}
	}
}

func TestRenderDataServicesStatesOutsideTheLists(t *testing.T) {
	ds := dataServices{
		SealedSecrets: &sealedSecretsStatus{Health: healthCritical, Reasons: []string{"noKeys"}},
		Garage:        &garageStatus{Keys: []garageOpKey{{Namespace: "storage", Name: "app-key", Health: healthCritical, Reasons: []string{"notReady"}}}},
		Velero: &veleroStatus{Adhoc: []veleroAdhoc{{Namespace: "velero", Health: healthWarning,
			veleroBackup: veleroBackup{Name: "before-upgrade", Phase: "Failed", FailureReason: "timed out"}}}},
		ExternalDNS: &externalDNSStatus{Records: []externalDNSRecord{{DNSName: "app.example.org", RecordType: "A", Health: healthWarning,
			Reasons: []string{"noController"}, SourceKind: "Ingress", SourceNamespace: "web", SourceName: "app"}}},
		CastAI: &castAIStatus{Recommendations: []castAIRecommendation{{Namespace: "web", Name: "api", Health: healthCritical, Message: "apply failed"}}},
	}

	var b strings.Builder
	renderDataServices(&b, &dataServicesState{dataServices: ds})
	out := b.String()

	for _, want := range []string{
		"    controller [critical]: noKeys\n",
		"    GarageKey storage/app-key [critical]: notReady\n",
		"    backup velero/before-upgrade [warning]: Failed; timed out\n",
		"    A app.example.org [warning]: noController; Ingress web/app\n",
		"  CAST AI: 1 recommendations, 1 critical\n    web/api [critical]: apply failed\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestStripURLPassword(t *testing.T) {
	for in, want := range map[string]string{
		"s3://user:secret@backups.example/lh": "s3://backups.example/lh",
		"s3://key:pa/ss@host":                 "s3://host",
		"cifs://u:p%zz@h/share":               "cifs://h/share",
		"s3://bucket@region/":                 "s3://bucket@region/",
		"nfs://nas.example:/export":           "nfs://nas.example:/export",
	} {
		if got := stripURLPassword(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

func TestScrubDataServices(t *testing.T) {
	in := sampleDataServices()
	out := scrubDataServices(*in)

	if got := out.Longhorn.BackupTargets[0].URL; got != "s3://backups.example/lh" {
		t.Errorf("scrubbed URL = %q", got)
	}

	if in.Longhorn.BackupTargets[0].URL != "s3://user:secret@backups.example/lh" {
		t.Error("the input was modified")
	}
}

func TestDataServicesFile(t *testing.T) {
	answers := map[string]string{
		"GET /apis":                                   `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /apis/longhorn.io/v1beta2/volumes":       string(readFixture(t, "longhorn/1.12/volumes.json")),
		"GET /apis/longhorn.io/v1beta2/replicas":      string(readFixture(t, "longhorn/1.12/replicas.json")),
		"GET /apis/longhorn.io/v1beta2/nodes":         string(readFixture(t, "longhorn/1.12/nodes.json")),
		"GET /apis/longhorn.io/v1beta2/backuptargets": string(readFixture(t, "longhorn/1.12/backuptargets.json")),
		"GET /api/v1/pods":                            `{"items":[]}`,
	}

	f := newFakeKubeAPI(t, answers)
	target := kubeTarget{f.kubeconfigFor(f.URL), "admin@test", ""}

	files, err := dataServicesFile(context.Background(), target, []string{adminRole})
	if err != nil || len(files) != 1 || files[0].name != "dataservices.json" {
		t.Fatalf("files %+v, err %v", files, err)
	}

	var got dataServicesState
	if err := json.Unmarshal(files[0].content, &got); err != nil || got.Longhorn == nil || len(got.Longhorn.Volumes) != 3 {
		t.Fatalf("content %s, err %v", files[0].content, err)
	}

	// No data service, or no os:admin: no file and no failure.
	none := newFakeKubeAPI(t, map[string]string{
		"GET /apis":        `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}}]}`,
		"GET /api/v1/pods": `{"items":[]}`,
	})

	if files, err := dataServicesFile(context.Background(), kubeTarget{none.kubeconfigFor(none.URL), "admin@test", ""}, []string{adminRole}); files != nil || err != nil {
		t.Errorf("nothing installed: %+v, %v", files, err)
	}

	if files, err := dataServicesFile(context.Background(), target, []string{"os:reader"}); files != nil || err != nil {
		t.Errorf("reader: %+v, %v", files, err)
	}

	// Unreachable: the step fails with the note.
	down := kubeTarget{f.kubeconfigFor("https://127.0.0.1:1"), "admin@test", ""}
	if _, err := dataServicesFile(context.Background(), down, []string{adminRole}); err == nil {
		t.Error("unreachable API: no error")
	}
}
