package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

const promoteClusterJSON = `{"metadata":{"name":"pg","namespace":"shop","uid":"cluster-uid","resourceVersion":"7"},"status":{"currentPrimary":"pg-1","targetPrimary":"pg-1"}}`
const promotePodJSON = `{"metadata":{"name":"pg-2","namespace":"shop","uid":"pod-uid","labels":{"cnpg.io/cluster":"pg"},"ownerReferences":[{"kind":"Cluster","name":"pg","uid":"cluster-uid"}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`

func promoteAPI(t *testing.T, cluster, pod string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"postgresql.cnpg.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg":          cluster,
		"GET /api/v1/namespaces/shop/pods/pg-2":                                pod,
		"GET /api/v1/namespaces/shop/pods/https:pg-1:8000/proxy/pg/status":     `{"isPrimary":true,"currentLsn":"0/1000","timeLineID":1}`,
		"GET /api/v1/namespaces/shop/pods/https:pg-2:8000/proxy/pg/status":     `{"isPrimary":false,"replayLsn":"0/0F00","timeLineID":1}`,
		"PATCH /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg/status": `{}`,
	})
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return f, k
}
func TestCNPGPromotePreviewLag(t *testing.T) {
	f, k := promoteAPI(t, promoteClusterJSON, promotePodJSON)
	preview, err := readCNPGPromotePreview(context.Background(), k, "shop", "pg", "pg-2")
	if err != nil || preview.LagBytes == nil || *preview.LagBytes != 256 || !preview.Ready || !preview.CanPromote {
		t.Fatalf("%+v %v", preview, err)
	}
	f.answerWith("GET /api/v1/namespaces/shop/pods/https:pg-2:8000/proxy/pg/status", 403, `{"kind":"Status","code":403,"reason":"Forbidden"}`)
	preview, err = readCNPGPromotePreview(context.Background(), k, "shop", "pg", "pg-2")
	if err != nil || preview.LagBytes != nil || !preview.CanPromote || len(preview.Warnings) == 0 {
		t.Fatalf("%+v %v", preview, err)
	}
}
func TestCNPGPromoteRefusals(t *testing.T) {
	for _, tc := range []struct{ name, cluster, pod string }{
		{"primary label", promoteClusterJSON, strings.Replace(promotePodJSON, `"cnpg.io/cluster":"pg"`, `"cnpg.io/cluster":"pg","cnpg.io/instanceRole":"primary"`, 1)},
		{"primary", strings.ReplaceAll(promoteClusterJSON, "pg-1", "pg-2"), promotePodJSON},
		{"non-ready", promoteClusterJSON, strings.Replace(promotePodJSON, `"status":"True"`, `"status":"False"`, 1)},
		{"foreign", promoteClusterJSON, strings.Replace(promotePodJSON, "cluster-uid", "other-uid", 1)},
		{"hibernated", strings.Replace(promoteClusterJSON, `"name":"pg","namespace"`, `"annotations":{"cnpg.io/hibernation":"on"},"name":"pg","namespace"`, 1), promotePodJSON},
		{"switching", strings.Replace(promoteClusterJSON, `"targetPrimary":"pg-1"`, `"targetPrimary":"pg-3"`, 1), promotePodJSON},
		{"fenced", strings.Replace(promoteClusterJSON, `"name":"pg","namespace"`, `"annotations":{"cnpg.io/fencedInstances":"[\"pg-2\"]"},"name":"pg","namespace"`, 1), promotePodJSON},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, k := promoteAPI(t, tc.cluster, tc.pod)
			if err := promoteCNPG(context.Background(), k, "shop", "pg", "pg-2", time.Now()); err == nil {
				t.Fatal("accepted unsafe target")
			}
			if lastRequest(f).method == "PATCH" {
				t.Fatal("patched after refusal")
			}
		})
	}
}

func TestCNPGPromoteConflictAndMissingOperator(t *testing.T) {
	f, k := promoteAPI(t, promoteClusterJSON, promotePodJSON)
	f.answerWith("PATCH /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg/status", 409, `{"kind":"Status","code":409,"reason":"Conflict","message":"changed"}`)
	if err := promoteCNPG(context.Background(), k, "shop", "pg", "pg-2", time.Now()); kubeCode(err) != 409 {
		t.Fatalf("%v", err)
	}
	f.answerWith("GET /apis", 200, `{"groups":[]}`)
	if _, err := readCNPGPromotePreview(context.Background(), k, "shop", "pg", "pg-2"); err == nil {
		t.Fatal("missing operator accepted")
	}
}
func TestCNPGPromoteUnknownTimelineAndDemo(t *testing.T) {
	f, k := promoteAPI(t, promoteClusterJSON, promotePodJSON)
	f.answerWith("GET /api/v1/namespaces/shop/pods/https:pg-2:8000/proxy/pg/status", 200, `{"isPrimary":false,"replayLsn":"0/0F00","timeLineID":2}`)
	r, err := readCNPGPromotePreview(context.Background(), k, "shop", "pg", "pg-2")
	if err != nil || r.LagBytes != nil || !slices.Contains(r.Warnings, "lagUnknown") {
		t.Fatalf("%+v %v", r, err)
	}
	demo := demoCNPGPromotePreview("demo", "auth-db", "auth-db-2")
	if demo.CurrentPrimary != "auth-db-1" || !demo.CanPromote || demo.LagBytes == nil {
		t.Fatalf("%+v", demo)
	}
}
func TestCNPGPromotePatch(t *testing.T) {
	f, k := promoteAPI(t, promoteClusterJSON, promotePodJSON)
	if err := promoteCNPG(context.Background(), k, "shop", "pg", "pg-2", time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	req := lastRequest(f)
	var patch struct {
		Metadata struct{ ResourceVersion string }
		Status   struct{ TargetPrimary, Phase, TargetPrimaryTimestamp string }
	}
	if err := json.Unmarshal([]byte(req.body), &patch); err != nil {
		t.Fatal(err)
	}
	if req.method != "PATCH" || !strings.HasSuffix(req.path, "/status") || patch.Metadata.ResourceVersion != "7" || patch.Status.TargetPrimary != "pg-2" || patch.Status.Phase != "Switchover in progress" || patch.Status.TargetPrimaryTimestamp == "" {
		t.Fatalf("%+v %+v", req, patch)
	}
}
func TestCNPGPromoteLSNs(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		value uint64
		ok    bool
	}{
		{`"1/00000010"`, 4294967312, true}, {`4096`, 4096, true}, {`"0/0"`, 0, true}, {`null`, 0, false}, {`""`, 0, false}, {`"bad"`, 0, false},
	} {
		got, ok := parseCNPGLSN(json.RawMessage(tc.raw))
		if got != tc.value || ok != tc.ok {
			t.Errorf("%s: %d %v", tc.raw, got, ok)
		}
	}
}
func TestCNPGPromoteDemoAndValidation(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := KubeCNPGPromote(cfg, "demo", "", "shop", "pg", "pg-2"); err == nil {
		t.Fatal("demo promoted")
	}
	if _, err := KubeCNPGPromotePreview("", "", "", "shop", "../pg", "pg-2"); err == nil {
		t.Fatal("invalid name accepted")
	}
}
