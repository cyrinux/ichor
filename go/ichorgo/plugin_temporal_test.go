package ichorgo

import (
	"net/http"
	"testing"
	"time"
)

// The two Temporal built-in plugins over an httptest API. They share the temporal.io group:
// the operator serves v1beta1, the worker controller v1alpha1, and the group's preferred
// version is v1beta1 when both run, so each plugin must find its own resource at its own
// version.

// tpNow is the fixtures' clock.
var tpNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

const (
	tpOperator   = "/apis/temporal.io/v1beta1/"
	tpController = "/apis/temporal.io/v1alpha1/"

	tpOperatorResources = `{"resources":[{"name":"temporalclusters","namespaced":true,"kind":"TemporalCluster"},` +
		`{"name":"temporalnamespaces","namespaced":true,"kind":"TemporalNamespace"},` +
		`{"name":"temporalschedules","namespaced":true,"kind":"TemporalSchedule"},` +
		`{"name":"temporalclusterclients","namespaced":true,"kind":"TemporalClusterClient"}]}`
	tpControllerResources = `{"resources":[{"name":"workerdeployments","namespaced":true,"kind":"WorkerDeployment"},` +
		`{"name":"connections","namespaced":true,"kind":"Connection"},` +
		`{"name":"clusterconnections","namespaced":false,"kind":"ClusterConnection"},` +
		`{"name":"workerresourcetemplates","namespaced":true,"kind":"WorkerResourceTemplate"}]}`

	tpClusters = `{"items":[
	  {"metadata":{"name":"main","namespace":"shop"},"spec":{"version":"1.28.1","numHistoryShards":4},
	   "status":{"version":"1.28.1","services":[{"name":"frontend","ready":true},{"name":"history","ready":true}],
	   "conditions":[{"type":"Ready","status":"True"},{"type":"ReconcileSuccess","status":"True"}]}},
	  {"metadata":{"name":"down","namespace":"lab"},"spec":{"version":"1.28.1","numHistoryShards":4},
	   "status":{"version":"1.28.1","services":[{"name":"frontend","ready":false}],"conditions":[{"type":"Ready","status":"False","message":"frontend is not ready"}]}},
	  {"metadata":{"name":"partial","namespace":"lab"},"spec":{"version":"1.28.1","numHistoryShards":4},
	   "status":{"version":"1.28.1","services":[{"name":"frontend","ready":true},{"name":"matching","ready":false}],"conditions":[{"type":"Ready","status":"True"}]}},
	  {"metadata":{"name":"failing","namespace":"lab"},"spec":{"version":"1.28.1","numHistoryShards":4},
	   "status":{"version":"1.28.1","conditions":[{"type":"Ready","status":"True"},{"type":"ReconcileSuccess","status":"False","message":"store unreachable"}]}},
	  {"metadata":{"name":"upgrading","namespace":"shop"},"spec":{"version":"1.28.1","numHistoryShards":4},
	   "status":{"version":"1.27.3","conditions":[{"type":"Ready","status":"True"}]}},
	  {"metadata":{"name":"fresh","namespace":"shop"},"spec":{"version":"1.28.1","numHistoryShards":4}}]}`
	tpNamespaces = `{"items":[
	  {"metadata":{"name":"billing","namespace":"shop"},"spec":{"clusterRef":{"name":"main"},"retentionPeriod":"720h"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
	  {"metadata":{"name":"remote","namespace":"lab"},"spec":{"clusterRef":{"name":"main","namespace":"shop"},"retentionPeriod":"24h"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
	  {"metadata":{"name":"stuck","namespace":"lab"},"spec":{"clusterRef":{"name":"down"},"retentionPeriod":"24h"},"status":{"conditions":[{"type":"Ready","status":"False","message":"cluster down"}]}}]}`
	tpSchedules = `{"items":[
	  {"metadata":{"name":"hourly","namespace":"shop"},"spec":{"namespaceRef":{"name":"billing"},"schedule":{"action":{"workflow":{"type":"Sync","taskQueue":"q"}}}},
	   "status":{"conditions":[{"type":"Ready","status":"True"}]}},
	  {"metadata":{"name":"nightly","namespace":"shop"},"spec":{"namespaceRef":{"name":"billing"},"schedule":{"action":{"workflow":{"type":"Report","taskQueue":"q"}},"state":{"paused":true}}},
	   "status":{"conditions":[{"type":"Ready","status":"True"}]}},
	  {"metadata":{"name":"used-up","namespace":"shop"},"spec":{"namespaceRef":{"name":"billing"},"schedule":{"action":{"workflow":{"type":"Once","taskQueue":"q"}},"state":{"limitedActions":true,"remainingActions":0}}},
	   "status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`
	tpClients = `{"items":[
	  {"metadata":{"name":"app","namespace":"shop"},"spec":{"clusterRef":{"name":"main"}},"status":{"serverName":"frontend.main","secretRef":{"name":"app-mtls"}}}]}`

	tpWorkers = `{"items":[
	  {"metadata":{"name":"steady","namespace":"shop"},"spec":{"rollout":{"strategy":"AllAtOnce"},"sunset":{},"workerOptions":{"connectionRef":{"name":"prod"},"temporalNamespace":"billing"}},
	   "status":{"currentVersion":{"buildID":"b1"},"targetVersion":{"buildID":"b1","status":"Current"},
	   "conditions":[{"type":"Ready","status":"True"},{"type":"ConnectionHealthy","status":"True"}]}},
	  {"metadata":{"name":"ramping","namespace":"shop"},"spec":{"rollout":{"strategy":"Progressive"},"sunset":{},"workerOptions":{"connectionRef":{"name":"prod"},"temporalNamespace":"billing"}},
	   "status":{"currentVersion":{"buildID":"b1"},"targetVersion":{"buildID":"b2","status":"Ramping","rampPercentage":25},
	   "deprecatedVersions":[{"buildID":"b0","status":"Draining"},{"buildID":"a9","status":"Drained"}],
	   "conditions":[{"type":"Ready","status":"True"},{"type":"ConnectionHealthy","status":"True"}]}},
	  {"metadata":{"name":"stalled","namespace":"lab"},"spec":{"rollout":{"strategy":"Progressive"},"sunset":{},"workerOptions":{"connectionRef":{"name":"lab"},"temporalNamespace":"x"}},
	   "status":{"currentVersion":{"buildID":"c1"},"targetVersion":{"buildID":"c2","status":"Inactive"},
	   "conditions":[{"type":"Stalled","status":"True","message":"gate failed"},{"type":"ConnectionHealthy","status":"True"}]}},
	  {"metadata":{"name":"cut-off","namespace":"lab"},"spec":{"rollout":{"strategy":"Manual"},"sunset":{},"workerOptions":{"connectionRef":{"name":"lab"},"temporalNamespace":"x"}},
	   "status":{"conditions":[{"type":"ConnectionHealthy","status":"False","message":"certificate expired"}]}}]}`
	tpConnections        = `{"items":[{"metadata":{"name":"prod","namespace":"shop"},"spec":{"hostPort":"frontend.shop:7233","mutualTLSSecretRef":{"name":"prod-mtls"}}}]}`
	tpClusterConnections = `{"items":[{"metadata":{"name":"shared"},"spec":{"hostPort":"frontend.shared:7233","apiKeySecretRef":{"name":"key","key":"k"}}}]}`
	tpTemplates          = `{"items":[{"metadata":{"name":"hpa","namespace":"shop"},"spec":{"workerDeploymentRef":{"name":"ramping"},"template":{"kind":"HorizontalPodAutoscaler"}}}]}`
)

