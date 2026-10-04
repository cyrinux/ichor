package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Shaped like mariadb-operator's objects on a real cluster: one MariaDB per cluster, its pods
// labelled app.kubernetes.io/name=mariadb and app.kubernetes.io/instance=<cluster>.
const mariadbFixture = `{"items":[
  {"metadata":{"name":"shop","namespace":"app"},"spec":{"replicas":2,"replication":{"enabled":true}},
   "status":{"currentPrimary":"shop-0","conditions":[{"type":"Ready","status":"True","reason":"StatefulSetReady","message":"Running"}]}},
  {"metadata":{"name":"down","namespace":"app"},"spec":{"replicas":2,"replication":{"enabled":true}},
   "status":{"currentPrimary":"down-0","conditions":[{"type":"Ready","status":"False","reason":"StatefulSetNotReady","message":"Not ready"}]}},
  {"metadata":{"name":"headless","namespace":"app"},"spec":{"replicas":2,"replication":{"enabled":true}},
   "status":{"currentPrimary":"headless-0","conditions":[{"type":"Ready","status":"False","reason":"SwitchPrimary","message":"Switching primary"}]}},
  {"metadata":{"name":"forum","namespace":"app"},"spec":{"replicas":3,"galera":{"enabled":true}},
   "status":{"currentPrimary":"forum-0","galeraRecovery":{"state":{}},
   "conditions":[{"type":"Ready","status":"False","reason":"GaleraNotReady","message":"Recovering Galera cluster"}]}},
  {"metadata":{"name":"rollout","namespace":"app"},"spec":{},
   "status":{"currentPrimary":"rollout-0","conditions":[{"type":"Ready","status":"False","reason":"Updating","message":"Updating"}]}},
  {"metadata":{"name":"nightly","namespace":"app"},"spec":{},
   "status":{"currentPrimary":"nightly-0","conditions":[{"type":"Ready","status":"True"}]}},
  {"metadata":{"name":"old","namespace":"app"},"spec":{},
   "status":{"currentPrimary":"old-0","conditions":[{"type":"Ready","status":"True"}]}},
  {"metadata":{"name":"paused","namespace":"app"},"spec":{"suspend":true},"status":{}}]}`

// Logical backups: shop's daily schedule has succeeded for a month (the condition's transition
// time stays put, its CronJob has the last run), old's last ran five days ago.
const mariadbBackupFixture = `{"items":[
  {"metadata":{"name":"shop-daily","namespace":"app"},"spec":{"mariaDbRef":{"name":"shop"},"schedule":{"cron":"0 3 * * *"}},
   "status":{"conditions":[{"type":"Complete","status":"True","reason":"CronJobSuccess","lastTransitionTime":"2026-09-01T03:05:00Z"}]}},
  {"metadata":{"name":"old-daily","namespace":"app"},"spec":{"mariaDbRef":{"name":"old","namespace":"app"},"schedule":{"cron":"0 3 * * *"}},
   "status":{"conditions":[{"type":"Complete","status":"True","reason":"CronJobSuccess","lastTransitionTime":"2026-09-28T03:05:00Z"}]}},
  {"metadata":{"name":"paused-daily","namespace":"app"},"spec":{"mariaDbRef":{"name":"paused"},"schedule":{"cron":"0 3 * * *","suspend":true}}}]}`

// A physical backup of nightly whose last run failed after an earlier success.
const mariadbPhysicalFixture = `{"items":[
  {"metadata":{"name":"nightly-snap","namespace":"app"},"spec":{"mariaDbRef":{"name":"nightly"},"schedule":{"cron":"0 1 * * *"}},
   "status":{"conditions":[{"type":"Complete","status":"False","reason":"JobFailed","message":"Failed","lastTransitionTime":"2026-10-03T01:10:00Z"}]}},
  {"metadata":{"name":"nightly-once","namespace":"app"},"spec":{"mariaDbRef":{"name":"nightly"}},
   "status":{"conditions":[{"type":"Complete","status":"True","reason":"JobComplete","lastTransitionTime":"2026-10-02T01:10:00Z"}]}}]}`

