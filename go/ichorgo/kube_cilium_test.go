package ichorgo

import (
	"context"
	"reflect"
	"testing"
)

func TestReadCiliumStatus(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"cilium-zz9k2","namespace":"net"},"spec":{"nodeName":"worker-2","containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.19.5@sha256:abc"}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"cilium-agent","ready":true}]}},
		  {"metadata":{"name":"cilium-ab4cd","namespace":"net"},"spec":{"nodeName":"worker-1","containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.19.5"}]},
		   "status":{"phase":"Pending"}},
		  {"metadata":{"name":"cilium-envoy-x","namespace":"net"},"spec":{"nodeName":"worker-1","containers":[{"name":"cilium-envoy","image":"quay.io/cilium/cilium-envoy:v1"}]},"status":{"phase":"Running"}}]}`,
		"GET /api/v1/namespaces/net/configmaps/cilium-config": `{"data":{"enable-hubble":"true","hubble-event-buffer-capacity":"8191"}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := readCiliumStatus(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	want := ciliumStatus{
		Installed: true, CNI: flowCNICilium, Namespace: "net", Version: "v1.19.5", Hubble: true, Buffer: 8191,
		Agents: []ciliumAgent{{Node: "worker-1", Pod: "cilium-ab4cd"}, {Node: "worker-2", Pod: "cilium-zz9k2", Ready: true}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}

	if req := f.recorded()[1]; req.path != "/api/v1/pods" {
		t.Fatalf("first request %+v", req)
	}
}

func TestReadCiliumStatusAbsent(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/pods": `{"items":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := readCiliumStatus(context.Background(), k)
	if err != nil || got.Installed || got.Hubble || len(got.Agents) != 0 {
		t.Fatalf("got %+v %v", got, err)
	}
}
