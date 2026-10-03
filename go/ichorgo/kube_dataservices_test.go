package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The fixtures under testdata/ are trimmed, anonymised captures of a real cluster
// (Garage v2.3.0 with a node down, Longhorn 1.12, CloudNativePG with the barman-cloud plugin).

var fixtureNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func loadFixture[T any](t *testing.T, path string) T {
	t.Helper()

	var v T
	if err := json.Unmarshal(readFixture(t, path), &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	return v
}

func TestMapLonghornFixtures(t *testing.T) {
	out := mapLonghorn("v1beta2",
		loadFixture[lhList[lhVolumeObject]](t, "longhorn/1.12/volumes.json").Items,
		loadFixture[lhList[lhReplicaObject]](t, "longhorn/1.12/replicas.json").Items,
		loadFixture[lhList[lhNodeObject]](t, "longhorn/1.12/nodes.json").Items,
		loadFixture[lhList[lhBackupTargetObject]](t, "longhorn/1.12/backuptargets.json").Items, "")

	var healths []string
	for _, v := range out.Volumes {
		healths = append(healths, v.Health)
	}

	if !slices.Equal(healths, []string{healthCritical, healthOK, healthIdle}) {
		t.Fatalf("volume order by health: %v", healths)
	}

	faulted, healthy, idle := out.Volumes[0], out.Volumes[1], out.Volumes[2]

	if faulted.Robustness != "faulted" || !slices.Equal(faulted.ReplicaNodes, []string{"node-6"}) || faulted.ReplicasHealthy != 0 {
		t.Errorf("faulted volume: %+v", faulted)
	}

	if healthy.ReplicasHealthy != 1 || healthy.Rebuilding != 0 || healthy.Size == 0 || healthy.PVCName == "" {
		t.Errorf("healthy volume: %+v", healthy)
	}

	if idle.ReplicasHealthy != 0 || !slices.Equal(idle.ReplicaNodes, []string{"node-2", "node-5", "node-8"}) {
		t.Errorf("idle volume: %+v", idle)
	}

	notReady := 0

	for _, n := range out.Nodes {
		if !n.Ready {
			notReady++
		}

		if len(n.Disks) == 0 || n.Disks[0].Path == "" || n.Disks[0].Maximum == 0 {
			t.Errorf("node %s disks: %+v", n.Name, n.Disks)
		}
	}

	if len(out.Nodes) != 3 || notReady != 1 {
		t.Errorf("nodes: %+v", out.Nodes)
	}

	if len(out.BackupTargets) != 1 || !out.BackupTargets[0].Available || out.BackupTargets[0].Message != "" {
		t.Errorf("backup targets: %+v", out.BackupTargets)
	}
}

func TestLonghornVolumeHealth(t *testing.T) {
	cases := []struct{ state, robustness, want string }{
		{"attached", "healthy", healthOK},
		{"attached", "degraded", healthWarning},
		{"attached", "unknown", healthWarning},
		{"detached", "faulted", healthCritical},
		{"attached", "faulted", healthCritical},
		{"detached", "unknown", healthIdle},
		{"attaching", "unknown", healthOK},
	}

	for _, c := range cases {
		if got := longhornVolumeHealth(c.state, c.robustness); got != c.want {
			t.Errorf("%s/%s: got %s, want %s", c.state, c.robustness, got, c.want)
		}
	}
}

func TestLonghornReplicaTimesInStatus(t *testing.T) {
	var r lhReplicaObject
	r.Spec.VolumeName, r.Spec.NodeID = "v", "n1"
	r.Status.CurrentState, r.Status.HealthyAt = "running", "2026-10-01T00:00:00Z"

	if v := mapLonghornVolume(lhVolumeObject{}, []lhReplicaObject{r}); v.ReplicasHealthy != 1 {
		t.Fatalf("healthyAt in status not read: %+v", v)
	}

	r.Status.HealthyAt = ""
	if v := mapLonghornVolume(lhVolumeObject{}, []lhReplicaObject{r}); v.Rebuilding != 1 {
		t.Fatalf("running replica without healthyAt is rebuilding: %+v", v)
	}
}

func TestMapCNPGFixtures(t *testing.T) {
	pods := []dsPod{
		fakePod("db-4", "pg-no-ready-4", "node-6", false, map[string]string{"cnpg.io/cluster": "pg-no-ready"}, "postgres", "ghcr.io/cloudnative-pg/postgresql:18"),
		fakePod("db-4", "pg-no-ready-5", "node-6", false, map[string]string{"cnpg.io/cluster": "pg-no-ready"}, "postgres", "ghcr.io/cloudnative-pg/postgresql:18"),
	}

	out := mapCNPG(
		loadFixture[lhList[cnpgClusterObject]](t, "cnpg/clusters.json").Items,
		loadFixture[lhList[cnpgScheduledBackupObject]](t, "cnpg/scheduledbackups.json").Items,
		loadFixture[lhList[cnpgObjectStoreObject]](t, "cnpg/objectstores.json").Items,
		pods, fixtureNow)

	type summary struct {
		Name, Health, Archiving, LastBackup string
		Reasons                             []string
	}

	var got []summary
	for _, c := range out.Clusters {
		got = append(got, summary{c.Name, c.Health, c.Archiving, c.LastBackup, c.Reasons})
	}

	want := []summary{
		{"pg-no-ready", healthCritical, "ok", "ok", []string{cnpgReasonNoInstance}},
		{"pg-backup-failed", healthWarning, "ok", "failed", []string{cnpgReasonBackupFailed}},
		{"pg-short-instances", healthWarning, "ok", "failed", []string{cnpgReasonInstances, cnpgReasonBackupFailed}},
		{"pg-healthy", healthOK, "ok", "ok", []string{}},
		{"pg-wal-archiver-off", healthOK, "off", "ok", []string{}},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("clusters differ")
	}

	noReady := out.Clusters[0]
	if len(noReady.InstancePods) != 2 || noReady.InstancePods[0].Node != "node-6" || noReady.InstancePods[0].Ready {
		t.Errorf("instance pods: %+v", noReady.InstancePods)
	}

	for _, c := range out.Clusters {
		if c.BackupMethod != "plugin" || c.ObjectStore != "garage-store" || !c.Scheduled || c.LastSuccessAt == 0 || c.RecoverableAt == 0 {
			t.Errorf("%s backup fields: %+v", c.Name, c)
		}
	}
}

func TestCNPGBackupStaleAndInTree(t *testing.T) {
	var c cnpgClusterObject
	c.Metadata.Namespace, c.Metadata.Name = "db", "pg"
	c.Spec.Instances = 1
	c.Status.ReadyInstances, c.Status.CurrentPrimary, c.Status.TargetPrimary = 1, "pg-1", "pg-1"
	c.Spec.Backup.BarmanObjectStore = json.RawMessage(`{"destinationPath":"s3://b/"}`)
	c.Status.LastSuccessfulBackup = fixtureNow.Add(-12 * 24 * time.Hour).Format(time.RFC3339)
	c.Status.Conditions = []kubeCondition{{Type: "ContinuousArchiving", Status: "False"}}

	weekly := []cnpgScheduledBackupObject{{}}
	weekly[0].Metadata.Namespace, weekly[0].Spec.Cluster.Name, weekly[0].Spec.Schedule = "db", "pg", "0 0 3 * * 0"

	got := mapCNPG([]cnpgClusterObject{c}, weekly, nil, nil, fixtureNow).Clusters[0]
	if got.BackupMethod != "in-tree" || got.Archiving != "failing" || got.LastBackup != "stale" ||
		!slices.Equal(got.Reasons, []string{cnpgReasonArchiving, cnpgReasonBackupStale}) || got.Health != healthWarning {
		t.Fatalf("in-tree stale cluster: %+v", got)
	}

	// Suspended schedules expect nothing; no backup at all is then "none", not stale.
	suspended := true
	weekly[0].Spec.Suspend = &suspended
	c.Status.LastSuccessfulBackup = ""

	if got := mapCNPG([]cnpgClusterObject{c}, weekly, nil, nil, fixtureNow).Clusters[0]; got.Scheduled || got.LastBackup != "none" {
		t.Fatalf("suspended schedule: %+v", got)
	}
}

func TestCNPGSwitchoverAndFailover(t *testing.T) {
	base := cnpgCluster{Instances: 3, ReadyInstances: 3, CurrentPrimary: "pg-1", TargetPrimary: "pg-2", Archiving: "ok", LastBackup: "ok"}

	if h, r := cnpgHealth(base, "True"); h != healthWarning || !slices.Equal(r, []string{cnpgReasonSwitchover}) {
		t.Errorf("switchover: %s %v", h, r)
	}

	failover := base
	failover.Phase, failover.ReadyInstances = "Failing over", 2

	if h, r := cnpgHealth(failover, "False"); h != healthCritical || r[0] != cnpgReasonFailover {
		t.Errorf("failover: %s %v", h, r)
	}

	notReady := base
	notReady.TargetPrimary = "pg-1"

	if h, r := cnpgHealth(notReady, "False"); h != healthWarning || !slices.Equal(r, []string{cnpgReasonNotReady}) {
		t.Errorf("not ready: %s %v", h, r)
	}
}

func TestCronInterval(t *testing.T) {
	const day = 24 * time.Hour

	cases := map[string]time.Duration{
		"0 0 3 * * 0":  7 * day,
		"0 45 4 * * 2": 7 * day,
		"0 0 0 * * *":  day,
		"30 2 * * *":   day,
		"0 0 1 1 * *":  31 * day,
		"@weekly":      7 * day,
		"@daily":       day,
		"nonsense":     8 * day,
		"":             8 * day,
	}

	for schedule, want := range cases {
		if got := cronInterval(schedule); got != want {
			t.Errorf("%q: got %v, want %v", schedule, got, want)
		}
	}
}

// fakePod is a running pod with one container.
func fakePod(namespace, name, node string, ready bool, labels map[string]string, container, image string) dsPod {
	var p dsPod
	p.Metadata.Namespace, p.Metadata.Name, p.Metadata.Labels = namespace, name, labels
	p.Spec.NodeName = node
	p.Spec.Containers = append(p.Spec.Containers, struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}{container, image})
	p.Status.Phase = "Running"
	p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, struct {
		Name  string `json:"name"`
		Ready bool   `json:"ready"`
	}{container, ready})

	return p
}