const mariadbCronJobFixture = `{"items":[
  {"metadata":{"namespace":"app","ownerReferences":[{"apiVersion":"k8s.mariadb.com/v1alpha1","kind":"Backup","name":"shop-daily"}]},
   "status":{"lastScheduleTime":"2026-10-03T03:00:00Z","lastSuccessfulTime":"2026-10-03T03:04:00Z"}},
  {"metadata":{"namespace":"app","ownerReferences":[{"apiVersion":"k8s.mariadb.com/v1alpha1","kind":"Backup","name":"old-daily"}]},
   "status":{"lastScheduleTime":"2026-09-28T03:00:00Z","lastSuccessfulTime":"2026-09-28T03:04:00Z"}},
  {"metadata":{"namespace":"app","ownerReferences":[{"apiVersion":"batch/v1","kind":"Other","name":"shop-daily"}]},
   "status":{"lastScheduleTime":"2026-10-03T11:00:00Z"}}]}`

func mariadbPods() []dsPod {
	pod := func(instance, name, node string, ready bool) dsPod {
		labels := map[string]string{"app.kubernetes.io/name": "mariadb", "app.kubernetes.io/instance": instance}
		p := fakePod("app", name, node, ready, labels, "mariadb", "docker-registry1.mariadb.com/library/mariadb:11.4")
		if !ready && node == "" {
			p.Status.Phase = "Pending"
		}

		return p
	}

	return []dsPod{
		pod("shop", "shop-1", "node-2", true),
		pod("shop", "shop-0", "node-1", true),
		pod("shop", "shop-backup-29321-x7k2p", "node-3", false), // a backup Job's pod
		pod("down", "down-0", "node-3", false),
		pod("down", "down-1", "", false),
		pod("headless", "headless-0", "node-3", false),
		pod("headless", "headless-1", "node-1", true),
		pod("forum", "forum-0", "node-1", true),
		pod("forum", "forum-1", "node-2", true),
		pod("forum", "forum-2", "node-3", false),
		pod("rollout", "rollout-0", "node-1", true),
		pod("nightly", "nightly-0", "node-2", true),
		pod("old", "old-0", "node-2", true),
		pod("paused", "paused-0", "node-1", true),
	}
}

func mariadbFixtureSources(t *testing.T) mariadbSources {
	t.Helper()

	var (
		mariadbs          kubeList[mariadbObject]
		logical, physical kubeList[mariadbBackupObject]
		cronJobs          kubeList[mariadbCronJob]
	)

	for doc, v := range map[string]any{mariadbFixture: &mariadbs, mariadbBackupFixture: &logical, mariadbPhysicalFixture: &physical, mariadbCronJobFixture: &cronJobs} {
		if err := json.Unmarshal([]byte(doc), v); err != nil {
			t.Fatal(err)
		}
	}

	return mariadbSources{mariadbs: mariadbs.Items, logical: logical.Items, physical: physical.Items, cronJobs: cronJobs.Items, pods: mariadbPods()}
}

func TestMapMariaDB(t *testing.T) {
	out := mapMariaDB(mariadbFixtureSources(t), fixtureNow)

	type summary struct {
		Name, Health, Topology, Primary string
		Ready                           int
		Reasons                         []string
	}

	var got []summary
	for _, c := range out.Clusters {
		got = append(got, summary{c.Name, c.Health, c.Topology, c.Primary, c.ReadyPods, c.Reasons})
	}

	want := []summary{
		{"down", healthCritical, "replication", "down-0", 0, []string{mariadbReasonNoReady}},
		{"headless", healthCritical, "replication", "headless-0", 1, []string{mariadbReasonNoPrimary, mariadbReasonPods}},
		{"forum", healthWarning, "galera", "forum-0", 2, []string{mariadbReasonPods, mariadbReasonGaleraRecovery}},
		{"nightly", healthWarning, "standalone", "nightly-0", 1, []string{mariadbReasonBackupFailed}},
		{"old", healthWarning, "standalone", "old-0", 1, []string{mariadbReasonBackupStale}},
		{"rollout", healthWarning, "standalone", "rollout-0", 1, []string{mariadbReasonNotReady}},
		{"shop", healthOK, "replication", "shop-0", 2, []string{}},
		{"paused", healthIdle, "standalone", "", 1, []string{}},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("clusters differ")
	}

	byName := map[string]mariadbCluster{}
	for _, c := range out.Clusters {
		byName[c.Name] = c
	}

	// The primary first, the backup Job's pod left out.
	shop := byName["shop"]
	if len(shop.Pods) != 2 || shop.Pods[0].Name != "shop-0" || shop.Pods[0].Role != "primary" || shop.Pods[1].Role != "replica" || shop.Pods[0].Node != "node-1" {
		t.Errorf("shop pods: %+v", shop.Pods)
	}

	// The CronJob's last success, not the condition's month-old transition.
	if shop.LastBackupAt != unixMilli("2026-10-03T03:04:00Z") || shop.LastBackupFailedAt != 0 || shop.BackupSchedule != "0 3 * * *" {
		t.Errorf("shop backups: %+v", shop)
	}

	if forum := byName["forum"]; forum.Pods[1].Role != "member" || forum.Message != "Recovering Galera cluster" {
		t.Errorf("forum: %+v", forum)
	}

	nightly := byName["nightly"]
	if nightly.LastBackupAt != unixMilli("2026-10-02T01:10:00Z") || nightly.LastBackupFailedAt != unixMilli("2026-10-03T01:10:00Z") {
		t.Errorf("nightly backups: %+v", nightly)
	}

	// A suspended schedule expects nothing.
	if paused := byName["paused"]; !paused.Suspended || paused.BackupSchedule != "" {
		t.Errorf("paused: %+v", paused)
	}
}

