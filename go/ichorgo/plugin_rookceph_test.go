package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Rook Ceph built-in plugin against what the Data services reader it replaced said about
// the same objects (its tests' fixtures and expectations): the same health and first reason
// per cluster, the same OSD and mon counts, Ceph's checks errors first, the same pools, and
// findings where the monitor alerted.

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

const (
	cephGroups    = `{"groups":[{"name":"ceph.rook.io","preferredVersion":{"version":"v1"}}]}`
	cephResources = `{"resources":[{"name":"cephclusters","namespaced":true,"kind":"CephCluster"},` +
		`{"name":"cephblockpools","namespaced":true,"kind":"CephBlockPool"},{"name":"cephfilesystems","namespaced":true,"kind":"CephFilesystem"},` +
		`{"name":"cephobjectstores","namespaced":true,"kind":"CephObjectStore"}]}`
)

// cephPluginAPI serves Rook with clusters and pods; the pods are filtered by the list's label
// selector, as the API server does.
func cephPluginAPI(t *testing.T, clusters string, pods []dsPod) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                                  cephGroups,
		"GET /apis/ceph.rook.io/v1":                  cephResources,
		"GET /apis/ceph.rook.io/v1/cephclusters":     clusters,
		"GET /apis/ceph.rook.io/v1/cephblockpools":   cephBlockPoolFixture,
		"GET /apis/ceph.rook.io/v1/cephfilesystems":  cephFilesystemFixture,
		"GET /apis/ceph.rook.io/v1/cephobjectstores": cephObjStoreFixture,
	})
	f.answerQuery = func(path, query string) (string, bool) {
		if path != "/api/v1/pods" {
			return "", false
		}

		q, _ := url.ParseQuery(query)
		matched := slices.DeleteFunc(slices.Clone(pods), func(p dsPod) bool { return !labelsMatch(p.Metadata.Labels, q.Get("labelSelector")) })

		out, err := json.Marshal(map[string]any{"metadata": map[string]any{}, "items": matched})
		if err != nil {
			t.Error(err)
		}

		return string(out), true
	}

	useFakeKube(t, f)

	return f
}

// labelsMatch reads the equality selectors the plugins use: "k=v" and "k!=v", comma-separated.
func labelsMatch(labels map[string]string, selector string) bool {
	for req := range strings.SplitSeq(selector, ",") {
		if k, v, ok := strings.Cut(req, "!="); ok {
			if labels[k] == v {
				return false
			}

			continue
		}

		if k, v, ok := strings.Cut(req, "="); ok && labels[k] != v {
			return false
		}
	}

	return true
}