// fixtureRunner answers the Garage commands from a fixture directory.
func fixtureRunner(t *testing.T, dir string) execFunc {
	t.Helper()

	return func(_ context.Context, _, _, _ string, argv []string) ([]byte, []byte, error) {
		switch {
		case slices.Equal(argv, garageCommands.health):
			return readFixture(t, dir+"/GetClusterHealth.json"), nil, nil
		case slices.Equal(argv, garageCommands.status):
			return readFixture(t, dir+"/GetClusterStatus.json"), nil, nil
		case slices.Equal(argv, garageCommands.stats):
			return readFixture(t, dir+"/GetNodeStatistics.json"), nil, nil
		default:
			t.Errorf("command outside the allow-list: %v", argv)

			return nil, nil, errors.New("not allowed")
		}
	}
}

func garageGroupFor(namespace, name string, pods ...dsPod) garageGroup {
	return garageGroup{namespace: namespace, name: name, container: "garage", pods: pods}
}

func noFallback(t *testing.T) healthFallback {
	return func(context.Context) (string, string) {
		t.Error("fallback used although the CLI answered")

		return garageUnknown, ""
	}
}

func TestReadGarageDegradedFixture(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/name": "garage"}
	var pods []dsPod

	for i, host := range []string{"garage-a", "garage-b", "garage-c", "garage-e", "garage-f", "garage-g"} {
		pods = append(pods, fakePod("garage", host, "node-"+string(rune('1'+i)), true, labels, "garage", "dxflrs/amd64_garage:v2.3.0"))
	}

	pods = append(pods, fakePod("garage", "garage-z", "node-9", false, labels, "garage", "dxflrs/amd64_garage:v2.3.0"))

	inst := readGarageInstance(context.Background(), fixtureRunner(t, "garage/v2.3.0/degraded"), garageGroupFor("garage", "garage", pods...), noFallback(t))

	if inst.Status != garageDegraded || inst.Source != garageSourceCLI || inst.Pod != "garage-a" || inst.Pods != 7 || inst.PodsReady != 6 {
		t.Fatalf("instance: %+v", inst)
	}

	if inst.StorageNodes != 7 || inst.StorageNodesUp != 6 || inst.PartitionsAllOk != 128 || inst.PartitionsQuorum != 256 || inst.Version != "v2.3.0" {
		t.Errorf("health counters: %+v", inst)
	}

	if inst.ResyncErrors != 85101 || inst.ResyncQueue != 85262 || inst.TableSyncQueue != 249502 {
		t.Errorf("resync totals: queue %d errors %d tables %d", inst.ResyncQueue, inst.ResyncErrors, inst.TableSyncQueue)
	}

	down := inst.Nodes[0]
	if down.Up || down.Hostname != "garage-d" || down.LastSeenSecs != 595830 || down.StatsError == "" || down.ResyncErrors != -1 {
		t.Errorf("down node first: %+v", down)
	}

	if inst.Nodes[1].KubeNode == "" || !inst.Nodes[1].Up {
		t.Errorf("pod hostname not mapped to its node: %+v", inst.Nodes[1])
	}

	wantMsg := "1 node down (zone zone-1, tags node-4, last seen 6d ago); 128/256 partitions not fully replicated; 85101 blocks failing to resync"
	if inst.Message != wantMsg {
		t.Errorf("message:\n got %q\nwant %q", inst.Message, wantMsg)
	}
}

