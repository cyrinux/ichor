package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Shaped like Rook's objects on a real cluster: the CephCluster status carries Ceph's own
// health, its checks and the raw capacity; pools, filesystems and object stores only a phase.
const (
	cephClusterFixture = `{"items":[
  {"metadata":{"name":"ceph","namespace":"storage"},"spec":{"external":{}},
   "status":{"phase":"Ready","state":"Created","message":"Cluster created successfully",
    "ceph":{"health":"HEALTH_ERR","lastChecked":"2026-10-03T11:59:00Z",
     "details":{"PG_DEGRADED":{"message":"Degraded data redundancy: 12 pgs degraded","severity":"HEALTH_WARN"},
                "OSD_FULL":{"message":"1 full osd(s)","severity":"HEALTH_ERR"},
                "MON_DOWN":{"message":"1/3 mons down, quorum a,b","severity":"HEALTH_WARN"}},
     "capacity":{"bytesTotal":1000,"bytesUsed":900,"bytesAvailable":100,"lastUpdated":"2026-10-03T11:59:00Z"}},
    "version":{"image":"quay.io/ceph/ceph:v19.2.3","version":"19.2.3-0"}}},
  {"metadata":{"name":"ok","namespace":"ceph-ok"},
   "status":{"phase":"Ready","ceph":{"health":"HEALTH_OK","capacity":{"bytesTotal":1000,"bytesUsed":100}},"version":{"version":"19.2.3-0"}}},
  {"metadata":{"name":"down","namespace":"ceph-down"},
   "status":{"phase":"Ready","ceph":{"health":"HEALTH_OK","capacity":{"bytesTotal":1000,"bytesUsed":100}}}},
  {"metadata":{"name":"remote","namespace":"ceph-ext"},"spec":{"external":{"enable":true}},
   "status":{"phase":"Connected","ceph":{"health":"HEALTH_OK","capacity":{"bytesTotal":1000,"bytesUsed":960}}}},
  {"metadata":{"name":"new","namespace":"ceph-new"},"status":{"phase":"Progressing","message":"Configuring Ceph Mons"}}]}`

	cephBlockPoolFixture  = `{"items":[{"metadata":{"name":"replicapool","namespace":"storage"},"status":{"phase":"Ready"}},{"metadata":{"name":"broken","namespace":"storage"},"status":{"phase":"Failure"}}]}`
	cephFilesystemFixture = `{"items":[{"metadata":{"name":"fs","namespace":"storage"},"status":{"phase":"Progressing"}}]}`
	cephObjStoreFixture   = `{"items":[{"metadata":{"name":"s3","namespace":"storage"},"status":{"phase":"Ready"}}]}`
)

func cephPods() []dsPod {
	osd := func(ns, id, node string, ready bool) dsPod {
		labels := map[string]string{"app": "rook-ceph-osd", "ceph-osd-id": id, "rook_cluster": ns}

		return fakePod(ns, "rook-ceph-osd-"+id+"-abc", node, ready, labels, "osd", "quay.io/ceph/ceph:v19.2.3")
	}
	mon := func(ns, id, node string, ready bool) dsPod {
		labels := map[string]string{"app": "rook-ceph-mon", "ceph_daemon_id": id, "rook_cluster": ns}

		return fakePod(ns, "rook-ceph-mon-"+id+"-abc", node, ready, labels, "mon", "quay.io/ceph/ceph:v19.2.3")
	}
	canary := func(ns, id, node string) dsPod {
		p := mon(ns, id, node, false)
		p.Metadata.Name = "rook-ceph-mon-" + id + "-canary-abc"
		p.Metadata.Labels["mon_canary"] = "true"

		return p
	}

	return []dsPod{
		osd("storage", "10", "node-1", true),
		osd("storage", "2", "node-2", true),
		osd("storage", "1", "node-3", false),
		mon("storage", "a", "node-1", true),
		mon("storage", "b", "node-2", true),
		mon("storage", "c", "node-3", false),
		osd("ceph-ok", "0", "node-1", true),
		mon("ceph-ok", "a", "node-1", true),
		canary("ceph-ok", "b", "node-2"), // counted, it would cost ceph-ok its quorum
		osd("ceph-down", "0", "node-4", false),
		osd("ceph-down", "1", "node-4", false),
		mon("ceph-down", "a", "node-4", true),
	}
}

