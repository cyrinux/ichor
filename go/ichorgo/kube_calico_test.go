package ichorgo

import (
	"context"
	"reflect"
	"testing"
)

func calicoPods(whiskerReady bool) []dsPod {
	whisker := fakePod("calico-system", "whisker-8676868cf8-d7n8n", "node-1", whiskerReady, map[string]string{"k8s-app": "whisker"}, "whisker", "quay.io/calico/whisker:v3.33.0")
	whisker.Spec.Containers = append(whisker.Spec.Containers, struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}{whiskerBackend, "quay.io/calico/whisker-backend:v3.33.0"})
	whisker.Status.ContainerStatuses = append(whisker.Status.ContainerStatuses, struct {
		Name  string `json:"name"`
		Ready bool   `json:"ready"`
	}{whiskerBackend, whiskerReady})

	return []dsPod{
		fakePod("calico-system", "calico-node-srncx", "node-2", true, map[string]string{"k8s-app": "calico-node"}, calicoNodeContainer, "quay.io/calico/node:v3.33.0"),
		fakePod("calico-system", "calico-node-abcde", "node-1", false, map[string]string{"k8s-app": "calico-node"}, calicoNodeContainer, "quay.io/calico/node:v3.33.0"),
		fakePod("calico-system", "goldmane-d7d9f6fb7-m8clw", "node-1", true, map[string]string{"k8s-app": "goldmane"}, "goldmane", "quay.io/calico/goldmane:v3.33.0"),
		whisker,
	}
}

func readCalicoStatusWith(t *testing.T, pods []dsPod, service string) ciliumStatus {
	t.Helper()

	answers := map[string]string{"GET /api/v1/pods": podListJSON(t, pods...)}
	if service != "" {
		answers["GET /api/v1/namespaces/calico-system/services/whisker"] = service
	}

	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	status, err := readFlowSource(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	return status
}

func TestReadCalicoStatus(t *testing.T) {
	status := readCalicoStatusWith(t, calicoPods(true), `{"spec":{"ports":[{"port":8443,"protocol":"TCP","targetPort":8443}]}}`)

	want := ciliumStatus{
		Installed: true, CNI: flowCNICalico, Namespace: "calico-system", Version: "v3.33.0", Hubble: true,
		Agents:  []ciliumAgent{{Node: "node-1", Pod: "calico-node-abcde"}, {Node: "node-2", Pod: "calico-node-srncx", Ready: true}},
		Whisker: &whiskerInfo{Namespace: "calico-system", Pod: "whisker-8676868cf8-d7n8n", Port: 8443, TLS: true},
	}
	if !reflect.DeepEqual(status, want) {
		t.Errorf("got %+v, whisker %+v", status, status.Whisker)
	}

	// Calico 3.30: a plain port.
	status = readCalicoStatusWith(t, calicoPods(false), `{"spec":{"ports":[{"port":8081,"protocol":"TCP"}]}}`)
	if !status.Hubble || status.Whisker == nil || status.Whisker.Port != 8081 || status.Whisker.TLS {
		t.Errorf("plain: %+v", status.Whisker)
	}

	// No whisker Service, or no whisker pod: Calico without flows.
	status = readCalicoStatusWith(t, calicoPods(true), "")
	if !status.Installed || status.Hubble || status.Whisker != nil {
		t.Errorf("no service: %+v", status)
	}

	status = readCalicoStatusWith(t, calicoPods(true)[:3], `{"spec":{"ports":[{"port":8081}]}}`)
	if !status.Installed || status.Hubble || status.Whisker != nil || status.Version != "v3.33.0" {
		t.Errorf("no whisker: %+v", status)
	}

	// Neither CNI.
	status = readCalicoStatusWith(t, calicoPods(true)[2:3], "")
	if status.Installed || status.CNI != "" {
		t.Errorf("none: %+v", status)
	}
}

func TestReadFlowSourcePrefersCilium(t *testing.T) {
	pods := append(calicoPods(true), fakePod("kube-system", "cilium-x", "node-1", true, map[string]string{"k8s-app": "cilium"}, ciliumAgentContainer, "quay.io/cilium/cilium:v1.18.2"))

	status := readCalicoStatusWith(t, pods, "")
	if status.CNI != flowCNICilium || status.Version != "v1.18.2" || status.Whisker != nil {
		t.Errorf("%+v", status)
	}
}

func TestWhiskerPort(t *testing.T) {
	if port, tls, ok := whiskerPort([]whiskerServicePort{{Name: "http", Port: 8081}, {Name: "https", Port: 9443}}); port != 9443 || !tls || !ok {
		t.Errorf("named: %d %v %v", port, tls, ok)
	}

	if _, _, ok := whiskerPort(nil); ok {
		t.Error("no port")
	}
}
