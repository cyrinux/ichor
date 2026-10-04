package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// Shaped like the Percona XtraDB Cluster operator's objects (pxc.percona.com/v1): status.state
// and per-component size/ready, spec.backup.schedule, and a backup CR per run naming its cluster.
const perconaFixture = `{"items":[
  {"metadata":{"name":"shop","namespace":"db","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"crVersion":"1.18.0","pxc":{"size":3},"haproxy":{"enabled":true,"size":2},"proxysql":{"enabled":false},
     "backup":{"schedule":[{"name":"daily","schedule":"0 2 * * *","keep":7,"storageName":"s3"},{"name":"weekly","schedule":"0 3 * * 0","keep":4,"storageName":"s3"}]}},
   "status":{"state":"ready","pxc":{"size":3,"ready":3,"status":"ready"},"haproxy":{"size":2,"ready":2,"status":"ready"}}},
  {"metadata":{"name":"crm","namespace":"db","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"crVersion":"1.18.0","pxc":{"size":3},"proxysql":{"enabled":true,"size":2}},
   "status":{"state":"initializing","pxc":{"size":3,"ready":2,"status":"initializing"},"proxysql":{"size":2,"ready":1,"status":"initializing"}}},
  {"metadata":{"name":"down","namespace":"db","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"crVersion":"1.18.0","pxc":{"size":3},"haproxy":{"enabled":true,"size":2}},
   "status":{"state":"error","message":["PXC: pxc: back-off restarting failed container"],"pxc":{"size":3,"status":"initializing"},"haproxy":{"size":2,"status":"initializing"}}},
  {"metadata":{"name":"fresh","namespace":"db","creationTimestamp":"2026-10-03T10:00:00Z"},
   "spec":{"crVersion":"1.18.0","pxc":{"size":1},"backup":{"schedule":[{"name":"daily","schedule":"0 2 * * *"}]}},
   "status":{"state":"initializing","pxc":{"size":1,"ready":1,"status":"ready"}}},
  {"metadata":{"name":"old","namespace":"db","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"crVersion":"1.17.0","pause":true,"pxc":{"size":3},"haproxy":{"enabled":true,"size":2}},
   "status":{"state":"paused","pxc":{"status":"paused"},"haproxy":{"status":"paused"}}},
  {"metadata":{"name":"wiki","namespace":"db","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"crVersion":"1.18.0","pxc":{"size":1},"backup":{"schedule":[{"name":"daily","schedule":"0 2 * * *"}]}},
   "status":{"state":"ready","pxc":{"size":1,"ready":1,"status":"ready"}}}]}`

const perconaBackupFixture = `{"items":[
  {"metadata":{"namespace":"db","creationTimestamp":"2026-10-03T02:00:00Z"},"spec":{"pxcCluster":"shop"},"status":{"state":"Succeeded","completed":"2026-10-03T02:10:00Z"}},
  {"metadata":{"namespace":"db","creationTimestamp":"2026-10-02T02:00:00Z"},"spec":{"pxcCluster":"shop"},"status":{"state":"Failed"}},
  {"metadata":{"namespace":"db","creationTimestamp":"2026-10-03T11:30:00Z"},"spec":{"pxcCluster":"shop"},"status":{"state":"Running"}},
  {"metadata":{"namespace":"db","creationTimestamp":"2026-09-20T02:00:00Z"},"spec":{"pxcCluster":"wiki"},"status":{"state":"Succeeded","completed":"2026-09-20T02:05:00Z"}},
  {"metadata":{"namespace":"db","creationTimestamp":"2026-10-03T02:00:00Z"},"spec":{"pxcCluster":"wiki"},"status":{"state":"Failed"}}]}`

func perconaPods() []dsPod {
	pod := func(cluster, component, name, node string, ready bool) dsPod {
		labels := map[string]string{
			"app.kubernetes.io/name": "percona-xtradb-cluster", "app.kubernetes.io/instance": cluster, "app.kubernetes.io/component": component,
		}
		p := fakePod("db", name, node, ready, labels, component, "percona/percona-xtradb-cluster:8.0.39")
		if !ready {
			p.Status.Phase, p.Spec.NodeName = "Pending", ""
		}

		return p
	}

	return []dsPod{
		pod("shop", "pxc", "shop-pxc-1", "node-2", true),
		pod("shop", "pxc", "shop-pxc-0", "node-1", true),
		pod("shop", "pxc", "shop-pxc-2", "node-3", true),
		pod("shop", "haproxy", "shop-haproxy-0", "node-1", true),
		pod("crm", "pxc", "crm-pxc-0", "node-1", true),
		pod("crm", "pxc", "crm-pxc-1", "node-2", true),
		pod("crm", "pxc", "crm-pxc-2", "", false),
		pod("down", "pxc", "down-pxc-0", "", false),
	}
}