func cephFixtureStatus(t *testing.T) *cephStatus {
	t.Helper()

	var (
		clusters                     kubeList[cephClusterObject]
		pools, filesystems, objStore kubeList[cephPoolObject]
	)

	for fixture, into := range map[string]any{
		cephClusterFixture: &clusters, cephBlockPoolFixture: &pools, cephFilesystemFixture: &filesystems, cephObjStoreFixture: &objStore,
	} {
		if err := json.Unmarshal([]byte(fixture), into); err != nil {
			t.Fatal(err)
		}
	}

	return mapCeph(clusters.Items, map[string][]cephPoolObject{
		"blockPool": pools.Items, "filesystem": filesystems.Items, "objectStore": objStore.Items,
	}, cephPods())
}

func TestMapCeph(t *testing.T) {
	out := cephFixtureStatus(t)

	type summary struct {
		Name, Health           string
		OSDsUp, OSDs, Mons     int
		Reasons, NotReadyNodes []string
	}

	var got []summary
	for _, c := range out.Clusters {
		got = append(got, summary{c.Name, c.Health, c.OSDsUp, c.OSDsTotal, c.MonsReady, c.Reasons, c.NotReadyNodes})
	}

	want := []summary{
		{"down", healthCritical, 0, 2, 1, []string{cephReasonNoOSD}, []string{"node-4"}},
		{"remote", healthCritical, 0, 0, 0, []string{cephReasonFull}, []string{}},
		{"ceph", healthCritical, 2, 3, 2, []string{cephReasonHealthErr, cephReasonNearFull, cephReasonOSDs, cephReasonMons}, []string{"node-3"}},
		{"new", healthWarning, 0, 0, 0, []string{cephReasonNotReady}, []string{}},
		{"ok", healthOK, 1, 1, 1, []string{}, []string{}},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("clusters differ")
	}

	ceph := out.Clusters[2]
	if ceph.CephHealth != "HEALTH_ERR" || ceph.BytesTotal != 1000 || ceph.BytesUsed != 900 || ceph.Version != "19.2.3-0" || ceph.MonsTotal != 3 {
		t.Errorf("cluster: %+v", ceph)
	}

	// Errors first, then by name.
	var checks []string
	for _, c := range ceph.Checks {
		checks = append(checks, c.Name)
	}

	if !slices.Equal(checks, []string{"OSD_FULL", "MON_DOWN", "PG_DEGRADED"}) || ceph.Checks[1].Message != "1/3 mons down, quorum a,b" {
		t.Errorf("checks: %+v", ceph.Checks)
	}

	if !out.Clusters[1].External {
		t.Error("remote is external")
	}

	// OSDs by namespace, then by number (not as strings).
	var osds []string
	for _, o := range out.OSDs {
		osds = append(osds, o.Namespace+"/"+o.ID+"@"+o.Node)
	}

	if !slices.Equal(osds, []string{"ceph-down/0@node-4", "ceph-down/1@node-4", "ceph-ok/0@node-1", "storage/1@node-3", "storage/2@node-2", "storage/10@node-1"}) {
		t.Errorf("osds: %v", osds)
	}

	var pools []string
	for _, p := range out.Pools {
		pools = append(pools, p.Kind+":"+p.Name+":"+p.Health)
	}

	if !slices.Equal(pools, []string{"blockPool:broken:critical", "filesystem:fs:warning", "blockPool:replicapool:ok", "objectStore:s3:ok"}) {
		t.Errorf("pools: %v", pools)
	}
}