func TestReadGarageSingleNodeFixture(t *testing.T) {
	// The anonymised fixture names its only node garage-a: the pod has that hostname.
	pod := fakePod("garage-nas", "garage-a", "node-2", true, map[string]string{"app.kubernetes.io/name": "garage-nas"}, "garage", "dxflrs/amd64_garage:v2.3.0")

	inst := readGarageInstance(context.Background(), fixtureRunner(t, "garage/v2.3.0/single-node"), garageGroupFor("garage-nas", "garage-nas", pod), noFallback(t))

	if inst.Status != garageHealthy || inst.Message != "" || inst.StorageNodes != 1 || inst.ResyncErrors != 0 || inst.TableSyncQueue != 1108 {
		t.Fatalf("single node: %+v", inst)
	}

	if len(inst.Nodes) != 1 || inst.Nodes[0].KubeNode != "node-2" || inst.Nodes[0].LastSeenSecs != -1 {
		t.Errorf("node: %+v", inst.Nodes)
	}
}

func TestReadGarageFallsBackToHealth(t *testing.T) {
	pod := fakePod("garage", "garage-0", "node-1", true, nil, "garage", "dxflrs/garage:v2.3.0")
	refused := func(context.Context, string, string, string, []string) ([]byte, []byte, error) {
		return nil, nil, errExecRefused
	}

	inst := readGarageInstance(context.Background(), refused, garageGroupFor("garage", "garage", pod), func(context.Context) (string, string) {
		return garageHealthy, "Garage is fully operational"
	})

	if inst.Status != garageHealthy || inst.Source != garageSourceHealth || !strings.Contains(inst.Message, "exec refused") || inst.ResyncErrors != -1 {
		t.Fatalf("fallback: %+v", inst)
	}
}