func TestMapPercona(t *testing.T) {
	var clusters kubeList[perconaClusterObject]
	if err := json.Unmarshal([]byte(perconaFixture), &clusters); err != nil {
		t.Fatal(err)
	}

	var backups kubeList[perconaBackupObject]
	if err := json.Unmarshal([]byte(perconaBackupFixture), &backups); err != nil {
		t.Fatal(err)
	}

	out := mapPercona(clusters.Items, backups.Items, perconaPods(), fixtureNow)

	type summary struct {
		Name, Health, Proxy string
		Ready, Size         int
		Reasons             []string
	}

	var got []summary
	for _, c := range out.Clusters {
		got = append(got, summary{c.Name, c.Health, c.Proxy, c.PXCReady, c.PXCSize, c.Reasons})
	}

	want := []summary{
		{"down", healthCritical, "haproxy", 0, 3, []string{perconaReasonError, perconaReasonNoMember, perconaReasonProxy}},
		{"crm", healthWarning, "proxysql", 2, 3, []string{perconaReasonMembers, perconaReasonProxy}},
		{"fresh", healthWarning, "", 1, 1, []string{perconaReasonInitializing}},
		{"wiki", healthWarning, "", 1, 1, []string{perconaReasonBackupFailed}},
		{"shop", healthOK, "haproxy", 3, 3, []string{}},
		{"old", healthIdle, "haproxy", 0, 3, []string{}},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("clusters differ")
	}

	// The members only, by name, with the backup times and schedules: a failure before the
	// last success is over.
	shop := out.Clusters[4]
	if names := []string{shop.Pods[0].Name, shop.Pods[1].Name, shop.Pods[2].Name}; len(shop.Pods) != 3 || !slices.Equal(names, []string{"shop-pxc-0", "shop-pxc-1", "shop-pxc-2"}) {
		t.Errorf("pods: %+v", shop.Pods)
	}

	if shop.LastBackupAt != unixMilli("2026-10-03T02:10:00Z") || shop.LastBackupFailedAt != unixMilli("2026-10-02T02:00:00Z") || len(shop.BackupSchedules) != 2 {
		t.Errorf("backups: %+v", shop)
	}

	if down := out.Clusters[0]; down.Message != "PXC: pxc: back-off restarting failed container" || down.Pods[0].Node != "" {
		t.Errorf("down: %+v", down)
	}
}

func TestPerconaBackupStale(t *testing.T) {
	var c perconaClusterObject
	c.Metadata.CreationTimestamp = "2026-01-01T00:00:00Z"
	c.Spec.PXC = &perconaPodSpec{Size: 1}
	c.Status.PXC = perconaAppStatus{Size: 1, Ready: 1}
	c.Status.State = "ready"
	c.Spec.Backup = &struct {
		Schedule []perconaSchedule `json:"schedule"`
	}{Schedule: []perconaSchedule{{Schedule: "0 2 * * *"}, {Schedule: "0 3 * * 0"}}}

	// Daily is the shortest: late after two days.
	got := mapPerconaCluster(c, perconaBackupTimes{success: fixtureNow.Add(-50 * time.Hour).UnixMilli()}, nil, fixtureNow)
	if !slices.Equal(got.Reasons, []string{perconaReasonBackupStale}) {
		t.Errorf("50h: %+v", got.Reasons)
	}

	got = mapPerconaCluster(c, perconaBackupTimes{success: fixtureNow.Add(-30 * time.Hour).UnixMilli()}, nil, fixtureNow)
	if got.Health != healthOK {
		t.Errorf("30h: %+v", got.Reasons)
	}

	// Never backed up: stale only once the cluster is older than that.
	if got = mapPerconaCluster(c, perconaBackupTimes{}, nil, fixtureNow); !slices.Equal(got.Reasons, []string{perconaReasonBackupStale}) {
		t.Errorf("never: %+v", got.Reasons)
	}

	c.Spec.Backup = nil
	if got = mapPerconaCluster(c, perconaBackupTimes{}, nil, fixtureNow); got.Health != healthOK || got.Pods == nil {
		t.Errorf("no schedule: %+v", got)
	}
}

func TestReadDataServicesPercona(t *testing.T) {
	podJSON, err := json.Marshal(map[string]any{"items": perconaPods()})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"pxc.percona.com","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/pxc.percona.com/v1/perconaxtradbclusters":       perconaFixture,
		"GET /apis/pxc.percona.com/v1/perconaxtradbclusterbackups": perconaBackupFixture,
		"GET /api/v1/pods": string(podJSON),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints("percona-xtradb"), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.Percona == nil || res.Percona.Error != "" || len(res.Percona.Clusters) != 6 || res.Percona.Version != "v1" {
		t.Fatalf("percona: %+v", res.Percona)
	}

	if res.Longhorn != nil || res.CNPG != nil || res.Garage != nil || res.Dragonfly != nil {
		t.Errorf("only Percona is installed: %+v", res)
	}
}

func TestPerconaDemoHasEveryState(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.Percona == nil || len(demo.Percona.Clusters) < 3 {
		t.Fatal("demo misses Percona")
	}

	var states []string
	for _, c := range demo.Percona.Clusters {
		states = append(states, c.Health)
	}

	for _, h := range []string{healthWarning, healthOK, healthIdle} {
		if !strings.Contains(strings.Join(states, ","), h) {
			t.Errorf("demo states: %v", states)
		}
	}
}

func TestPerconaImagesInCatalog(t *testing.T) {
	catalog := loadAppCatalog()

	for _, image := range []string{
		"percona/percona-xtradb-cluster-operator:1.18.0",
		"docker.io/percona/percona-xtradb-cluster:8.0.42-33.1",
		"registry.example.com/mirror/percona-xtradb-cluster:8.4.5",
	} {
		if id := catalog.identify(parseImageRef(image)); id.app == nil || id.app.ID != "percona-xtradb" {
			t.Errorf("identify(%q) = %+v", image, id)
		}
	}
}
