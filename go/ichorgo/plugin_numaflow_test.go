package ichorgo

import (
	"net/http"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Numaflow built-in plugin over an httptest API serving its four lists: one object per
// health rule, the buffer service each pipeline joins in its own namespace, the vertices
// counted per pipeline, and the pause and resume actions.

// nfNow is the fixtures' clock.
var nfNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

const (
	nfBase      = "/apis/numaflow.numaproj.io/v1alpha1/"
	nfGroups    = `{"groups":[{"name":"numaflow.numaproj.io","preferredVersion":{"version":"v1alpha1"}}]}`
	nfResources = `{"resources":[{"name":"pipelines","namespaced":true,"kind":"Pipeline"},` +
		`{"name":"vertices","namespaced":true,"kind":"Vertex"},{"name":"monovertices","namespaced":true,"kind":"MonoVertex"},` +
		`{"name":"interstepbufferservices","namespaced":true,"kind":"InterStepBufferService"}]}`

	nfPipelines = `{"items":[
	  {"metadata":{"name":"orders","namespace":"shop"},"spec":{"interStepBufferServiceName":"main"},
	   "status":{"phase":"Running","vertexCount":2,"conditions":[{"type":"VerticesHealthy","status":"True"},{"type":"DaemonServiceHealthy","status":"True"}]}},
	  {"metadata":{"name":"failing","namespace":"lab"},"status":{"phase":"Failed","message":"buffer service default is not ready"}},
	  {"metadata":{"name":"sick","namespace":"lab"},"status":{"phase":"Running","conditions":[{"type":"VerticesHealthy","status":"False","message":"vertex cat has 0 ready pods"}]}},
	  {"metadata":{"name":"no-daemon","namespace":"lab"},"status":{"phase":"Running","conditions":[{"type":"SideInputsManagersHealthy","status":"False"}]}},
	  {"metadata":{"name":"pausing","namespace":"lab"},"spec":{"lifecycle":{"desiredPhase":"Paused"}},"status":{"phase":"Pausing"}},
	  {"metadata":{"name":"paused","namespace":"shop"},"spec":{"lifecycle":{"desiredPhase":"Paused"}},"status":{"phase":"Paused"}},
	  {"metadata":{"name":"fresh","namespace":"shop"}}]}`
	nfMonoVertices = `{"items":[
	  {"metadata":{"name":"ok","namespace":"shop"},"status":{"phase":"Running","desiredReplicas":2,"readyReplicas":2,
	   "conditions":[{"type":"DaemonHealthy","status":"True"},{"type":"Deployed","status":"True"}]}},
	  {"metadata":{"name":"short","namespace":"shop"},"status":{"phase":"Running","desiredReplicas":3,"readyReplicas":1}},
	  {"metadata":{"name":"no-daemon","namespace":"shop"},"status":{"phase":"Running","conditions":[{"type":"DaemonHealthy","status":"False"}]}},
	  {"metadata":{"name":"crashing","namespace":"lab"},"status":{"phase":"Failed","message":"container udf keeps restarting"}},
	  {"metadata":{"name":"resting","namespace":"lab"},"spec":{"lifecycle":{"desiredPhase":"Paused"}},"status":{"phase":"Paused"}}]}`
	nfVertices = `{"items":[
	  {"metadata":{"name":"orders-in","namespace":"shop"},"spec":{"name":"in","pipelineName":"orders"},"status":{"phase":"Running","desiredReplicas":1,"readyReplicas":1}},
	  {"metadata":{"name":"orders-out","namespace":"shop"},"spec":{"name":"out","pipelineName":"orders"},"status":{"phase":"Running","desiredReplicas":2,"readyReplicas":1}},
	  {"metadata":{"name":"failing-in","namespace":"lab","labels":{"numaflow.numaproj.io/pipeline-name":"failing"}},"status":{"phase":"Failed","message":"cannot reach the buffer"}},
	  {"metadata":{"name":"paused-in","namespace":"shop"},"spec":{"name":"in","pipelineName":"paused"},"status":{"phase":"Paused"}}]}`
	nfBufferServices = `{"items":[
	  {"metadata":{"name":"main","namespace":"shop"},"spec":{"jetstream":{"version":"2.10","replicas":3}},
	   "status":{"type":"jetstream","phase":"Running","conditions":[{"type":"ChildrenResourcesHealthy","status":"True"}]}},
	  {"metadata":{"name":"main","namespace":"lab"},"status":{"type":"jetstream","phase":"Running"}},
	  {"metadata":{"name":"default","namespace":"lab"},"status":{"type":"jetstream","phase":"Failed","message":"0 ready replicas"}},
	  {"metadata":{"name":"new","namespace":"shop"},"status":{"type":"jetstream","phase":"Pending"}},
	  {"metadata":{"name":"degraded","namespace":"shop"},"status":{"type":"jetstream","phase":"Running","conditions":[{"type":"ChildrenResourcesHealthy","status":"False"}]}}]}`
)

// nfAPI serves the four lists; verticesStatus answers the vertex list (200: the fixture,
// 403: a kubeconfig that may not list vertices).
func nfAPI(t *testing.T, verticesStatus int) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": nfGroups,
		"GET /apis/numaflow.numaproj.io/v1alpha1":   nfResources,
		"GET " + nfBase + "pipelines":               nfPipelines,
		"GET " + nfBase + "monovertices":            nfMonoVertices,
		"GET " + nfBase + "vertices":                nfVertices,
		"GET " + nfBase + "interstepbufferservices": nfBufferServices,
	})

	if verticesStatus == http.StatusForbidden {
		f.answerWith("GET "+nfBase+"vertices", verticesStatus, forbiddenStatus)
	}

	useFakeKube(t, f)

	return f
}

