package ichorgo

import (
	"context"
	"testing"
)

// The autoscaler chip on the workload rows: the HPA (or the KEDA ScaledObject behind one)
// joined by (kind, namespace, name), and a failed autoscaler read taking only the chips.
func TestWorkloadAutoscalerChip(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"keda.sh","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/apps/v1/deployments": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{"replicas":3},"status":{"readyReplicas":3}},
			{"metadata":{"name":"worker","namespace":"shop"},"spec":{"replicas":2},"status":{"readyReplicas":1}},
			{"metadata":{"name":"plain","namespace":"shop"},"spec":{"replicas":1},"status":{"readyReplicas":1}}]}`,
		"GET /apis/apps/v1/statefulsets": `{"items":[]}`,
		"GET /apis/apps/v1/daemonsets":   `{"items":[]}`,
		"GET /apis/autoscaling/v2/horizontalpodautoscalers": `{"items":[
			{"metadata":{"name":"web-hpa","namespace":"shop"},"spec":{"scaleTargetRef":{"kind":"Deployment","name":"web"},"minReplicas":1,"maxReplicas":5},"status":{"currentReplicas":3,"desiredReplicas":3}},
			{"metadata":{"name":"keda-hpa-worker","namespace":"shop","ownerReferences":[{"kind":"ScaledObject","name":"worker-so"}]},"spec":{"scaleTargetRef":{"kind":"Deployment","name":"worker"},"maxReplicas":10},"status":{"currentReplicas":2,"desiredReplicas":2}}]}`,
		"GET /apis/keda.sh/v1alpha1/scaledobjects": `{"items":[
			{"metadata":{"name":"worker-so","namespace":"shop","annotations":{"autoscaling.keda.sh/paused-replicas":"0"}},"spec":{"scaleTargetRef":{"kind":"Deployment","name":"worker"},"maxReplicaCount":10},"status":{"hpaName":"keda-hpa-worker"}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := listWorkloads(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]kubeWorkload{}
	for _, w := range list.Workloads {
		byName[w.Name] = w
	}

	// The HPA-driven workload: its chip names the HPA with its bounds.
	web, ok := byName["web"]
	if !ok || web.Autoscaler == nil {
		t.Fatalf("web: %+v", web)
	}
	if web.Autoscaler.By != "hpa" || web.Autoscaler.Name != "web-hpa" || web.Autoscaler.Min != 1 || web.Autoscaler.Max != 5 || web.Autoscaler.Current != 3 {
		t.Errorf("web autoscaler %+v", web.Autoscaler)
	}

	// The KEDA-driven one: one chip, by keda, naming the ScaledObject, paused.
	worker, ok := byName["worker"]
	if !ok || worker.Autoscaler == nil {
		t.Fatalf("worker: %+v", worker)
	}
	if worker.Autoscaler.By != "keda" || worker.Autoscaler.Name != "worker-so" || !worker.Autoscaler.Paused {
		t.Errorf("worker autoscaler %+v", worker.Autoscaler)
	}

	// A workload nothing scales: no chip.
	if plain := byName["plain"]; plain.Autoscaler != nil {
		t.Errorf("plain autoscaler %+v", plain.Autoscaler)
	}
}

func TestWorkloadAutoscalerReadFailsQuietly(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[]}`,
		"GET /apis/apps/v1/deployments": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{"replicas":3},"status":{"readyReplicas":3}}]}`,
		"GET /apis/apps/v1/statefulsets": `{"items":[]}`,
		"GET /apis/apps/v1/daemonsets":   `{"items":[]}`,
	})
	f.answerWith("GET /apis/autoscaling/v2/horizontalpodautoscalers", 403,
		`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"forbidden","code":403}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := listWorkloads(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.Workloads) != 1 || list.Workloads[0].Autoscaler != nil {
		t.Fatalf("workloads %+v", list.Workloads)
	}
}
