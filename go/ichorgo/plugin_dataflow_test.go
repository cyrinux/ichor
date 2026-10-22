package ichorgo

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Dataflow built-in plugin over Config Connector's DataflowJob and DataflowFlexTemplateJob,
// as an API server serves them: each job's health and reason from its JobState and its Ready
// condition, the summary's counts, the findings, and no section without Config Connector.

const (
	dataflowBase   = "GET /apis/dataflow.cnrm.cloud.google.com/v1beta1"
	dataflowGroups = `{"groups":[{"name":"dataflow.cnrm.cloud.google.com","preferredVersion":{"version":"v1beta1"}}]}`
)

func dataflowRoutes() map[string]string {
	return map[string]string{
		"GET /apis":                    dataflowGroups,
		dataflowBase:                   `{"resources":[{"name":"dataflowjobs","namespaced":true,"kind":"DataflowJob"},{"name":"dataflowflextemplatejobs","namespaced":true,"kind":"DataflowFlexTemplateJob"}]}`,
		dataflowBase + "/dataflowjobs": `{"items":[]}`,
		dataflowBase + "/dataflowflextemplatejobs": `{"items":[]}`,
	}
}

// dataflowJob is one job as the API serves it; state "" leaves the status out (no reconcile
// yet), ready "" leaves the Ready condition out.
func dataflowJob(t *testing.T, name, state, jobType, ready, reason, message string) string {
	t.Helper()

	obj := map[string]any{
		"metadata": map[string]any{"name": name, "namespace": "pipelines", "generation": 1, "creationTimestamp": "2026-10-01T00:00:00Z"},
		"spec":     map[string]any{"region": "europe-west1", "templateGcsPath": "gs://bucket/templates/t", "containerSpecGcsPath": "gs://bucket/flex/t.json", "maxWorkers": 4, "numWorkers": 2},
	}

	status := map[string]any{"observedGeneration": 1}
	if state != "" {
		status["state"], status["type"], status["jobId"] = state, jobType, "2026-10-01_00_00_00-1"
	}

	if ready != "" {
		status["conditions"] = []map[string]string{{"type": "Ready", "status": ready, "reason": reason, "message": message, "lastTransitionTime": "2026-10-01T00:00:00Z"}}
	}

	obj["status"] = status

	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// dataflowTree renders the plugin, alone and at fixtureNow, over routes.
func dataflowTree(t *testing.T, routes map[string]string) plugin.Tree {
	t.Helper()

	useBuiltinPlugin(t, "dataflow", fixtureNow)
	useFakeKube(t, newFakeKubeAPI(t, routes))

	return pluginData(t, "cfg", "ctx", "", "dataflow", "", "")
}

// Each JobState and Ready condition gives one health and reason, the same for both kinds.
func TestDataflowPluginHealth(t *testing.T) {
	cases := []struct {
		name, state, jobType, ready, reason, status, health, why, trailing string
	}{
		{"running", "JOB_STATE_RUNNING", "JOB_TYPE_STREAMING", "True", "UpToDate", "Running", healthOK, "", "streaming"},
		// Failed wins over a Ready condition that still says up to date.
		{"failed", "JOB_STATE_FAILED", "JOB_TYPE_BATCH", "True", "UpToDate", "Failed", healthCritical, "failed", "batch"},
		{"failed-reconcile", "JOB_STATE_FAILED", "JOB_TYPE_BATCH", "False", "UpdateFailed", "Failed", healthCritical, "failed", "batch"},
		{"dependency", "", "", "False", "DependencyNotFound", "Unknown", healthWarning, "not-ready", ""},
		{"conflict", "JOB_STATE_RUNNING", "JOB_TYPE_STREAMING", "False", "ManagementConflict", "Running", healthWarning, "not-ready", "streaming"},
		{"pending", "JOB_STATE_PENDING", "JOB_TYPE_BATCH", "True", "UpToDate", "Pending", "info", "transitioning", "batch"},
		{"queued", "JOB_STATE_QUEUED", "JOB_TYPE_BATCH", "True", "UpToDate", "Queued", "info", "transitioning", "batch"},
		{"draining", "JOB_STATE_DRAINING", "JOB_TYPE_STREAMING", "True", "UpToDate", "Draining", "info", "transitioning", "streaming"},
		{"cancelling", "JOB_STATE_CANCELLING", "JOB_TYPE_STREAMING", "True", "UpToDate", "Cancelling", "info", "transitioning", "streaming"},
		{"updated", "JOB_STATE_UPDATED", "JOB_TYPE_STREAMING", "True", "UpToDate", "Updated", "info", "transitioning", "streaming"},
		{"done", "JOB_STATE_DONE", "JOB_TYPE_BATCH", "True", "UpToDate", "Done", healthIdle, "finished", "batch"},
		{"cancelled", "JOB_STATE_CANCELLED", "JOB_TYPE_BATCH", "True", "UpToDate", "Cancelled", healthIdle, "finished", "batch"},
		{"drained", "JOB_STATE_DRAINED", "JOB_TYPE_STREAMING", "True", "UpToDate", "Drained", healthIdle, "finished", "streaming"},
		{"stopped", "JOB_STATE_STOPPED", "JOB_TYPE_BATCH", "True", "UpToDate", "Stopped", healthIdle, "finished", "batch"},
		{"state-unknown", "JOB_STATE_UNKNOWN", "JOB_TYPE_BATCH", "True", "UpToDate", "Unknown", "unknown", "unknown", "batch"},
		// Before the first reconcile: no status at all.
		{"fresh", "", "", "", "", "Unknown", "unknown", "unknown", ""},
	}

	items := make([]string, 0, len(cases))
	for _, tt := range cases {
		items = append(items, dataflowJob(t, tt.name, tt.state, tt.jobType, tt.ready, tt.reason, "controller detail"))
	}

	list := `{"items":[` + strings.Join(items, ",") + `]}`
	routes := dataflowRoutes()
	routes[dataflowBase+"/dataflowjobs"] = list
	routes[dataflowBase+"/dataflowflextemplatejobs"] = list
	tree := dataflowTree(t, routes)

	for _, card := range []string{"jobs", "flexJobs"} {
		rows := map[string]plugin.TreeRow{}
		for _, row := range rookCard(t, tree, card).Rows {
			rows[row.Title] = row
		}

		for _, tt := range cases {
			row := rows[tt.name]
			if row.Health == nil || row.Health.Tone != tt.health || row.Health.Reason != tt.why {
				t.Errorf("%s %s: health %+v, want %s %q", card, tt.name, row.Health, tt.health, tt.why)
			}

			if row.Status == nil || row.Status.Text != tt.status {
				t.Errorf("%s %s: status %+v, want %s", card, tt.name, row.Status, tt.status)
			}

			if got := trailingText(row); got != tt.trailing {
				t.Errorf("%s %s: trailing %q, want %q", card, tt.name, got, tt.trailing)
			}

			// An empty value is left out of the row.
			state, _ := rowItems(row, "keyValues")["State"]["value"].(string)
			if state != tt.state {
				t.Errorf("%s %s: State %q, want %q", card, tt.name, state, tt.state)
			}
		}
	}
}

func trailingText(row plugin.TreeRow) string {
	if row.Trailing == nil {
		return ""
	}

	return row.Trailing.Text
}

// The summary counts both kinds; failed and not-reconciled jobs are findings, with the
// reconcile message only when Ready is False.
func TestDataflowPluginSummaryAndFindings(t *testing.T) {
	routes := dataflowRoutes()
	routes[dataflowBase+"/dataflowjobs"] = `{"items":[` + strings.Join([]string{
		dataflowJob(t, "enrich", "JOB_STATE_RUNNING", "JOB_TYPE_STREAMING", "True", "UpToDate", "The resource is up to date"),
		dataflowJob(t, "export", "JOB_STATE_FAILED", "JOB_TYPE_BATCH", "False", "UpdateFailed", "the job ended in JOB_STATE_FAILED"),
		dataflowJob(t, "backfill", "JOB_STATE_DRAINED", "JOB_TYPE_STREAMING", "True", "UpToDate", "The resource is up to date"),
	}, ",") + `]}`
	routes[dataflowBase+"/dataflowflextemplatejobs"] = `{"items":[` + strings.Join([]string{
		dataflowJob(t, "score", "JOB_STATE_RUNNING", "JOB_TYPE_STREAMING", "True", "UpToDate", "The resource is up to date"),
		dataflowJob(t, "features", "JOB_STATE_FAILED", "JOB_TYPE_BATCH", "True", "UpToDate", "The resource is up to date"),
		dataflowJob(t, "tiles", "", "", "False", "DependencyNotReady", "reference StorageBucket pipelines/tiles is not ready"),
	}, ",") + `]}`

	tree := dataflowTree(t, routes)
	if !tree.Active || tree.DetectedVia != "api" || len(tree.Errors) != 0 {
		t.Fatalf("active %v via %q, errors %+v", tree.Active, tree.DetectedVia, tree.Errors)
	}

	if tree.Summary.Title != "6 Dataflow jobs" || tree.Summary.Health != healthCritical {
		t.Errorf("summary %+v", tree.Summary)
	}

	badges := map[string]string{}
	for _, b := range tree.Summary.Badges {
		badges[b.Label] = b.Value + " " + b.Tone
	}

	if want := map[string]string{"failed": "2 critical", "not ready": "1 warning", "running": "2 info"}; !maps.Equal(badges, want) {
		t.Errorf("badges %v, want %v", badges, want)
	}

	want := []string{
		"critical failed pipelines/export",
		"critical flex-failed pipelines/features",
		"warning flex-not-ready pipelines/tiles",
	}
	if got := findingsOf(tree); !slices.Equal(got, want) {
		t.Errorf("findings %v, want %v", got, want)
	}

	details := map[string]string{}
	for _, f := range tree.Findings {
		details[f.ID] = f.Detail
	}

	if want := map[string]string{
		"failed":         "job failed: the job ended in JOB_STATE_FAILED",
		"flex-failed":    "job failed",
		"flex-not-ready": "not reconciled: DependencyNotReady: reference StorageBucket pipelines/tiles is not ready",
	}; !maps.Equal(details, want) {
		t.Errorf("finding details %v, want %v", details, want)
	}
}

// No Config Connector, or Config Connector without its Dataflow kinds: no section.
func TestDataflowPluginInactiveWithoutConfigConnector(t *testing.T) {
	for _, tt := range []struct{ name, route, body string }{
		{"no group", "GET /apis", `{"groups":[]}`},
		{"other kinds", dataflowBase, `{"resources":[{"name":"other","namespaced":true,"kind":"Other"}]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := dataflowRoutes()
			routes[tt.route] = tt.body

			if tree := dataflowTree(t, routes); tree.Active {
				t.Fatalf("active without Dataflow: %+v", tree.Summary)
			}
		})
	}
}

// A Flex job's details: its workers, container and image; a spec change not reconciled yet
// says so.
func TestDataflowPluginFlexDetails(t *testing.T) {
	job := map[string]any{
		"metadata": map[string]any{"name": "score", "namespace": "pipelines", "generation": 3},
		"spec": map[string]any{"region": "us-central1", "containerSpecGcsPath": "gs://bucket/flex/score.json", "numWorkers": 2, "maxWorkers": 10,
			"sdkContainerImage": "registry.example/beam/score:1.0", "autoscalingAlgorithm": "AUTOSCALING_ALGORITHM_BASIC", "enableStreamingEngine": true},
		"status": map[string]any{"state": "JOB_STATE_RUNNING", "type": "JOB_TYPE_STREAMING", "jobId": "job-1", "observedGeneration": 2},
	}

	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}

	routes := dataflowRoutes()
	routes[dataflowBase+"/dataflowflextemplatejobs"] = `{"items":[` + string(raw) + `]}`
	row := rookCard(t, dataflowTree(t, routes), "flexJobs").Rows[0]

	kv := rowItems(row, "keyValues")
	for label, want := range map[string]string{
		"Workers": "2 / 10", "Container": "gs://bucket/flex/score.json", "SDK image": "registry.example/beam/score:1.0",
		"Autoscaling": "AUTOSCALING_ALGORITHM_BASIC", "Job ID": "job-1", "Region": "us-central1",
	} {
		if got := kv[label]["value"]; got != want {
			t.Errorf("%s = %v, want %s", label, got, want)
		}
	}

	badges := rowItems(row, "badges")
	if badges["Streaming Engine"]["tone"] != healthOK || badges["Last change applied"]["tone"] != healthIdle {
		t.Errorf("badges %v", badges)
	}
}
