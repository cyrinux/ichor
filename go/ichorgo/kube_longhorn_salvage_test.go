package ichorgo

import (
	"context"
	"strings"
	"testing"
)

const salvageVolume = `{"metadata":{"name":"vol","namespace":"storage"},"status":{"state":"detached","robustness":"faulted"}}`
const salvageReplicas = `{"items":[{"metadata":{"name":"vol-r1","namespace":"storage"},"spec":{"volumeName":"vol","nodeID":"node-1","diskID":"disk-1","healthyAt":"2026-10-01T10:00:00Z","failedAt":"2026-10-02T10:00:00Z"}},{"metadata":{"name":"other-r","namespace":"storage"},"spec":{"volumeName":"other","nodeID":"node-1","diskID":"disk-1"}}]}`
const salvageNodes = `{"items":[{"metadata":{"name":"node-1","namespace":"storage"},"status":{"conditions":[{"type":"Ready","status":"True"}],"diskStatus":{"disk":{"diskUUID":"disk-1","conditions":[{"type":"Schedulable","status":"True"}]}}}}]}`

func salvageAPI(t *testing.T) (*fakeKubeAPI, *kubeClient) {
	t.Helper()
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /apis/longhorn.io/v1beta2/namespaces/storage/volumes/vol":                        salvageVolume,
		"GET /apis/longhorn.io/v1beta2/namespaces/storage/replicas":                           salvageReplicas,
		"GET /apis/longhorn.io/v1beta2/namespaces/storage/nodes":                              salvageNodes,
		"POST /api/v1/namespaces/storage/services/longhorn-backend:9500/proxy/v1/volumes/vol": `{}`,
	})
	k, e := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if e != nil {
		t.Fatal(e)
	}
	return f, k
}
func TestLonghornSalvagePreviewAndAction(t *testing.T) {
	f, k := salvageAPI(t)
	p, e := readLonghornSalvagePreview(context.Background(), k, "storage", "vol")
	if e != nil || !p.CanSalvage || len(p.Replicas) != 1 || !p.Replicas[0].Eligible || p.Replicas[0].HealthyAt == 0 || p.Replicas[0].FailedAt == 0 {
		t.Fatalf("%+v %v", p, e)
	}
	if e := salvageLonghorn(context.Background(), k, "storage", "vol", []string{"vol-r1"}); e != nil {
		t.Fatal(e)
	}
	r := lastRequest(f)
	if r.method != "POST" || !strings.Contains(r.query, "action=salvage") || r.body != `{"replicaNames":["vol-r1"]}` {
		t.Fatalf("%+v", r)
	}
}
func TestLonghornSalvageRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, route, body string
		names             []string
	}{
		{"empty", "", "", nil}, {"foreign", "", "", []string{"other-r"}}, {"duplicate", "", "", []string{"vol-r1", "vol-r1"}},
		{"attached", "GET /apis/longhorn.io/v1beta2/namespaces/storage/volumes/vol", strings.Replace(salvageVolume, "detached", "attached", 1), []string{"vol-r1"}},
		{"healthy", "GET /apis/longhorn.io/v1beta2/namespaces/storage/volumes/vol", strings.Replace(salvageVolume, "faulted", "healthy", 1), []string{"vol-r1"}},
		{"node down", "GET /apis/longhorn.io/v1beta2/namespaces/storage/nodes", strings.Replace(salvageNodes, "True", "False", 1), []string{"vol-r1"}},
		{"disk unavailable", "GET /apis/longhorn.io/v1beta2/namespaces/storage/nodes", strings.Replace(salvageNodes, "disk-1", "disk-2", 1), []string{"vol-r1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, k := salvageAPI(t)
			if tc.route != "" {
				f.answerWith(tc.route, 200, tc.body)
			}
			if e := salvageLonghorn(context.Background(), k, "storage", "vol", tc.names); e == nil {
				t.Fatal("accepted unsafe selection")
			}
			if lastRequest(f).method == "POST" {
				t.Fatal("submitted after refusal")
			}
		})
	}
}
func TestLonghornSalvagePartialAndDenied(t *testing.T) {
	f, k := salvageAPI(t)
	f.answerWith("GET /apis/longhorn.io/v1beta2/namespaces/storage/replicas", 403, `{"kind":"Status"}`)
	p, e := readLonghornSalvagePreview(context.Background(), k, "storage", "vol")
	if e != nil || p.Error == "" || p.CanSalvage {
		t.Fatalf("%+v %v", p, e)
	}
	if e := salvageLonghorn(context.Background(), k, "storage", "vol", []string{"vol-r1"}); e == nil {
		t.Fatal("partial read accepted")
	}
	f.answerWith("GET /apis/longhorn.io/v1beta2/namespaces/storage/replicas", 200, salvageReplicas)
	f.answerWith("POST /api/v1/namespaces/storage/services/longhorn-backend:9500/proxy/v1/volumes/vol", 409, `{"kind":"Status"}`)
	if e := salvageLonghorn(context.Background(), k, "storage", "vol", []string{"vol-r1"}); kubeCode(e) != 409 {
		t.Fatalf("%v", e)
	}
}
func TestLonghornSalvageMissingAndDemo(t *testing.T) {
	f, k := salvageAPI(t)
	f.answerWith("GET /apis", 200, `{"groups":[]}`)
	if _, err := readLonghornSalvagePreview(context.Background(), k, "storage", "vol"); err == nil {
		t.Fatal("missing operator accepted")
	}
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := KubeLonghornSalvage(cfg, "Demo cluster", "", "longhorn-system", "pvc-5d1e", `["pvc-5d1e-r1"]`); err == nil {
		t.Fatal("demo accepted")
	}
	if out, err := KubeLonghornSalvagePreview(cfg, "Demo cluster", "", "longhorn-system", "pvc-5d1e"); err != nil || !strings.Contains(out, `"canSalvage":true`) {
		t.Fatalf("%s %v", out, err)
	}
	for _, raw := range []string{`[]`, `null`, `["../r"]`, `invalid`} {
		if err := KubeLonghornSalvage(cfg, "Demo cluster", "", "storage", "vol", raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
