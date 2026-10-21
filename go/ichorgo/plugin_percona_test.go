package ichorgo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Percona XtraDB Cluster built-in plugin against what the Data services reader it replaced
// said about the same objects (its tests' fixtures and expectations): the same health and first
// reason per cluster, the operator's member and proxy counts, its messages, the backups' last
// success and failure (native:percona-backups) and schedules, and findings where the monitor
// alerted.

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

const perconaGroups = `{"groups":[{"name":"pxc.percona.com","preferredVersion":{"version":"v1"}}]}`

// perconaPluginAPI serves the operator with the clusters and backups given, and the pods
// filtered by the list's label selector.
func perconaPluginAPI(t *testing.T, clusters, backups string, pods []dsPod) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                    perconaGroups,
		"GET /apis/pxc.percona.com/v1": `{"resources":[{"name":"perconaxtradbclusters","namespaced":true,"kind":"PerconaXtraDBCluster"}]}`,
		"GET /apis/pxc.percona.com/v1/perconaxtradbclusters":       clusters,
		"GET /apis/pxc.percona.com/v1/perconaxtradbclusterbackups": backups,
	})
	f.answerQuery = podsBySelector(t, pods)

	useFakeKube(t, f)

	return f
}

func perconaBuiltin(t *testing.T) *plugin.Compiled {
	t.Helper()

	return useBuiltinPlugin(t, "percona-xtradb", fixtureNow)
}

// The clusters, pods and findings of the reader's fixtures.
func TestPerconaPluginTreeMatchesTheReader(t *testing.T) {
	perconaBuiltin(t)
	f := perconaPluginAPI(t, perconaFixture, perconaBackupFixture, perconaPods())

	tree := pluginData(t, "cfg", "ctx", "", "percona-xtradb", "", "")
	if !tree.Active || tree.DetectedVia != "api" || len(tree.Errors) != 0 {
		t.Fatalf("tree active %v via %q, errors %+v", tree.Active, tree.DetectedVia, tree.Errors)
	}

	type cluster struct {
		Name, Health, Reason, Members, Proxy string
	}

	var got []cluster

	for _, row := range rookCard(t, tree, "clusters").Rows {
		proxy, _ := rowItems(row, "keyValues")["Proxy"]["value"].(string)
		got = append(got, cluster{row.Title, row.Health.Tone, row.Health.Reason, row.Trailing.Text, proxy})
	}

	// The reader's first reason, members and proxy; the paused cluster is idle, which sorts
	// before ok here (after it in the reader).
	want := []cluster{
		{"down", healthCritical, "error", "0/3", "HAProxy 0/2"},
		{"crm", healthWarning, "members", "2/3", "ProxySQL 1/2"},
		{"fresh", healthWarning, "initializing", "1/1", ""},
		{"wiki", healthWarning, "backupFailed", "1/1", ""},
		{"old", healthIdle, "paused", "0/3", "HAProxy 0/2"},
		{"shop", healthOK, "", "3/3", "HAProxy 2/2"},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("clusters differ")
	}

	rows := rookCard(t, tree, "clusters").Rows

	// A failure before the last success is over; the schedules that should keep them coming.
	shop := rowItems(rows[5], "keyValues")
	if shop["Last backup"]["value"] != "10h ago" || shop["Last failure"]["value"] != "34h ago" || shop["Schedules"]["value"] != "0 2 * * *, 0 3 * * 0" {
		t.Errorf("shop backups: %v", shop)
	}

	// The operator's messages while the cluster needs a look; none for a healthy one.
	if msg := rowSection(rows[0], "text"); msg == nil || msg.Text != "PXC: pxc: back-off restarting failed container" {
		t.Errorf("down message: %+v", msg)
	}

	if rowSection(rows[5], "text") != nil {
		t.Errorf("shop shows a message: %+v", rows[5].Detail)
	}

	// The members only, by name; one not scheduled anywhere is critical.
	var pods []string
	for _, row := range rookCard(t, tree, "pods").Rows {
		pods = append(pods, row.Title+"@"+row.Trailing.Text+":"+row.Health.Tone)
	}

	if want := []string{
		"crm-pxc-0@node-1:ok", "crm-pxc-1@node-2:ok", "crm-pxc-2@:critical", "down-pxc-0@:critical",
		"shop-pxc-0@node-1:ok", "shop-pxc-1@node-2:ok", "shop-pxc-2@node-3:ok",
	}; !slices.Equal(pods, want) {
		t.Errorf("pods %v", pods)
	}

	// The monitor alerted on the critical cluster, the member missing and the failed backup:
	// not on the operator still initializing.
	var findings []string
	for _, f := range tree.Findings {
		findings = append(findings, f.Severity+" "+f.ID+" "+f.Title)
	}

	slices.Sort(findings)

	if want := []string{"critical cluster-critical db/down", "warning cluster-warning db/crm", "warning cluster-warning db/wiki"}; !slices.Equal(findings, want) {
		t.Errorf("findings %v", findings)
	}

	if tree.Summary.Title != "6 Percona clusters" || tree.Summary.Health != healthCritical {
		t.Errorf("summary %+v", tree.Summary)
	}

	if sel := podSelectors(f); !slices.Equal(sel, []string{"app.kubernetes.io/name=percona-xtradb-cluster,app.kubernetes.io/component=pxc"}) {
		t.Errorf("pods listed with %v", sel)
	}
}