func TestMariaDBCronJobRunFailed(t *testing.T) {
	var run mariadbCronJob
	run.Status.LastScheduleTime, run.Status.LastSuccessfulTime = "2026-10-03T03:00:00Z", "2026-10-02T03:04:00Z"

	if acc := addMariaDBBackup(mariadbBackups{}, mariadbBackupObject{}, run); acc.lastFailure <= acc.lastSuccess {
		t.Errorf("a run that ended without success: %+v", acc)
	}

	// Still running: not a failure yet.
	run.Status.Active = []json.RawMessage{json.RawMessage(`{}`)}
	if acc := addMariaDBBackup(mariadbBackups{}, mariadbBackupObject{}, run); acc.lastFailure != 0 {
		t.Errorf("a running backup: %+v", acc)
	}
}

func TestReadDataServicesMariaDB(t *testing.T) {
	podJSON, err := json.Marshal(map[string]any{"items": mariadbPods()})
	if err != nil {
		t.Fatal(err)
	}

	// No physicalbackups route: an operator that predates them answers 404.
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"k8s.mariadb.com","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/k8s.mariadb.com/v1alpha1/mariadbs": mariadbFixture,
		"GET /apis/k8s.mariadb.com/v1alpha1/backups":  mariadbBackupFixture,
		"GET /apis/batch/v1/cronjobs":                 mariadbCronJobFixture,
		"GET /api/v1/pods":                            string(podJSON),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints("mariadb"), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.MariaDB == nil || res.MariaDB.Error != "" || len(res.MariaDB.Clusters) != 8 || res.MariaDB.Version != "v1alpha1" {
		t.Fatalf("mariadb: %+v", res.MariaDB)
	}

	if res.Longhorn != nil || res.CNPG != nil || res.Garage != nil || res.Dragonfly != nil {
		t.Errorf("only MariaDB is installed: %+v", res)
	}

	asked := map[string]bool{}
	for _, r := range f.recorded() {
		asked[r.path] = true
	}

	if !asked["/api/v1/pods"] || !asked["/apis/k8s.mariadb.com/v1alpha1/physicalbackups"] {
		t.Errorf("requests: %v", asked)
	}
}

func TestMariaDBWithoutScheduleSkipsCronJobs(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/k8s.mariadb.com/v1alpha1/mariadbs": mariadbFixture,
		"GET /apis/k8s.mariadb.com/v1alpha1/backups":  `{"items":[]}`,
		"GET /api/v1/pods":                            `{"items":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if out := readMariaDB(context.Background(), k, "v1alpha1", fixtureNow); out.Error != "" {
		t.Fatalf("error: %s", out.Error)
	}

	for _, r := range f.recorded() {
		if strings.Contains(r.path, "cronjobs") {
			t.Error("cronjobs listed without a scheduled backup")
		}
	}
}

func TestMariaDBDemoHasEveryState(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.MariaDB == nil || len(demo.MariaDB.Clusters) < 2 {
		t.Fatal("demo misses MariaDB")
	}

	var states []string
	for _, c := range demo.MariaDB.Clusters {
		states = append(states, c.Health)
	}

	if !strings.Contains(strings.Join(states, ","), healthWarning) || !strings.Contains(strings.Join(states, ","), healthOK) {
		t.Errorf("demo states: %v", states)
	}
}