// tpAPI serves the operator's resources, the worker controller's, or both; with both the
// group prefers v1beta1, as the API server does.
func tpAPI(t *testing.T, operator, controller bool) *fakeKubeAPI {
	t.Helper()

	answers := map[string]string{}

	versions, preferred := []string{}, ""
	if operator {
		versions, preferred = append(versions, `{"groupVersion":"temporal.io/v1beta1","version":"v1beta1"}`), "v1beta1"
		answers["GET /apis/temporal.io/v1beta1"] = tpOperatorResources
		answers["GET "+tpOperator+"temporalclusters"] = tpClusters
		answers["GET "+tpOperator+"temporalnamespaces"] = tpNamespaces
		answers["GET "+tpOperator+"temporalschedules"] = tpSchedules
		answers["GET "+tpOperator+"temporalclusterclients"] = tpClients
	}

	if controller {
		versions = append(versions, `{"groupVersion":"temporal.io/v1alpha1","version":"v1alpha1"}`)
		if preferred == "" {
			preferred = "v1alpha1"
		}

		answers["GET /apis/temporal.io/v1alpha1"] = tpControllerResources
		answers["GET "+tpController+"workerdeployments"] = tpWorkers
		answers["GET "+tpController+"connections"] = tpConnections
		answers["GET "+tpController+"clusterconnections"] = tpClusterConnections
		answers["GET "+tpController+"workerresourcetemplates"] = tpTemplates
	}

	group := `{"name":"temporal.io","versions":[`
	for i, v := range versions {
		if i > 0 {
			group += ","
		}

		group += v
	}

	answers["GET /apis"] = `{"groups":[` + group + `],"preferredVersion":{"groupVersion":"temporal.io/` + preferred + `","version":"` + preferred + `"}}]}`

	f := newFakeKubeAPI(t, answers)
	useFakeKube(t, f)

	return f
}

