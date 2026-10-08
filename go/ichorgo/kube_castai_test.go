package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

// Shaped like the Workload Autoscaler's objects on a real cluster: one Recommendation per
// workload, the original requests in an annotation, VPA and HPA conditions.
const castAIFixture = `{"items":[
  {"metadata":{"name":"api-deployment","namespace":"shop","annotations":{
     "autoscaling.cast.ai/recommendation-apply-mode":"deferred",
     "autoscaling.cast.ai/first-seen-container-resources":"{\"api\":{\"requests\":{\"cpu\":\"500m\",\"memory\":\"1Gi\"},\"limits\":{\"cpu\":\"1\",\"memory\":\"1Gi\"}}}"}},
   "spec":{"targetRef":{"apiVersion":"apps/v1","kind":"Deployment","name":"api"},
     "recommendation":[{"containerName":"api","requests":{"cpu":"120m","memory":"640Mi"},"limits":{"cpu":"1","memory":"1Gi"}}]},
   "status":{"conditions":[{"type":"HPAHealthy","status":"True","message":"ok"},{"type":"VPAHealthy","status":"True","message":"ok"}]}},
  {"metadata":{"name":"worker-statefulset","namespace":"shop","annotations":{"autoscaling.cast.ai/recommendation-apply-mode":"immediate"}},
   "spec":{"targetRef":{"kind":"StatefulSet","name":"worker"},
     "recommendation":[{"containerName":"worker","requests":{"cpu":2,"memory":"3Gi"}}]},
   "status":{"conditions":[{"type":"VPAHealthy","status":"False","message":"webhook unreachable"}]}},
  {"metadata":{"name":"report-cronjob","namespace":"shop","annotations":{
     "autoscaling.cast.ai/recommendation-apply-mode":"deferred",
     "autoscaling.cast.ai/first-seen-container-resources":"{\"report\":{\"requests\":{\"cpu\":\"100m\",\"memory\":\"128Mi\"}}}"}},
   "spec":{"targetRef":{"kind":"CronJob","name":"report"},"applyPolicy":{"readonly":true,"reasons":[{"id":"ManagedByOther","message":"managed by another autoscaler"}]},
     "recommendation":[{"containerName":"report","requests":{"cpu":"50m","memory":"256Mi"}}]},
   "status":{"conditions":[{"type":"VPAHealthy","status":"True"}]}}]}`

func TestMapCastAI(t *testing.T) {
	var list kubeList[castAIObject]
	if err := json.Unmarshal([]byte(castAIFixture), &list); err != nil {
		t.Fatal(err)
	}

	out := mapCastAI(list.Items)

	if len(out.Recommendations) != 3 {
		t.Fatalf("recommendations: %+v", out.Recommendations)
	}

	// Worst first: the VPA that cannot sync, then the read-only one, then the healthy one.
	var names []string
	for _, r := range out.Recommendations {
		names = append(names, r.Name+":"+r.Health)
	}

	if want := []string{"worker-statefulset:critical", "report-cronjob:warning", "api-deployment:ok"}; !slices.Equal(names, want) {
		t.Fatalf("order: %v", names)
	}

	api := out.Recommendations[2]
	if api.Kind != "Deployment" || api.Workload != "api" || api.Mode != "deferred" || api.ReadOnly {
		t.Fatalf("api: %+v", api)
	}

	if c := api.Containers[0]; c.OriginalCPU != "500m" || c.CPU != "120m" || c.OriginalMemory != "1Gi" || c.Memory != "640Mi" || c.CPULimit != "1" {
		t.Fatalf("api container: %+v", c)
	}

	if api.CPUDeltaMilli != -380 || api.MemoryDeltaBytes != -384*1024*1024 {
		t.Fatalf("api deltas: %d %d", api.CPUDeltaMilli, api.MemoryDeltaBytes)
	}

	worker := out.Recommendations[0]
	if worker.Containers[0].CPU != "2" || worker.Containers[0].OriginalCPU != "" || worker.CPUDeltaMilli != 0 {
		t.Fatalf("worker (bare number, no original): %+v", worker.Containers[0])
	}

	if !slices.Equal(worker.Reasons, []string{castAIReasonVPA}) || worker.Message != "webhook unreachable" {
		t.Fatalf("worker reasons: %+v", worker)
	}

	report := out.Recommendations[1]
	if !report.ReadOnly || !slices.Equal(report.Reasons, []string{castAIReasonReadOnly}) || report.Message != "managed by another autoscaler" {
		t.Fatalf("report: %+v", report)
	}

	// The api and report workloads have originals: -380m-50m CPU, -384Mi+128Mi memory.
	if out.Compared != 2 || out.CPUDeltaMilli != -430 || out.MemoryDeltaBytes != -256*1024*1024 {
		t.Fatalf("totals: %+v", out)
	}
}

func TestReadDataServicesCastAI(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"autoscaling.cast.ai","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/autoscaling.cast.ai/v1/recommendations": castAIFixture,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints("castai"), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.CastAI == nil || res.CastAI.Error != "" || len(res.CastAI.Recommendations) != 3 || res.CastAI.Version != "v1" {
		t.Fatalf("castai: %+v", res.CastAI)
	}

	if res.Dragonfly != nil || res.Longhorn != nil {
		t.Errorf("only CAST AI is installed: %+v", res)
	}
}

func TestReadDataServicesCastAIForbidden(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"autoscaling.cast.ai","preferredVersion":{"version":"v1"}}]}`,
	})
	f.answerWith("GET /apis/autoscaling.cast.ai/v1/recommendations", 403, `{"kind":"Status","message":"recommendations is forbidden","reason":"Forbidden","code":403}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, nil, fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.CastAI == nil || res.CastAI.Error == "" || len(res.CastAI.Recommendations) != 0 {
		t.Fatalf("a forbidden listing is the section's error: %+v", res.CastAI)
	}
}

func TestCastAIDemoHasEveryState(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.CastAI == nil || len(demo.CastAI.Recommendations) < 3 {
		t.Fatal("demo misses CAST AI")
	}

	var states []string
	for _, r := range demo.CastAI.Recommendations {
		states = append(states, r.Health)
	}

	for _, want := range []string{healthOK, healthWarning, healthCritical} {
		if !slices.Contains(states, want) {
			t.Errorf("demo has no %s recommendation: %v", want, states)
		}
	}

	if demo.CastAI.CPUDeltaMilli >= 0 {
		t.Errorf("the demo should show a saving: %d", demo.CastAI.CPUDeltaMilli)
	}
}