func TestCephClusterHealth(t *testing.T) {
	tests := []struct {
		name    string
		c       cephCluster
		health  string
		reasons []string
	}{
		{"error", cephCluster{Phase: "Ready", CephHealth: "HEALTH_ERR"}, healthCritical, []string{cephReasonHealthErr}},
		{"failure", cephCluster{Phase: "Failure"}, healthCritical, []string{cephReasonFailure}},
		{"full", cephCluster{Phase: "Ready", BytesTotal: 100, BytesUsed: 96}, healthCritical, []string{cephReasonFull}},
		{"exactly 85%", cephCluster{Phase: "Ready", BytesTotal: 100, BytesUsed: 85}, healthOK, []string{}},
		{"no quorum", cephCluster{Phase: "Ready", MonsReady: 1, MonsTotal: 3}, healthCritical, []string{cephReasonNoQuorum}},
		{"one mon of two", cephCluster{Phase: "Ready", MonsReady: 1, MonsTotal: 2}, healthCritical, []string{cephReasonNoQuorum}},
		{"one mon down", cephCluster{Phase: "Ready", MonsReady: 4, MonsTotal: 5}, healthWarning, []string{cephReasonMons}},
		// Progressing with something else wrong says only the rest.
		{"progressing", cephCluster{Phase: "Progressing", CephHealth: "HEALTH_WARN"}, healthWarning, []string{cephReasonHealthWarn}},
	}

	for _, tt := range tests {
		health, reasons := cephClusterHealth(tt.c)
		if health != tt.health || !slices.Equal(reasons, tt.reasons) {
			t.Errorf("%s: %s %v, want %s %v", tt.name, health, reasons, tt.health, tt.reasons)
		}
	}
}

func TestReadDataServicesCeph(t *testing.T) {
	podJSON, err := json.Marshal(map[string]any{"items": cephPods()})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                                  `{"groups":[{"name":"ceph.rook.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/ceph.rook.io/v1/cephclusters":     cephClusterFixture,
		"GET /apis/ceph.rook.io/v1/cephblockpools":   cephBlockPoolFixture,
		"GET /apis/ceph.rook.io/v1/cephfilesystems":  cephFilesystemFixture,
		"GET /apis/ceph.rook.io/v1/cephobjectstores": cephObjStoreFixture,
		"GET /api/v1/pods":                           string(podJSON),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints("rook"), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.Ceph == nil || res.Ceph.Error != "" || len(res.Ceph.Clusters) != 5 || len(res.Ceph.Pools) != 4 || len(res.Ceph.OSDs) != 6 || res.Ceph.Version != "v1" {
		t.Fatalf("ceph: %+v", res.Ceph)
	}

	if res.Longhorn != nil || res.CNPG != nil || res.Garage != nil || res.Dragonfly != nil {
		t.Errorf("only Rook Ceph is installed: %+v", res)
	}

	// Its OSD and mon pods come from one label-selected listing.
	for _, r := range f.recorded() {
		if r.path == "/api/v1/pods" {
			return
		}
	}

	t.Error("pods not listed")
}

func TestCephDemoHasEveryState(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.Ceph == nil || len(demo.Ceph.Clusters) == 0 || len(demo.Ceph.OSDs) == 0 || len(demo.Ceph.Pools) == 0 {
		t.Fatal("demo misses Rook Ceph")
	}

	c := demo.Ceph.Clusters[0]
	if health, reasons := cephClusterHealth(c); health != c.Health || !slices.Equal(reasons, c.Reasons) {
		t.Errorf("demo cluster says %s %v, rules say %s %v", c.Health, c.Reasons, health, reasons)
	}

	var states []string
	for _, p := range demo.Ceph.Pools {
		states = append(states, p.Health)
	}

	if !strings.Contains(strings.Join(states, ","), healthWarning) || !strings.Contains(strings.Join(states, ","), healthOK) {
		t.Errorf("demo pool states: %v", states)
	}
}
