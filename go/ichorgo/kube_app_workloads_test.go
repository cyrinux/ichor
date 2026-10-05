package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func ownedPod(name, ownerKind, ownerName string) string {
	owners := ""
	if ownerKind != "" {
		owners = `,"ownerReferences":[{"kind":"` + ownerKind + `","name":"` + ownerName + `"}]`
	}

	return `{"metadata":{"name":"` + name + `","namespace":"shop"` + owners + `},"spec":{"containers":[{"name":"c","image":"x"}]},"status":{"phase":"Running"}}`
}

const appWorkloadAnswers = `{"metadata":{"name":"%s","namespace":"shop"},"spec":{"replicas":2},"status":{"readyReplicas":2}}`

func workloadNames(list kubeWorkloadList) string {
	names := make([]string, 0, len(list.Workloads))
	for _, w := range list.Workloads {
		names = append(names, w.Kind+"/"+w.Name)
	}

	return strings.Join(names, ",")
}

func TestAppWorkloadsReadsOnlyWhatThePodsLeadTo(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/pods/web-1":            ownedPod("web-1", "ReplicaSet", "web-5d8f"),
		"GET /api/v1/namespaces/shop/pods/web-2":            ownedPod("web-2", "ReplicaSet", "web-5d8f"),
		"GET /api/v1/namespaces/shop/pods/db-0":             ownedPod("db-0", "StatefulSet", "db"),
		"GET /api/v1/namespaces/shop/pods/static":           ownedPod("static", "Node", "w1"),
		"GET /api/v1/namespaces/shop/pods/job-1":            ownedPod("job-1", "ReplicaSet", "orphan-77"),
		"GET /apis/apps/v1/namespaces/shop/deployments/web": fmt.Sprintf(appWorkloadAnswers, "web"),
		"GET /apis/apps/v1/namespaces/shop/statefulsets/db": fmt.Sprintf(appWorkloadAnswers, "db"),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// gone: deleted since the inventory; orphan: a ReplicaSet without its Deployment (404).
	pods := []routePod{{"shop", "web-1"}, {"shop", "web-2"}, {"shop", "db-0"}, {"shop", "static"}, {"shop", "gone"}, {"shop", "job-1"}, {"../x", "y"}}

	list, err := appWorkloads(context.Background(), k, pods)
	if err != nil {
		t.Fatal(err)
	}

	if got := workloadNames(list); got != "StatefulSet/db,Deployment/web" {
		t.Fatalf("got %s", got)
	}

	deployments := 0

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, "/pods") || strings.HasSuffix(r.path, "/deployments") || strings.Contains(r.path, "..") {
			t.Fatalf("listed %s", r.path)
		}

		if r.path == "/apis/apps/v1/namespaces/shop/deployments/web" {
			deployments++
		}
	}

	if deployments != 1 {
		t.Fatalf("the Deployment read %d times", deployments)
	}
}

func TestAppWorkloadsManyPodsListTheirNamespace(t *testing.T) {
	var rows []string

	pods := []routePod{}

	for i := range routePodsByName + 1 {
		name := fmt.Sprintf("agent-%d", i)
		pods = append(pods, routePod{"shop", name})
		rows = append(rows, `{"cells":["`+name+`"],"object":{"metadata":{"name":"`+name+`","namespace":"shop","ownerReferences":[{"kind":"DaemonSet","name":"agent"}]}}}`)
	}

	rows = append(rows, `{"cells":["other"],"object":{"metadata":{"name":"other","namespace":"shop","ownerReferences":[{"kind":"DaemonSet","name":"other"}]}}}`)

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/pods":                   `{"kind":"Table","metadata":{},"columnDefinitions":[{"name":"Name"}],"rows":[` + strings.Join(rows, ",") + `]}`,
		"GET /apis/apps/v1/namespaces/shop/daemonsets/agent": fmt.Sprintf(appWorkloadAnswers, "agent"),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := appWorkloads(context.Background(), k, pods)
	if err != nil || workloadNames(list) != "DaemonSet/agent" {
		t.Fatalf("got %v %+v", err, list)
	}
}

func TestAppWorkloadsFailsOnOtherErrors(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/pods/web-1": "not json",
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := appWorkloads(context.Background(), k, []routePod{{"shop", "web-1"}}); err == nil {
		t.Fatal("a broken answer passed for none")
	}
}

func TestKubeAppWorkloadsDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeAppWorkloads(cfg, "", "", `[{"namespace":"demo","pod":"worker-6f4b8-pqrst"},{"namespace":"demo","pod":"postgres-0"}]`)
	if err != nil {
		t.Fatal(err)
	}

	var list kubeWorkloadList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	if len(list.Workloads) != 2 {
		t.Fatalf("demo workloads %s", out)
	}

	if _, err := KubeAppWorkloads(cfg, "", "", "not json"); err == nil {
		t.Fatal("invalid pod list accepted")
	}
}