// nfWant is one row's health tone and reason, and its trailing text.
type nfWant struct{ tone, reason, trailing string }

func assertNFRows(t *testing.T, tree plugin.Tree, card string, want map[string]nfWant) map[string]plugin.TreeRow {
	t.Helper()

	rows := esRowsByKey(t, tree, card)
	if len(rows) != len(want) {
		t.Fatalf("%s: %d rows, want %d", card, len(rows), len(want))
	}

	for key, w := range want {
		row := rows[key]
		if row.Health == nil || row.Health.Tone != w.tone || row.Health.Reason != w.reason {
			t.Errorf("%s %s: health %+v, want %s %q", card, key, row.Health, w.tone, w.reason)
		}

		if row.Trailing == nil || row.Trailing.Text != w.trailing {
			t.Errorf("%s %s: trailing %+v, want %q", card, key, row.Trailing, w.trailing)
		}
	}

	return rows
}

// Every object's health and reason, the vertices counted per pipeline, the buffer service
// joined in the pipeline's own namespace, and the summary.
func TestNumaflowPluginRows(t *testing.T) {
	useBuiltinPlugin(t, "numaflow", nfNow)
	nfAPI(t, http.StatusOK)

	tree := pluginData(t, "cfg", "ctx", "", "numaflow", "", "")
	if !tree.Active || tree.DetectedVia != "api" || tree.APIVersion != "v1alpha1" || len(tree.Errors) != 0 {
		t.Fatalf("tree active %v via %q, version %q, errors %+v", tree.Active, tree.DetectedVia, tree.APIVersion, tree.Errors)
	}

	pipelines := assertNFRows(t, tree, "pipelines", map[string]nfWant{
		"shop/orders":   {"ok", "", "2/2"},
		"lab/failing":   {"critical", "failed", "0/1"},
		"lab/sick":      {"warning", "verticesUnhealthy", "0/0"},
		"lab/no-daemon": {"warning", "daemonUnhealthy", "0/0"},
		"lab/pausing":   {"info", "transitioning", "0/0"},
		"shop/paused":   {"idle", "paused", "0/1"},
		"shop/fresh":    {"unknown", "notReconciled", "0/0"},
	})
	assertNFRows(t, tree, "monoVertices", map[string]nfWant{
		"shop/ok":        {"ok", "", "2/2"},
		"shop/short":     {"warning", "podsNotReady", "1/3"},
		"shop/no-daemon": {"warning", "daemonUnhealthy", "0/0"},
		"lab/crashing":   {"critical", "failed", "0/0"},
		"lab/resting":    {"idle", "paused", "0/0"},
	})
	assertNFRows(t, tree, "vertices", map[string]nfWant{
		"shop/orders-in":  {"ok", "", "1/1"},
		"shop/orders-out": {"warning", "podsNotReady", "1/2"},
		"lab/failing-in":  {"critical", "failed", "0/0"},
		"shop/paused-in":  {"idle", "paused", "0/0"},
	})
	assertNFRows(t, tree, "bufferServices", map[string]nfWant{
		"shop/main":     {"ok", "", "jetstream"},
		"lab/main":      {"ok", "", "jetstream"},
		"lab/default":   {"critical", "isbsvcFailed", "jetstream"},
		"shop/new":      {"info", "pending", "jetstream"},
		"shop/degraded": {"warning", "childrenUnhealthy", "jetstream"},
	})

	if msg := pipelines["lab/failing"].Health.Message; msg != "buffer service default is not ready" {
		t.Errorf("failing message %q", msg)
	}

	// orders names its buffer service; failing defaults to "default", in lab, not shop's.
	for key, want := range map[string]string{"shop/orders": "shop/main", "lab/failing": "lab/default", "shop/fresh": ""} {
		ref, _ := rowItems(pipelines[key], "relations")["Buffer service"]["ref"].(map[string]any)
		got := ""
		if ref != nil {
			got = ref["namespace"].(string) + "/" + ref["name"].(string)
		}

		if got != want {
			t.Errorf("%s: buffer service %q, want %q", key, got, want)
		}
	}

	badges := map[string]string{}
	for _, b := range tree.Summary.Badges {
		badges[b.Label] = b.Value
	}

	if tree.Summary.Title != "12 pipelines" || badges["failed"] != "2" || badges["paused"] != "2" ||
		badges["buffer down"] != "1" || tree.Summary.Health != "critical" {
		t.Errorf("summary %+v", tree.Summary)
	}
}

