package ichorgo

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The MariaDB built-in plugin against what the Data services reader it replaced said about the
// same objects (its tests' fixtures and expectations): the same health and first reason per
// cluster, the same ready pods, primary and roles, the backups' last success, failure and
// schedule (native:mariadb-backups), and findings where the monitor alerted.

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
   "status":{"conditions":[{"type":"Complete","status":"True","reason":"JobFailed","message":"Failed","lastTransitionTime":"2026-10-03T01:10:00Z"}]}},
  {"metadata":{"name":"nightly-once","namespace":"app"},"spec":{"mariaDbRef":{"name":"nightly"}},
   "status":{"conditions":[{"type":"Complete","status":"True","reason":"JobComplete","lastTransitionTime":"2026-10-02T01:10:00Z"}]}}]}`

const mariadbCronJobFixture = `{"items":[
  {"metadata":{"namespace":"app","ownerReferences":[{"apiVersion":"k8s.mariadb.com/v1alpha1","kind":"Backup","name":"shop-daily"}]},
   "status":{"lastScheduleTime":"2026-10-03T03:00:00Z","lastSuccessfulTime":"2026-10-03T03:04:00Z"}},
  {"metadata":{"namespace":"app","ownerReferences":[{"apiVersion":"k8s.mariadb.com/v1alpha1","kind":"Backup","name":"old-daily"}]},
   "status":{"lastScheduleTime":"2026-09-28T03:00:00Z","lastSuccessfulTime":"2026-09-28T03:04:00Z"}},
  {"metadata":{"namespace":"app","ownerReferences":[{"apiVersion":"batch/v1","kind":"Other","name":"shop-daily"}]},
   "status":{"lastScheduleTime":"2026-10-03T11:00:00Z"}}]}`

// mariadbPods are the clusters' StatefulSet pods (the StatefulSet controller labels each with
// statefulset.kubernetes.io/pod-name) and a backup Job's pod, which carries the operator's
// labels but not that one.
func mariadbPods() []dsPod {
	pod := func(instance, name, node string, ready bool) dsPod {
		labels := map[string]string{"app.kubernetes.io/name": "mariadb", "app.kubernetes.io/instance": instance, "statefulset.kubernetes.io/pod-name": name}
		p := fakePod("app", name, node, ready, labels, "mariadb", "docker-registry1.mariadb.com/library/mariadb:11.4")
		if !ready && node == "" {
			p.Status.Phase = "Pending"
		}

		return p
	}

	job := pod("shop", "shop-backup-29321-x7k2p", "node-3", false)
	delete(job.Metadata.Labels, "statefulset.kubernetes.io/pod-name")

	return []dsPod{
		pod("shop", "shop-1", "node-2", true),
		pod("shop", "shop-0", "node-1", true),
		job,
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

const mariadbGroups = `{"groups":[{"name":"k8s.mariadb.com","preferredVersion":{"version":"v1alpha1"}}]}`

// mariadbPluginAPI serves mariadb-operator with the clusters, the backups given (a missing
// physical backups answer is a 404, as from an operator that predates them), the CronJobs and
// the pods, filtered by the list's label selector.
func mariadbPluginAPI(t *testing.T, clusters, logical, physical string, pods []dsPod) *fakeKubeAPI {
	t.Helper()

	answers := map[string]string{
		"GET /apis":                                   mariadbGroups,
		"GET /apis/k8s.mariadb.com/v1alpha1":          `{"resources":[{"name":"mariadbs","namespaced":true,"kind":"MariaDB"}]}`,
		"GET /apis/k8s.mariadb.com/v1alpha1/mariadbs": clusters,
		"GET /apis/k8s.mariadb.com/v1alpha1/backups":  logical,
		"GET /apis/batch/v1/cronjobs":                 mariadbCronJobFixture,
	}
	if physical != "" {
		answers["GET /apis/k8s.mariadb.com/v1alpha1/physicalbackups"] = physical
	}

	f := newFakeKubeAPI(t, answers)
	f.answerQuery = podsBySelector(t, pods)

	useFakeKube(t, f)

	return f
}

func mariadbBuiltin(t *testing.T) *plugin.Compiled {
	t.Helper()

	return useBuiltinPlugin(t, "mariadb", fixtureNow)
}

// The clusters, pods and findings of the reader's fixtures.
func TestMariaDBPluginTreeMatchesTheReader(t *testing.T) {
	mariadbBuiltin(t)
	f := mariadbPluginAPI(t, mariadbFixture, mariadbBackupFixture, mariadbPhysicalFixture, mariadbPods())

	tree := pluginData(t, "cfg", "ctx", "", "mariadb", "", "")
	if !tree.Active || tree.DetectedVia != "api" || len(tree.Errors) != 0 {
		t.Fatalf("tree active %v via %q, errors %+v", tree.Active, tree.DetectedVia, tree.Errors)
	}

	type cluster struct {
		Name, Health, Reason, Topology, Primary, Ready string
		LastBackup, LastFailure, Schedule              string
	}

	var got []cluster

	for _, row := range rookCard(t, tree, "clusters").Rows {
		kv := rowItems(row, "keyValues")
		value := func(label string) string { s, _ := kv[label]["value"].(string); return s }
		got = append(got, cluster{
			row.Title, row.Health.Tone, row.Health.Reason, row.Status.Text, value("Primary"), row.Trailing.Text,
			value("Last backup"), value("Last failure"), value("Schedule"),
		})
	}

	// The reader's first reason, ready pods and primary; the CronJob's last success rather
	// than the condition's month-old transition; a suspended schedule expects nothing. The
	// suspended cluster is idle, which sorts before ok here (after it in the reader).
	want := []cluster{
		{"down", healthCritical, "noReady", "replication", "down-0", "0/2", "never", "never", ""},
		{"headless", healthCritical, "noPrimary", "replication", "headless-0", "1/2", "never", "never", ""},
		{"forum", healthWarning, "pods", "Galera", "forum-0", "2/3", "never", "never", ""},
		{"nightly", healthWarning, "backupFailed", "standalone", "nightly-0", "1/1", "35h ago", "11h ago", "0 1 * * *"},
		{"old", healthWarning, "backupStale", "standalone", "old-0", "1/1", "5d ago", "never", "0 3 * * *"},
		{"rollout", healthWarning, "notReady", "standalone", "rollout-0", "1/1", "never", "never", ""},
		{"paused", healthIdle, "suspended", "suspended", "", "1/1", "never", "never", ""},
		{"shop", healthOK, "", "replication", "shop-0", "2/2", "9h ago", "never", "0 3 * * *"},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("clusters differ")
	}

	if h := rookCard(t, tree, "clusters").Rows[5].Health; h.Message != "operator: Updating" {
		t.Errorf("rollout: %+v", h)
	}

	// The primary first under its cluster, members of a Galera cluster, the backup Job's pod
	// left out.
	var pods []string
	for _, row := range rookCard(t, tree, "pods").Rows {
		if strings.HasPrefix(row.Title, "shop") || strings.HasPrefix(row.Title, "forum") {
			pods = append(pods, row.Title+":"+row.Status.Text+"@"+row.Trailing.Text+":"+row.Health.Tone)
		}
	}

	if want := []string{
		"forum-0:primary@node-1:ok", "forum-1:member@node-2:ok", "forum-2:member@node-3:critical",
		"shop-0:primary@node-1:ok", "shop-1:replica@node-2:ok",
	}; !slices.Equal(pods, want) {
		t.Errorf("pods %v", pods)
	}

	// The monitor alerted on the critical clusters, a Galera recovery and the backups: not on
	// a rollout.
	var findings []string
	for _, f := range tree.Findings {
		findings = append(findings, f.Severity+" "+f.ID+" "+f.Title)
	}

	slices.Sort(findings)

	if want := []string{
		"critical cluster-critical app/down", "critical cluster-critical app/headless",
		"warning cluster-warning app/forum", "warning cluster-warning app/nightly", "warning cluster-warning app/old",
	}; !slices.Equal(findings, want) {
		t.Errorf("findings %v", findings)
	}

	if tree.Summary.Title != "8 MariaDB clusters" || tree.Summary.Health != healthCritical {
		t.Errorf("summary %+v", tree.Summary)
	}

	// The StatefulSets' pods come from a label-selected listing; physical backups were asked.
	if sel := podSelectors(f); !slices.Equal(sel, []string{"app.kubernetes.io/name=mariadb,statefulset.kubernetes.io/pod-name"}) {
		t.Errorf("pods listed with %v", sel)
	}
}

// Each rule of the reader's health on its own.
func TestMariaDBPluginHealth(t *testing.T) {
	ready := func(cluster, name string) dsPod {
		labels := map[string]string{"app.kubernetes.io/name": "mariadb", "app.kubernetes.io/instance": "c", "statefulset.kubernetes.io/pod-name": name}

		return fakePod(cluster, name, "node-1", true, labels, "mariadb", "")
	}

	tests := []struct {
		name, spec, status string
		pods               []dsPod
		health, reason     string
	}{
		// The primary down but a replica ready: no primary.
		{"no-primary", `{"replicas":2,"replication":{"enabled":true}}`, `{"currentPrimary":"c-0"}`, []dsPod{ready("no-primary", "c-1")}, healthCritical, "noPrimary"},
		// A Galera cluster has no primary to lose.
		{"galera", `{"replicas":1,"galera":{"enabled":true}}`, `{"currentPrimary":"c-9"}`, []dsPod{ready("galera", "c-0")}, healthOK, ""},
		{"stopped", `{"replicas":0}`, `{}`, nil, healthOK, ""},
		{"not-ready", `{}`, `{"conditions":[{"type":"Ready","status":"False"}]}`, []dsPod{ready("not-ready", "c-0")}, healthWarning, "notReady"},
		{"unknown", `{}`, `{"conditions":[{"type":"Ready","status":"Unknown"}]}`, []dsPod{ready("unknown", "c-0")}, healthOK, ""},
		{"recovering", `{}`, `{"galeraRecovery":{}}`, []dsPod{ready("recovering", "c-0")}, healthWarning, "galeraRecovery"},
	}

	var (
		items []string
		pods  []dsPod
	)

	for _, tt := range tests {
		items = append(items, fmt.Sprintf(`{"metadata":{"name":"c","namespace":%q},"spec":%s,"status":%s}`, tt.name, tt.spec, tt.status))
		pods = append(pods, tt.pods...)
	}

	mariadbBuiltin(t)
	mariadbPluginAPI(t, `{"items":[`+strings.Join(items, ",")+`]}`, `{"items":[]}`, `{"items":[]}`, pods)

	got, messages := map[string]string{}, map[string]string{}
	for _, row := range rookCard(t, pluginData(t, "cfg", "ctx", "", "mariadb", "", ""), "clusters").Rows {
		got[row.Subtitle] = row.Health.Tone + " " + row.Health.Reason
		messages[row.Subtitle] = row.Health.Message
	}

	for _, tt := range tests {
		if want := tt.health + " " + tt.reason; got[tt.name] != want {
			t.Errorf("%s: %q, want %q", tt.name, got[tt.name], want)
		}
	}

	// Without the operator's words, the reason says so in the app's.
	if messages["not-ready"] != "not ready" {
		t.Errorf("not-ready: %q", messages["not-ready"])
	}
}

// A CronJob run that ended without a success is a failure; one still running is not yet.
func TestMariaDBCronJobRunFailed(t *testing.T) {
	var run mariadbCronJob
	run.Status.LastScheduleTime, run.Status.LastSuccessfulTime = "2026-10-03T03:00:00Z", "2026-10-02T03:04:00Z"

	if acc := addMariaDBBackup(mariadbBackups{}, mariadbBackupObject{}, run); acc.lastFailure <= acc.lastSuccess {
		t.Errorf("a run that ended without success: %+v", acc)
	}

	run.Status.Active = []json.RawMessage{json.RawMessage(`{}`)}
	if acc := addMariaDBBackup(mariadbBackups{}, mariadbBackupObject{}, run); acc.lastFailure != 0 {
		t.Errorf("a running backup: %+v", acc)
	}
}

// Without a scheduled backup no CronJob is listed; an operator without physical backups
// (404) is no error.
func TestMariaDBPluginWithoutScheduleSkipsCronJobs(t *testing.T) {
	mariadbBuiltin(t)
	f := mariadbPluginAPI(t, mariadbFixture, `{"items":[]}`, "", nil)

	if tree := pluginData(t, "cfg", "ctx", "", "mariadb", "", ""); len(tree.Errors) != 0 {
		t.Fatalf("errors %+v", tree.Errors)
	}

	asked := map[string]bool{}
	for _, r := range f.recorded() {
		asked[r.path] = true
	}

	if asked["/apis/batch/v1/cronjobs"] || !asked["/apis/k8s.mariadb.com/v1alpha1/physicalbackups"] {
		t.Errorf("requests: %v", asked)
	}
}

// A namespace scope reads the backups and CronJobs of that namespace only.
func TestMariaDBPluginScope(t *testing.T) {
	mariadbBuiltin(t)

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": mariadbGroups,
		"GET /apis/k8s.mariadb.com/v1alpha1/namespaces/app/mariadbs":        mariadbFixture,
		"GET /apis/k8s.mariadb.com/v1alpha1/namespaces/app/backups":         mariadbBackupFixture,
		"GET /apis/k8s.mariadb.com/v1alpha1/namespaces/app/physicalbackups": mariadbPhysicalFixture,
		"GET /apis/batch/v1/namespaces/app/cronjobs":                        mariadbCronJobFixture,
	})
	f.answerQuery = podsBySelector(t, nil)
	useFakeKube(t, f)

	tree := pluginData(t, "cfg", "ctx", "", "mariadb", "app", "")
	if len(tree.Errors) != 0 {
		t.Fatalf("errors %+v", tree.Errors)
	}

	backups := map[string]any{}
	for _, row := range rookCard(t, tree, "clusters").Rows {
		backups[row.Title] = rowItems(row, "keyValues")["Last backup"]["value"]
	}

	if backups["shop"] != "9h ago" || backups["nightly"] != "35h ago" {
		t.Errorf("last backups %v", backups)
	}
}

// The French tree takes the apps' former strings.
func TestMariaDBPluginFrench(t *testing.T) {
	mariadbBuiltin(t)
	mariadbPluginAPI(t, mariadbFixture, mariadbBackupFixture, mariadbPhysicalFixture, mariadbPods())

	tree := pluginData(t, "cfg", "ctx", "", "mariadb", "", "fr")
	if tree.Summary.Title != "8 clusters MariaDB" || rookCard(t, tree, "clusters").Empty != "Aucun cluster MariaDB." {
		t.Fatalf("summary %+v", tree.Summary)
	}

	rows := rookCard(t, tree, "clusters").Rows
	if h := rows[1].Health; h.Message != "aucun primaire" || rows[0].Status.Text != "réplication" {
		t.Errorf("headless in French: %+v %+v", h, rows[0].Status)
	}
}

// The demo renders the plugin's samples: the former demo's shop database whose last backup
// failed, its healthy Galera cluster and its suspended one, and one cluster per other state.
func TestMariaDBPluginDemo(t *testing.T) {
	mariadbBuiltin(t)
	noKubeDial(t)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "mariadb", "", "")

	health := map[string]string{}
	for _, row := range rookCard(t, tree, "clusters").Rows {
		health[row.Title] = row.Health.Tone + " " + row.Health.Reason
	}

	want := map[string]string{
		"shop-db": "warning backupFailed", "forum-db": "ok ", "legacy-db": "idle suspended",
		"wiki-db": "warning galeraRecovery", "metrics-db": "critical noPrimary", "search-db": "critical noReady",
		"audit-db": "warning notReady", "queue-db": "warning notReady", "reports-db": "warning backupStale", "cart-db": "warning pods",
	}
	if fmt.Sprint(health) != fmt.Sprint(want) || len(tree.Errors) != 0 {
		t.Fatalf("demo rows %v, errors %+v", health, tree.Errors)
	}
}

// Data services no longer carry MariaDB: the plugin does.
func TestDataServicesHaveNoMariaDB(t *testing.T) {
	raw, err := json.Marshal(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(jsonKeys(raw), "mariadb") {
		t.Error("the Data services JSON still has a mariadb key")
	}
}
