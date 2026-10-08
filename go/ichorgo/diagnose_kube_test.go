package ichorgo

import (
	"strings"
	"testing"
	"time"
)

const kubeDiagnosisNodesJSON = `{"items":[
 {"metadata":{"name":"master-a","labels":{"node-role.kubernetes.io/control-plane":""},"creationTimestamp":"2026-01-01T00:00:00Z"},
  "status":{"allocatable":{"cpu":"4","memory":"8Gi","pods":"110"},"addresses":[{"type":"InternalIP","address":"10.0.0.5"}],
   "conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.34.1","osImage":"Ubuntu 24.04","kernelVersion":"6.8.0","containerRuntimeVersion":"containerd://2.0.0","architecture":"amd64"}}},
 {"metadata":{"name":"node-b","creationTimestamp":"2026-01-01T00:00:00Z"},"spec":{"unschedulable":true},
  "status":{"allocatable":{"cpu":"8","memory":"16Gi","pods":"110"},"addresses":[{"type":"InternalIP","address":"10.0.0.6"}],
   "conditions":[{"type":"Ready","status":"False","reason":"KubeletNotReady"},{"type":"DiskPressure","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.34.1"}}}]}`

const kubeDiagnosisPodsJSON = `{"items":[
 {"metadata":{"name":"crasher-7d9c-x1","namespace":"shop","creationTimestamp":"2026-10-08T09:00:00Z","ownerReferences":[{"kind":"ReplicaSet","name":"crasher-7d9c","controller":true}]},
  "spec":{"nodeName":"node-b","containers":[{"name":"app","image":"shop/app:1.2"}]},
  "status":{"phase":"Running","containerStatuses":[{"name":"app","ready":false,"restartCount":12,"state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"reason":"Error","exitCode":1}}}]}},
 {"metadata":{"name":"fine-1","namespace":"shop","creationTimestamp":"2026-10-08T09:00:00Z"},
  "spec":{"nodeName":"master-a","containers":[{"name":"c","image":"shop/fine:1"}]},
  "status":{"phase":"Running","containerStatuses":[{"name":"c","ready":true,"state":{"running":{}}}]}}]}`

// kubeDiagnosisEventsJSON holds one Warning event of the last minutes and one of yesterday.
func kubeDiagnosisEventsJSON(now time.Time) string {
	recent, old := now.Add(-2*time.Minute).UTC().Format(time.RFC3339), now.Add(-30*time.Hour).UTC().Format(time.RFC3339)

	return `{"items":[
 {"metadata":{"name":"e1","namespace":"shop","creationTimestamp":"` + recent + `"},"involvedObject":{"kind":"Pod","namespace":"shop","name":"crasher-7d9c-x1"},
  "type":"Warning","reason":"BackOff","message":"Back-off restarting failed container app","count":40,"lastTimestamp":"` + recent + `"},
 {"metadata":{"name":"e2","namespace":"shop","creationTimestamp":"` + old + `"},"involvedObject":{"kind":"Pod","namespace":"shop","name":"old-1"},
  "type":"Warning","reason":"FailedMount","message":"yesterday","count":1,"lastTimestamp":"` + old + `"}]}`
}

func kubeDiagnosisAPI(t *testing.T) *fakeKubeAPI {
	t.Helper()

	return newFakeKubeAPI(t, map[string]string{
		"GET /version":       `{"gitVersion":"v1.34.1"}`,
		"GET /api/v1/nodes":  kubeDiagnosisNodesJSON,
		"GET /api/v1/pods":   kubeDiagnosisPodsJSON,
		"GET /api/v1/events": kubeDiagnosisEventsJSON(time.Now()),
		"GET /apis":          `{"groups":[]}`,
	})
}

// A cluster added from a kubeconfig gets a report from the Kubernetes API alone, end to end
// through the stored kubeconfig, with the names masked when asked.
func TestCollectDiagnosisFromKubeconfig(t *testing.T) {
	f := kubeDiagnosisAPI(t)

	stored, err := MergeKubeconfig("", "", strings.Replace(f.kubeconfigFor(f.URL), "server: https://other.invalid:6443", "server: "+f.URL, 1), "")
	if err != nil {
		t.Fatal(err)
	}

	d, err := CollectDiagnosis(stored, "admin@test", "", false)
	if err != nil {
		t.Fatal(err)
	}

	report := d.Report()

	for _, want := range []string{
		"Kubernetes cluster report", "API server v1.34.1",
		"NODES (2: 1 ready, 1 not ready, 1 cordoned)",
		"- master-a [10.0.0.5]: control-plane, Ready", "kubelet v1.34.1; os Ubuntu 24.04; kernel 6.8.0",
		"- node-b [10.0.0.6]: worker, NOT READY, cordoned (unschedulable), pressure: DiskPressure",
		"PODS NOT HEALTHY (1)", "shop/crasher-7d9c-x1: CrashLoopBackOff, ready 0/1, 12 restarts, on node-b, owned by ReplicaSet/crasher-7d9c",
		"WARNING EVENTS, LAST HOUR (1, newest first)", "Pod shop/crasher-7d9c-x1 BackOff (x40)",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}

	for _, unwanted := range []string{"Talos cluster report", "ETCD", "fine-1", "yesterday", "secret-token"} {
		if strings.Contains(report, unwanted) {
			t.Errorf("report holds %q:\n%s", unwanted, report)
		}
	}

	if d.Anonymized() {
		t.Error("anonymized without being asked")
	}

	prompt := d.Prompt("en", "")
	if !strings.Contains(prompt, "through the Kubernetes API only") || strings.Contains(prompt, "talosctl or kubectl command") {
		t.Errorf("prompt:\n%s", prompt)
	}

	masked, err := CollectDiagnosis(stored, "admin@test", "", true)
	if err != nil {
		t.Fatal(err)
	}

	if r := masked.Report(); strings.Contains(r, "master-a") || strings.Contains(r, "10.0.0.5") || strings.Contains(r, "node-b") || !strings.Contains(r, "cp-1") {
		t.Errorf("masked report:\n%s", r)
	}
}

// Credentials that may not list nodes still get a report.
func TestReadKubeDiagnosisForbiddenNodes(t *testing.T) {
	f := kubeDiagnosisAPI(t)
	f.answerWith("GET /api/v1/nodes", 403, `{"kind":"Status","reason":"Forbidden","message":"nodes is forbidden"}`)
	useFakeKube(t, f)

	data, err := collectKubeDiagnosis(kubeTarget{"cfg", "ctx", ""})
	if err != nil {
		t.Fatal(err)
	}

	report := renderDiagnosis(data)
	if !strings.Contains(report, "NODES\n  not read: the credentials may not list nodes") || !strings.Contains(report, "PODS NOT HEALTHY (1)") {
		t.Fatalf("report:\n%s", report)
	}
}

// Nothing wrong: every section says so rather than disappearing.
func TestRenderKubeDiagnosisEmpty(t *testing.T) {
	report := renderDiagnosis(diagnosisData{At: time.Unix(1_700_000_000, 0), Kube: &kubeDiagnosis{ServerVersion: "v1.34.1", Nodes: []kubeNodeInfo{}}})

	for _, want := range []string{"NODES (0: 0 ready, 0 not ready, 0 cordoned)", "PODS NOT HEALTHY (0)\n  none", "WARNING EVENTS, LAST HOUR (0, newest first)\n  none"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}

	if !strings.HasSuffix(report, "\n") || strings.Contains(report, "GITOPS") {
		t.Errorf("report:\n%s", report)
	}
}