// Pause is on while a pipeline or mono vertex is wanted running, Resume while it is wanted
// paused; the other one is off with its reason.
func TestNumaflowPauseResumeButtons(t *testing.T) {
	useBuiltinPlugin(t, "numaflow", nfNow)
	nfAPI(t, http.StatusOK)

	tree := pluginData(t, "cfg", "ctx", "", "numaflow", "", "")

	for _, c := range []struct{ card, key, pause, resume string }{
		{"pipelines", "shop/orders", "pausePipeline", "resumePipeline"},
		{"pipelines", "shop/paused", "pausePipeline", "resumePipeline"},
		{"pipelines", "lab/pausing", "pausePipeline", "resumePipeline"},
		{"monoVertices", "shop/ok", "pauseMonoVertex", "resumeMonoVertex"},
		{"monoVertices", "lab/resting", "pauseMonoVertex", "resumeMonoVertex"},
	} {
		row := esRowsByKey(t, tree, c.card)[c.key]
		running := c.key == "shop/orders" || c.key == "shop/ok"

		pause, resume := forceSyncButton(t, row, "Pause"), forceSyncButton(t, row, "Resume")
		if pause["id"] != c.pause || pause["enabled"] != running || resume["id"] != c.resume || resume["enabled"] != !running {
			t.Errorf("%s: pause %+v, resume %+v", c.key, pause, resume)
		}

		off, reason := resume, "already running"
		if !running {
			off, reason = pause, "already paused"
		}

		if off["disabledReason"] != reason || off["confirm"] != "plain" {
			t.Errorf("%s: off button %+v, want %q", c.key, off, reason)
		}
	}
}

// Pause and Resume send one merge patch of spec.lifecycle.desiredPhase, and the audit log
// records it under the plugin's action.
func TestNumaflowPauseResumePatch(t *testing.T) {
	for _, c := range []struct{ action, resource, name, phase, current string }{
		{"pausePipeline", "pipelines", "orders", "Paused", "Running"},
		{"resumePipeline", "pipelines", "orders", "Running", "Paused"},
		{"pauseMonoVertex", "monovertices", "ok", "Paused", "Running"},
		{"resumeMonoVertex", "monovertices", "ok", "Running", "Paused"},
	} {
		t.Run(c.action, func(t *testing.T) {
			withDataDir(t)
			SetAuditEventClusters("")
			useBuiltinPlugin(t, "numaflow", nfNow)

			path := nfBase + "namespaces/shop/" + c.resource + "/" + c.name
			object := `{"metadata":{"name":"` + c.name + `","namespace":"shop","resourceVersion":"42"},` +
				`"spec":{"lifecycle":{"desiredPhase":"` + c.current + `"}}}`

			f := nfAPI(t, http.StatusOK)
			f.answerWith("GET "+path, http.StatusOK, object)
			f.answerWith("PATCH "+path, http.StatusOK, object)

			if err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "numaflow", c.action, "shop", c.name, ""); err != nil {
				t.Fatal(err)
			}

			want := `{"metadata":{"resourceVersion":"42"},"spec":{"lifecycle":{"desiredPhase":"` + c.phase + `"}}}`
			if got := mutation(t, f); got.method != http.MethodPatch || got.path != path ||
				got.contentType != "application/merge-patch+json" || got.body != want {
				t.Errorf("got %s %s %s\n%s\nwant %s", got.method, got.path, got.contentType, got.body, want)
			}

			if entries := readAudit(t, "", "plugin:numaflow:"+c.action); len(entries) != 1 || entries[0].Outcome != auditOK {
				t.Errorf("audit %+v", entries)
			}
		})
	}
}

// A kubeconfig that may not list vertices still shows the rest; the vertex list lands in the
// tree's errors and the pipelines count no vertex.
func TestNumaflowVerticesForbidden(t *testing.T) {
	useBuiltinPlugin(t, "numaflow", nfNow)
	nfAPI(t, http.StatusForbidden)

	tree := pluginData(t, "cfg", "ctx", "", "numaflow", "", "")
	if !tree.Active || len(tree.Errors) != 1 || tree.Errors[0].Resource != "vertices.numaflow.numaproj.io" {
		t.Fatalf("active %v, errors %+v", tree.Active, tree.Errors)
	}

	if row := esRowsByKey(t, tree, "pipelines")["shop/orders"]; row.Health == nil || row.Health.Tone != "ok" || row.Trailing.Text != "0/0" {
		t.Errorf("orders %+v %+v", row.Health, row.Trailing)
	}
}