// Every rule of the reader's health on its own; a size of 0 in the status reads the spec's.
func TestPerconaPluginHealth(t *testing.T) {
	tests := []struct {
		name, spec, status, health, reason string
	}{
		{"no-member", `{"pxc":{"size":3}}`, `{"state":"initializing","pxc":{"size":3}}`, healthCritical, "noMember"},
		{"spec-size", `{"pxc":{"size":2}}`, `{"state":"initializing"}`, healthCritical, "noMember"},
		{"proxy", `{"pxc":{"size":1},"haproxy":{"enabled":true,"size":2}}`, `{"state":"ready","pxc":{"size":1,"ready":1},"haproxy":{"ready":1}}`, healthWarning, "proxy"},
		{"both-proxies", `{"pxc":{"size":1},"haproxy":{"enabled":true,"size":1},"proxysql":{"enabled":true,"size":3}}`, `{"state":"ready","pxc":{"size":1,"ready":1},"haproxy":{"size":1,"ready":1}}`, healthOK, ""},
		{"stopped", `{"pxc":{"size":0}}`, `{"state":"ready"}`, healthOK, ""},
		{"error-case", `{"pxc":{"size":1}}`, `{"state":"Error","pxc":{"size":1,"ready":1}}`, healthCritical, "error"},
		{"paused-state", `{"pxc":{"size":1}}`, `{"state":"paused"}`, healthIdle, "paused"},
	}

	var items []string
	for _, tt := range tests {
		items = append(items, fmt.Sprintf(`{"metadata":{"name":"c","namespace":%q,"creationTimestamp":"2026-01-01T00:00:00Z"},"spec":%s,"status":%s}`, tt.name, tt.spec, tt.status))
	}

	perconaBuiltin(t)
	perconaPluginAPI(t, `{"items":[`+strings.Join(items, ",")+`]}`, `{"items":[]}`, nil)

	got := map[string]string{}
	for _, row := range rookCard(t, pluginData(t, "cfg", "ctx", "", "percona-xtradb", "", ""), "clusters").Rows {
		got[row.Subtitle] = row.Health.Tone + " " + row.Health.Reason
	}

	for _, tt := range tests {
		if want := tt.health + " " + tt.reason; got[tt.name] != want {
			t.Errorf("%s: %q, want %q", tt.name, got[tt.name], want)
		}
	}
}