func TestReadGarageNoReadyPod(t *testing.T) {
	pod := fakePod("garage", "garage-0", "node-1", false, nil, "garage", "dxflrs/garage:v2.3.0")
	never := func(context.Context, string, string, string, []string) ([]byte, []byte, error) {
		t.Error("exec on a pod that is not ready")

		return nil, nil, nil
	}

	if inst := readGarageInstance(context.Background(), never, garageGroupFor("garage", "garage", pod), noFallback(t)); inst.Status != garageUnavailable {
		t.Fatalf("no ready pod: %+v", inst)
	}
}

func TestGarageCommandError(t *testing.T) {
	stderr := readFixture(t, "garage/v2.3.0/stderr-sample.txt")

	err := garageCommandError(&kubeExecError{ExitCode: 1, Message: "exit 1"}, stderr)
	if err == nil || err.Error() != "JSON error: missing field `body`" {
		t.Fatalf("got %v", err)
	}

	if err := garageCommandError(errExecRefused, stderr); !errors.Is(err, errExecRefused) {
		t.Fatalf("transport errors pass through: %v", err)
	}

	if err := garageCommandError(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGarageHealthFromProxy(t *testing.T) {
	cases := []struct {
		status      int
		ctype, body string
		want        string
	}{
		{200, "text/plain", "Garage is fully operational", garageHealthy},
		{200, "text/plain", "Garage is operational but some storage nodes are unavailable", garageDegraded},
		{503, "text/plain", "Quorum is not available for some partitions", garageUnavailable},
		{503, "application/json", `{"kind":"Status","message":"no endpoints available for service"}`, garageUnavailable},
		{500, "text/plain", "boom", garageUnknown},
	}

	for _, c := range cases {
		if got, _ := garageHealthFromProxy(c.status, c.ctype, []byte(c.body)); got != c.want {
			t.Errorf("%d %q: got %s, want %s", c.status, c.body, got, c.want)
		}
	}
}

func TestFindGarageGroups(t *testing.T) {
	ds := map[string]string{"app.kubernetes.io/name": "garage"}
	pods := []dsPod{
		fakePod("storage", "garage-x1", "n1", true, ds, "garage", "dxflrs/amd64_garage:v2.3.0"),
		fakePod("storage", "garage-x2", "n2", true, ds, "garage", "dxflrs/amd64_garage:v2.3.0"),
		fakePod("nas", "garage-nas-0", "n3", true, map[string]string{"app.kubernetes.io/name": "garage-nas"}, "garage", "dxflrs/garage:v2.3.0"),
		fakePod("web", "nginx-1", "n1", true, nil, "nginx", "nginx:1.27"),
	}

	groups := findGarageGroups(pods)
	if len(groups) != 2 || groups[0].name != "garage" || len(groups[0].pods) != 2 || groups[1].name != "garage-nas" || groups[1].container != "garage" {
		t.Fatalf("groups: %+v", groups)
	}
}

func TestGarageVerdictQuorumLoss(t *testing.T) {
	inst := garageInstance{Status: garageDegraded, StorageNodes: 3, StorageNodesUp: 1, Partitions: 256, PartitionsQuorum: 0, PartitionsAllOk: 0}

	status, msg := garageVerdict(inst)
	if status != garageUnavailable || !strings.HasPrefix(msg, "1/3 storage nodes up; 256/256 partitions without write quorum") {
		t.Fatalf("got %s %q", status, msg)
	}
}

func TestReadDataServicesEndToEnd(t *testing.T) {
	answers := map[string]string{
		"GET /apis":                                        string(readFixture(t, "apis.json")),
		"GET /apis/longhorn.io/v1beta2/volumes":            string(readFixture(t, "longhorn/1.12/volumes.json")),
		"GET /apis/longhorn.io/v1beta2/replicas":           string(readFixture(t, "longhorn/1.12/replicas.json")),
		"GET /apis/longhorn.io/v1beta2/nodes":              string(readFixture(t, "longhorn/1.12/nodes.json")),
		"GET /apis/longhorn.io/v1beta2/backuptargets":      string(readFixture(t, "longhorn/1.12/backuptargets.json")),
		"GET /apis/postgresql.cnpg.io/v1/clusters":         string(readFixture(t, "cnpg/clusters.json")),
		"GET /apis/postgresql.cnpg.io/v1/scheduledbackups": string(readFixture(t, "cnpg/scheduledbackups.json")),
		"GET /apis/barmancloud.cnpg.io/v1/objectstores":    string(readFixture(t, "cnpg/objectstores.json")),
		"GET /api/v1/pods":                                 podListJSON(t, fakePod("garage", "garage-a", "node-1", true, map[string]string{"app.kubernetes.io/name": "garage"}, "garage", "dxflrs/amd64_garage:v2.3.0")),
	}

	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, fixtureRunner(t, "garage/v2.3.0/single-node"), parseHints(""), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.Longhorn == nil || res.Longhorn.Error != "" || len(res.Longhorn.Volumes) != 3 || res.Longhorn.Version != "v1beta2" {
		t.Errorf("longhorn: %+v", res.Longhorn)
	}

	if res.CNPG == nil || res.CNPG.Error != "" || len(res.CNPG.Clusters) != 5 {
		t.Errorf("cnpg: %+v", res.CNPG)
	}

	if res.Garage == nil || len(res.Garage.Instances) != 1 || res.Garage.Instances[0].Status != garageHealthy {
		t.Errorf("garage: %+v", res.Garage)
	}

	// Hints without garage skip the pod listing for Garage; CNPG then lists only its pods.
	res, err = readDataServices(context.Background(), k, fixtureRunner(t, "garage/v2.3.0/single-node"), parseHints("longhorn,cloudnative-pg"), fixtureNow)
	if err != nil || res.Garage != nil || res.CNPG == nil {
		t.Fatalf("hinted: %+v %v", res, err)
	}
}

func TestReadDataServicesNothingInstalled(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":        `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}}]}`,
		"GET /api/v1/pods": `{"items":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints(""), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if out, _ := toJSON(res); out != "{}" {
		t.Fatalf("expected no section, got %s", out)
	}
}

func TestReadDataServicesSectionErrors(t *testing.T) {
	// Longhorn is served but its volumes cannot be read: the error stays in its section.
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":        `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /api/v1/pods": `{"items":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints(""), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.Longhorn == nil || !strings.Contains(res.Longhorn.Error, "not found") || res.Garage != nil {
		t.Fatalf("section error: %+v", res.Longhorn)
	}
}

func podListJSON(t *testing.T, pods ...dsPod) string {
	t.Helper()

	data, err := json.Marshal(map[string]any{"items": pods})
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestKubeDataServicesDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeDataServices(cfg, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var res dataServices
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Longhorn == nil || res.Garage == nil || res.CNPG == nil {
		t.Fatalf("demo misses a section: %s", out)
	}

	// The demo shows every state the app draws.
	seen := map[string]bool{}
	for _, v := range res.Longhorn.Volumes {
		seen["lh:"+v.Health] = true
	}

	for _, c := range res.CNPG.Clusters {
		seen["pg:"+c.Health] = true
		seen["backup:"+c.LastBackup] = true
		seen["archiving:"+c.Archiving] = true
	}

	for _, g := range res.Garage.Instances {
		seen["garage:"+g.Status] = true
	}

	for _, want := range []string{"lh:critical", "lh:warning", "lh:ok", "lh:idle", "pg:critical", "pg:warning", "pg:ok",
		"backup:failed", "backup:stale", "archiving:off", "garage:degraded", "garage:healthy"} {
		if !seen[want] {
			t.Errorf("demo lacks %s", want)
		}
	}
}