// Each plugin is detected by its own resource at its own version: with both operators the
// group prefers v1beta1 and the worker controller still shows; with one of them only, the
// other plugin is inactive and reports nothing.
func TestTemporalPluginsDetectTheirOwnResource(t *testing.T) {
	for _, c := range []struct {
		name                         string
		operator, controller         bool
		wantOperator, wantController bool
	}{
		{"both", true, true, true, true},
		{"operator only", true, false, true, false},
		{"controller only", false, true, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			for id, want := range map[string]bool{"temporal-operator": c.wantOperator, "temporal-worker-controller": c.wantController} {
				useBuiltinPlugin(t, id, tpNow)
				tpAPI(t, c.operator, c.controller)

				tree := pluginData(t, "cfg", "ctx", "", id, "", "")
				if tree.Active != want || len(tree.Errors) != 0 {
					t.Errorf("%s: active %v (want %v), errors %+v", id, tree.Active, want, tree.Errors)
				}

				if want && tree.DetectedVia != "api" {
					t.Errorf("%s: detected via %q", id, tree.DetectedVia)
				}
			}
		})
	}
}

// Every cluster, namespace and schedule's health and reason, the namespace → cluster join
// (in the namespace's own namespace, or the one its clusterRef names), and the summary.
func TestTemporalOperatorRows(t *testing.T) {
	useBuiltinPlugin(t, "temporal-operator", tpNow)
	tpAPI(t, true, true)

	tree := pluginData(t, "cfg", "ctx", "", "temporal-operator", "", "")
	if tree.APIVersion != "v1beta1" {
		t.Errorf("api version %q", tree.APIVersion)
	}

	assertNFRows(t, tree, "clusters", map[string]nfWant{
		"shop/main":      {"ok", "", "2/2"},
		"lab/down":       {"critical", "notReady", "0/1"},
		"lab/partial":    {"warning", "servicesDown", "1/2"},
		"lab/failing":    {"warning", "reconcileFailed", "0/0"},
		"shop/upgrading": {"info", "upgrading", "0/0"},
		"shop/fresh":     {"unknown", "notReconciled", "0/0"},
	})

	clusters := esRowsByKey(t, tree, "clusters")
	if msg := clusters["lab/partial"].Health.Message; msg != "services not ready: matching" {
		t.Errorf("partial message %q", msg)
	}

	namespaces := esRowsByKey(t, tree, "namespaces")
	for key, want := range map[string]string{"shop/billing": "shop/main", "lab/remote": "shop/main", "lab/stuck": "lab/down"} {
		ref, _ := rowItems(namespaces[key], "relations")["Cluster"]["ref"].(map[string]any)
		if got := stringOf(ref["namespace"]) + "/" + stringOf(ref["name"]); got != want {
			t.Errorf("%s: cluster %q, want %q", key, got, want)
		}
	}

	if h := namespaces["lab/stuck"].Health; h == nil || h.Reason != "notReady" {
		t.Errorf("stuck health %+v", h)
	}

	schedules := esRowsByKey(t, tree, "schedules")
	for key, want := range map[string]string{"shop/hourly": "", "shop/nightly": "paused", "shop/used-up": "exhausted"} {
		if h := schedules[key].Health; h == nil || h.Reason != want {
			t.Errorf("%s: health %+v, want %q", key, h, want)
		}
	}

	badges := map[string]string{}
	for _, b := range tree.Summary.Badges {
		badges[b.Label] = b.Value
	}

	if tree.Summary.Title != "6 Temporal clusters" || badges["not ready"] != "1" || badges["paused schedules"] != "1" || tree.Summary.Health != "critical" {
		t.Errorf("summary %+v", tree.Summary)
	}
}