func rookBuiltin(t *testing.T) *plugin.Compiled {
	t.Helper()

	return useBuiltinPlugin(t, "rook", time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
}

// rookCard is the tree's card id.
func rookCard(t *testing.T, tree plugin.Tree, id string) plugin.TreeCard {
	t.Helper()

	for _, c := range tree.Cards {
		if c.ID == id {
			return c
		}
	}

	t.Fatalf("no card %s in %+v", id, tree.Cards)

	return plugin.TreeCard{}
}

// rowSection is the row's first section of a type, nil when it has none.
func rowSection(row plugin.TreeRow, typ string) *plugin.TreeSection {
	for i := range row.Detail {
		if row.Detail[i].Type == typ {
			return &row.Detail[i]
		}
	}

	return nil
}

// The clusters, OSDs, pools and findings of the reader's fixtures.
func TestRookPluginTreeMatchesTheReader(t *testing.T) {
	rookBuiltin(t)
	f := cephPluginAPI(t, cephClusterFixture, cephPods())

	tree := pluginData(t, "cfg", "ctx", "", "rook", "", "")
	if !tree.Active || tree.DetectedVia != "api" || len(tree.Errors) != 0 {
		t.Fatalf("tree active %v via %q, errors %+v", tree.Active, tree.DetectedVia, tree.Errors)
	}

	type cluster struct {
		Name, Health, Reason, Status   string
		OSDs, Mons, OSDsDown, MonsDown string
	}

	var got []cluster

	for _, row := range rookCard(t, tree, "clusters").Rows {
		kv, badges := rowItems(row, "keyValues"), rowItems(row, "badges")
		value := func(m map[string]map[string]any, label string) string { s, _ := m[label]["value"].(string); return s }
		got = append(got, cluster{
			row.Title, row.Health.Tone, row.Health.Reason, row.Status.Text,
			value(kv, "OSDs up"), value(kv, "Mons ready"), value(badges, "OSDs down"), value(badges, "mons down"),
		})
	}

	// The reader's order: health, then namespace/name; its first reason.
	want := []cluster{
		{"down", healthCritical, "noOSD", "HEALTH_OK", "0/2", "1/1", "2", ""},
		{"remote", healthCritical, "full", "HEALTH_OK", "0/0", "0/0", "", ""},
		{"ceph", healthCritical, "healthErr", "HEALTH_ERR", "2/3", "2/3", "1", "1"},
		{"new", healthWarning, "notReady", "Progressing", "0/0", "0/0", "", ""},
		{"ok", healthOK, "", "HEALTH_OK", "1/1", "1/1", "", ""},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("clusters differ")
	}

	ceph := rookCard(t, tree, "clusters").Rows[2]

	// Ceph's checks, errors first, then by name; the capacity.
	checks := rowSection(ceph, "text")
	if checks == nil || checks.Text != "1 full osd(s)\n1/3 mons down, quorum a,b\nDegraded data redundancy: 12 pgs degraded" || checks.Tone != healthCritical {
		t.Errorf("checks %+v", checks)
	}

	if g := rowSection(ceph, "gauge"); g == nil || *g.Value != 900 || *g.Max != 1000 || g.Tone != healthWarning {
		t.Errorf("capacity %+v", g)
	}

	if ceph.Trailing.Text != "19.2.3-0" {
		t.Errorf("version %+v", ceph.Trailing)
	}

	// A cluster being set up says Rook's message; one with checks or ready does not.
	if note := rowSection(rookCard(t, tree, "clusters").Rows[3], "text"); note == nil || note.Text != "Configuring Ceph Mons" {
		t.Errorf("new: %+v", note)
	}

	if note := rowSection(rookCard(t, tree, "clusters").Rows[4], "text"); note != nil {
		t.Errorf("ok shows %+v", note)
	}

	// OSDs by namespace, then by number (not as strings); a pod not ready is critical.
	var osds []string
	for _, row := range rookCard(t, tree, "osds").Rows {
		osds = append(osds, row.Subtitle+"/"+row.Title+"@"+row.Trailing.Text+":"+row.Health.Tone)
	}

	if want := []string{
		"ceph-down/osd.0@node-4:critical", "ceph-down/osd.1@node-4:critical", "ceph-ok/osd.0@node-1:ok",
		"storage/osd.1@node-3:critical", "storage/osd.2@node-2:ok", "storage/osd.10@node-1:ok",
	}; !slices.Equal(osds, want) {
		t.Errorf("osds %v", osds)
	}

	var pools []string
	for _, id := range []string{"blockPools", "filesystems", "objectStores"} {
		for _, row := range rookCard(t, tree, id).Rows {
			pools = append(pools, row.Status.Text+":"+row.Title+":"+row.Health.Tone)
		}
	}

	if want := []string{"block pool:broken:critical", "block pool:replicapool:ok", "filesystem:fs:warning", "object store:s3:ok"}; !slices.Equal(pools, want) {
		t.Errorf("pools %v", pools)
	}

	// The monitor alerted on the critical clusters and the failed pool: not on a cluster
	// still being set up.
	var findings []string
	for _, f := range tree.Findings {
		findings = append(findings, f.Severity+" "+f.ID+" "+f.Title)
	}

	slices.Sort(findings)

	if want := []string{
		"critical cluster-critical ceph-down/down", "critical cluster-critical ceph-ext/remote", "critical cluster-critical storage/ceph",
		"critical pool-failed storage/broken",
	}; !slices.Equal(findings, want) {
		t.Errorf("findings %v", findings)
	}

	if tree.Summary.Title != "5 Ceph clusters" || tree.Summary.Health != healthCritical {
		t.Errorf("summary %+v", tree.Summary)
	}

	// The OSD and mon pods come from two label-selected listings, canaries left out.
	var selectors []string
	for _, r := range f.recorded() {
		if r.path == "/api/v1/pods" {
			q, _ := url.ParseQuery(r.query)
			selectors = append(selectors, q.Get("labelSelector"))
		}
	}

	slices.Sort(selectors)

	if want := []string{"app=rook-ceph-mon,mon_canary!=true", "app=rook-ceph-osd"}; !slices.Equal(selectors, want) {
		t.Errorf("pods listed with %v", selectors)
	}
}

// cephPod is an OSD or mon pod of a namespace, ready or not.
func cephPod(ns, app, id string, ready bool) dsPod {
	return fakePod(ns, app+"-"+id, "node-1", ready, map[string]string{"app": app, "ceph-osd-id": id}, "c", "")
}

// cephPodsOf are n pods of app in ns, the first ready ones ready.
func cephPodsOf(ns, app string, ready, n int) []dsPod {
	out := []dsPod{}
	for i := range n {
		out = append(out, cephPod(ns, app, fmt.Sprint(i), i < ready))
	}

	return out
}

// Each rule of the reader's health, on its own: the worst reason wins.
func TestRookPluginClusterHealth(t *testing.T) {
	tests := []struct {
		name, status   string
		pods           []dsPod
		health, reason string
	}{
		{"error", `"phase":"Ready","ceph":{"health":"HEALTH_ERR"}`, nil, healthCritical, "healthErr"},
		{"failure", `"phase":"Failure"`, nil, healthCritical, "failure"},
		{"full", `"phase":"Ready","ceph":{"capacity":{"bytesTotal":100,"bytesUsed":96}}`, nil, healthCritical, "full"},
		{"exactly-85", `"phase":"Ready","ceph":{"capacity":{"bytesTotal":100,"bytesUsed":85}}`, nil, healthOK, ""},
		{"near-full", `"phase":"Ready","ceph":{"capacity":{"bytesTotal":100,"bytesUsed":90}}`, nil, healthWarning, "nearFull"},
		{"no-osd", `"phase":"Ready"`, cephPodsOf("no-osd", "rook-ceph-osd", 0, 2), healthCritical, "noOSD"},
		{"osd-down", `"phase":"Ready"`, cephPodsOf("osd-down", "rook-ceph-osd", 2, 3), healthWarning, "osds"},
		{"no-quorum", `"phase":"Ready"`, cephPodsOf("no-quorum", "rook-ceph-mon", 1, 3), healthCritical, "noQuorum"},
		{"one-mon-of-two", `"phase":"Ready"`, cephPodsOf("one-mon-of-two", "rook-ceph-mon", 1, 2), healthCritical, "noQuorum"},
		{"one-mon-down", `"phase":"Ready"`, cephPodsOf("one-mon-down", "rook-ceph-mon", 4, 5), healthWarning, "mons"},
		// Progressing with something else wrong says only the rest.
		{"progressing", `"phase":"Progressing","ceph":{"health":"HEALTH_WARN"}`, nil, healthWarning, "healthWarn"},
		{"connected", `"phase":"Connected"`, nil, healthOK, ""},
	}

	var (
		items []string
		pods  []dsPod
	)

	for _, tt := range tests {
		items = append(items, `{"metadata":{"name":"c","namespace":"`+tt.name+`"},"status":{`+tt.status+`}}`)
		pods = append(pods, tt.pods...)
	}

	rookBuiltin(t)
	cephPluginAPI(t, `{"items":[`+strings.Join(items, ",")+`]}`, pods)

	got := map[string]string{}
	for _, row := range rookCard(t, pluginData(t, "cfg", "ctx", "", "rook", "", ""), "clusters").Rows {
		got[row.Subtitle] = row.Health.Tone + " " + row.Health.Reason
	}

	for _, tt := range tests {
		if want := tt.health + " " + tt.reason; got[tt.name] != want {
			t.Errorf("%s: %q, want %q", tt.name, got[tt.name], want)
		}
	}
}

// A Ceph warning, near-full capacity or an OSD down alerts; a mon down or a cluster still
// being set up is shown, not alerted.
func TestRookPluginWarningFindings(t *testing.T) {
	rookBuiltin(t)
	cephPluginAPI(t, `{"items":[
	  {"metadata":{"name":"c","namespace":"warn"},"status":{"phase":"Ready","ceph":{"health":"HEALTH_WARN"}}},
	  {"metadata":{"name":"c","namespace":"near"},"status":{"phase":"Ready","ceph":{"capacity":{"bytesTotal":100,"bytesUsed":90}}}},
	  {"metadata":{"name":"c","namespace":"osd"},"status":{"phase":"Ready"}},
	  {"metadata":{"name":"c","namespace":"mon"},"status":{"phase":"Ready"}},
	  {"metadata":{"name":"c","namespace":"new"},"status":{"phase":"Progressing"}}]}`,
		slices.Concat(cephPodsOf("osd", "rook-ceph-osd", 1, 2), cephPodsOf("mon", "rook-ceph-mon", 2, 3)))

	var findings []string

	for _, f := range pluginData(t, "cfg", "ctx", "", "rook", "", "").Findings {
		if strings.HasPrefix(f.ID, "cluster-") {
			findings = append(findings, f.Severity+" "+f.Title)
		}
	}

	slices.Sort(findings)

	if want := []string{"warning near/c", "warning osd/c", "warning warn/c"}; !slices.Equal(findings, want) {
		t.Errorf("findings %v", findings)
	}
}

// The French tree takes the apps' former strings.
func TestRookPluginFrench(t *testing.T) {
	rookBuiltin(t)
	cephPluginAPI(t, cephClusterFixture, cephPods())

	tree := pluginData(t, "cfg", "ctx", "", "rook", "", "fr")
	if tree.Summary.Title != "5 clusters Ceph" || rookCard(t, tree, "clusters").Empty != "Aucun cluster Ceph." {
		t.Fatalf("summary %+v", tree.Summary)
	}

	if h := rookCard(t, tree, "clusters").Rows[0].Health; h.Message != "aucun OSD actif" {
		t.Errorf("down in French: %+v", h)
	}
}

// The demo renders the plugin's samples: the former demo's nearly full cluster with an OSD
// down, and one cluster per kind of trouble.
func TestRookPluginDemo(t *testing.T) {
	rookBuiltin(t)
	noKubeDial(t)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "rook", "", "")

	health := map[string]string{}
	for _, row := range rookCard(t, tree, "clusters").Rows {
		health[row.Title] = row.Health.Tone + " " + row.Health.Reason
	}

	want := map[string]string{"rook-ceph": "warning healthWarn", "archive": "critical healthErr", "scratch": "warning notReady"}
	if fmt.Sprint(health) != fmt.Sprint(want) || len(tree.Errors) != 0 {
		t.Fatalf("demo rows %v, errors %+v", health, tree.Errors)
	}
}

// Data services no longer carry Rook Ceph: the plugin does.
func TestDataServicesHaveNoCeph(t *testing.T) {
	raw, err := json.Marshal(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(jsonKeys(raw), "ceph") {
		t.Error("the Data services JSON still has a ceph key")
	}
}