// A cluster is late when its last success (else its creation) is older than twice its most
// frequent schedule's interval, that interval read as the reader's cronInterval reads it, for
// every kind of cron: just inside is fine, just past is late.
func TestPerconaPluginBackupStale(t *testing.T) {
	crons := []string{
		"0 2 * * *", "0 3 * * 0", "0 4 1 * *", "@daily", "@midnight", "@hourly", "@weekly", "@monthly", "@yearly", "@annually",
		"@every 6h", "*/5 * * * *", "0 0 2 * * *", "0 0 2 * * MON", "0 0 2 1 * ?", "0 2 ? * *", "bogus", "0 2 * *", "@every",
	}

	var clusters, backups []string

	expect := map[string]string{}

	for i, cron := range crons {
		interval := cronInterval(cron)

		for _, c := range []struct {
			suffix string
			age    time.Duration
			health string
		}{{"in", 2*interval - time.Hour, healthOK}, {"late", 2*interval + time.Hour, healthWarning}} {
			ns := fmt.Sprintf("c%d-%s", i, c.suffix)
			expect[ns] = c.health

			clusters = append(clusters, fmt.Sprintf(`{"metadata":{"name":"c","namespace":%q,"creationTimestamp":"2020-01-01T00:00:00Z"},`+
				`"spec":{"pxc":{"size":1},"backup":{"schedule":[{"name":"s","schedule":%q}]}},"status":{"state":"ready","pxc":{"size":1,"ready":1}}}`, ns, cron))
			backups = append(backups, fmt.Sprintf(`{"metadata":{"namespace":%q,"creationTimestamp":"2020-01-01T00:00:00Z"},"spec":{"pxcCluster":"c"},`+
				`"status":{"state":"Succeeded","completed":%q}}`, ns, fixtureNow.Add(-c.age).Format(time.RFC3339)))
		}
	}

	// Two schedules: the most frequent one decides. Never backed up: late only once the
	// cluster is older than that. No schedule: never late.
	clusters = append(clusters,
		`{"metadata":{"name":"c","namespace":"two","creationTimestamp":"2026-01-01T00:00:00Z"},"spec":{"pxc":{"size":1},"backup":{"schedule":[{"schedule":"0 3 * * 0"},{"schedule":"0 2 * * *"}]}},"status":{"state":"ready","pxc":{"size":1,"ready":1}}}`,
		`{"metadata":{"name":"c","namespace":"never","creationTimestamp":"2026-01-01T00:00:00Z"},"spec":{"pxc":{"size":1},"backup":{"schedule":[{"schedule":"0 2 * * *"}]}},"status":{"state":"ready","pxc":{"size":1,"ready":1}}}`,
		`{"metadata":{"name":"c","namespace":"young","creationTimestamp":"2026-10-02T12:00:00Z"},"spec":{"pxc":{"size":1},"backup":{"schedule":[{"schedule":"0 2 * * *"}]}},"status":{"state":"ready","pxc":{"size":1,"ready":1}}}`,
		`{"metadata":{"name":"c","namespace":"none","creationTimestamp":"2026-01-01T00:00:00Z"},"spec":{"pxc":{"size":1}},"status":{"state":"ready","pxc":{"size":1,"ready":1}}}`)
	backups = append(backups, `{"metadata":{"namespace":"two","creationTimestamp":"2026-10-01T10:00:00Z"},"spec":{"pxcCluster":"c"},"status":{"state":"Succeeded","completed":"2026-10-01T10:00:00Z"}}`)
	expect["two"], expect["never"], expect["young"], expect["none"] = healthWarning, healthWarning, healthOK, healthOK

	perconaBuiltin(t)
	perconaPluginAPI(t, `{"items":[`+strings.Join(clusters, ",")+`]}`, `{"items":[`+strings.Join(backups, ",")+`]}`, nil)

	got := map[string]string{}
	for _, row := range rookCard(t, pluginData(t, "cfg", "ctx", "", "percona-xtradb", "", ""), "clusters").Rows {
		got[row.Subtitle] = row.Health.Tone
		if row.Health.Tone == healthWarning && row.Health.Reason != "backupStale" {
			t.Errorf("%s: %+v", row.Subtitle, row.Health)
		}
	}

	for ns, want := range expect {
		if got[ns] != want {
			t.Errorf("%s: %q, want %q", ns, got[ns], want)
		}
	}
}

// The French tree takes the apps' former strings.
func TestPerconaPluginFrench(t *testing.T) {
	perconaBuiltin(t)
	perconaPluginAPI(t, perconaFixture, perconaBackupFixture, perconaPods())

	tree := pluginData(t, "cfg", "ctx", "", "percona-xtradb", "", "fr")
	if tree.Summary.Title != "6 clusters Percona" || rookCard(t, tree, "clusters").Empty != "Aucun cluster Percona XtraDB." {
		t.Fatalf("summary %+v", tree.Summary)
	}

	if h := rookCard(t, tree, "clusters").Rows[1].Health; h.Message != "membres manquants" {
		t.Errorf("crm in French: %+v", h)
	}
}

// The demo renders the plugin's samples: the former demo's shop cluster with a member missing,
// its healthy cluster behind ProxySQL and its paused one, and one cluster per other state.
func TestPerconaPluginDemo(t *testing.T) {
	perconaBuiltin(t)
	noKubeDial(t)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "percona-xtradb", "", "")

	health := map[string]string{}
	for _, row := range rookCard(t, tree, "clusters").Rows {
		health[row.Title] = row.Health.Tone + " " + row.Health.Reason
	}

	want := map[string]string{
		"shop-mysql": "warning members", "crm-mysql": "ok ", "legacy-mysql": "idle paused",
		"billing-mysql": "critical error", "search-mysql": "critical noMember", "cache-mysql": "warning proxy",
		"fresh-mysql": "warning initializing", "wiki-mysql": "warning backupFailed", "reports-mysql": "warning backupStale",
	}
	if fmt.Sprint(health) != fmt.Sprint(want) || len(tree.Errors) != 0 {
		t.Fatalf("demo rows %v, errors %+v", health, tree.Errors)
	}
}

// Data services no longer carry Percona: the plugin does.
func TestDataServicesHaveNoPercona(t *testing.T) {
	raw, err := json.Marshal(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(jsonKeys(raw), "percona") {
		t.Error("the Data services JSON still has a percona key")
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