// Pause is on for an active schedule and Unpause for a paused one; each sends one merge patch
// of spec.schedule.state.paused, audited.
func TestTemporalSchedulePause(t *testing.T) {
	useBuiltinPlugin(t, "temporal-operator", tpNow)
	tpAPI(t, true, false)

	schedules := esRowsByKey(t, pluginData(t, "cfg", "ctx", "", "temporal-operator", "", ""), "schedules")
	for key, paused := range map[string]bool{"shop/hourly": false, "shop/nightly": true} {
		pause, unpause := forceSyncButton(t, schedules[key], "Pause"), forceSyncButton(t, schedules[key], "Unpause")
		if pause["enabled"] != !paused || unpause["enabled"] != paused || pause["confirm"] != "plain" {
			t.Errorf("%s: pause %+v, unpause %+v", key, pause, unpause)
		}
	}

	for _, c := range []struct {
		action, name string
		paused       bool
	}{{"pauseSchedule", "hourly", true}, {"unpauseSchedule", "nightly", false}} {
		t.Run(c.action, func(t *testing.T) {
			withDataDir(t)
			SetAuditEvents(false)
			useBuiltinPlugin(t, "temporal-operator", tpNow)

			path := tpOperator + "namespaces/shop/temporalschedules/" + c.name
			object := `{"metadata":{"name":"` + c.name + `","namespace":"shop","resourceVersion":"7"},` +
				`"spec":{"schedule":{"state":{"paused":` + boolText(!c.paused) + `}}}}`

			f := tpAPI(t, true, false)
			f.answerWith("GET "+path, http.StatusOK, object)
			f.answerWith("PATCH "+path, http.StatusOK, object)

			if err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "temporal-operator", c.action, "shop", c.name, ""); err != nil {
				t.Fatal(err)
			}

			want := `{"metadata":{"resourceVersion":"7"},"spec":{"schedule":{"state":{"paused":` + boolText(c.paused) + `}}}}`
			if got := mutation(t, f); got.path != path || got.contentType != "application/merge-patch+json" || got.body != want {
				t.Errorf("got %s %s %s\nwant %s", got.path, got.contentType, got.body, want)
			}

			if entries := readAudit(t, "", "plugin:temporal-operator:"+c.action); len(entries) != 1 || entries[0].Outcome != auditOK {
				t.Errorf("audit %+v", entries)
			}
		})
	}
}

// Every worker deployment's health and reason, the ramp, the draining builds, the template's
// worker, and the summary.
func TestTemporalWorkerControllerRows(t *testing.T) {
	useBuiltinPlugin(t, "temporal-worker-controller", tpNow)
	tpAPI(t, true, true)

	tree := pluginData(t, "cfg", "ctx", "", "temporal-worker-controller", "", "")
	if tree.APIVersion != "v1alpha1" {
		t.Errorf("api version %q", tree.APIVersion)
	}

	workers := assertNFRows(t, tree, "workers", map[string]nfWant{
		"shop/steady":  {"ok", "", "b1"},
		"shop/ramping": {"info", "ramping", "25%"},
		"lab/stalled":  {"critical", "rolloutStalled", "c1"},
		"lab/cut-off":  {"critical", "connectionUnhealthy", ""},
	})

	if got := rowItems(workers["shop/ramping"], "keyValues")["Draining builds"]["value"]; got != "b0" {
		t.Errorf("draining %v", got)
	}

	if got := relationName(esRowsByKey(t, tree, "templates")["shop/hpa"], "Worker deployment"); got != "ramping" {
		t.Errorf("template worker %q", got)
	}

	if auth := esRowsByKey(t, tree, "clusterConnections")["/shared"].Status.Text; auth != "API key" {
		t.Errorf("cluster connection auth %q", auth)
	}

	badges := map[string]string{}
	for _, b := range tree.Summary.Badges {
		badges[b.Label] = b.Value
	}

	if tree.Summary.Title != "4 worker deployments" || badges["stalled"] != "2" || badges["ramping"] != "1" || tree.Summary.Health != "critical" {
		t.Errorf("summary %+v", tree.Summary)
	}
}

func stringOf(v any) string {
	s, _ := v.(string)

	return s
}

func boolText(b bool) string {
	if b {
		return "true"
	}

	return "false"
}
