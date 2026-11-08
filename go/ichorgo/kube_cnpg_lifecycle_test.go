package ichorgo

import (
	"context"
	"strings"
	"testing"
)

const lifecycleCluster = `{"metadata":{"name":"pg","namespace":"shop","uid":"cluster-uid","resourceVersion":"7"},"status":{"currentPrimary":"pg-1","targetPrimary":"pg-1","phase":"Cluster in healthy state"}}`
const lifecyclePrimary = `{"metadata":{"name":"pg-1","namespace":"shop","uid":"pod-uid","labels":{"cnpg.io/cluster":"pg","cnpg.io/instanceRole":"primary"},"ownerReferences":[{"kind":"Cluster","name":"pg","uid":"cluster-uid"}]},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`

func lifecycleAPI(t *testing.T, c, p string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"postgresql.cnpg.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg":          c,
		"GET /api/v1/namespaces/shop/pods/pg-1":                                p,
		"PATCH /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg/status": `{}`,
		"PATCH /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg":        `{}`,
	})
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return f, k
}
func TestCNPGRestartPrimaryInPlace(t *testing.T) {
	f, k := lifecycleAPI(t, lifecycleCluster, lifecyclePrimary)
	if _, err := restartCNPGPrimary(context.Background(), k, "shop", "pg"); err != nil {
		t.Fatal(err)
	}
	req := lastRequest(f)
	if req.method != "PATCH" || !strings.HasSuffix(req.path, "/status") || !strings.Contains(req.body, `"resourceVersion":"7"`) || !strings.Contains(req.body, `"phase":"Primary instance is being restarted in-place"`) {
		t.Fatalf("%+v", req)
	}
}
func TestCNPGRestartPrimaryRefusals(t *testing.T) {
	for _, tc := range []struct{ name, cluster, pod string }{
		{"foreign", lifecycleCluster, strings.Replace(lifecyclePrimary, "cluster-uid", "other-uid", 1)},
		{"not ready", lifecycleCluster, strings.Replace(lifecyclePrimary, `"status":"True"`, `"status":"False"`, 1)},
		{"not primary", lifecycleCluster, strings.Replace(lifecyclePrimary, `"cnpg.io/instanceRole":"primary"`, `"cnpg.io/instanceRole":"replica"`, 1)},
		{"switching", strings.Replace(lifecycleCluster, `"targetPrimary":"pg-1"`, `"targetPrimary":"pg-2"`, 1), lifecyclePrimary},
		{"duplicate", strings.Replace(lifecycleCluster, "Cluster in healthy state", "Primary instance is being restarted in-place", 1), lifecyclePrimary},
		{"hibernated", strings.Replace(lifecycleCluster, `"name":"pg","namespace"`, `"annotations":{"cnpg.io/hibernation":"on"},"name":"pg","namespace"`, 1), lifecyclePrimary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, k := lifecycleAPI(t, tc.cluster, tc.pod)
			if _, err := restartCNPGPrimary(context.Background(), k, "shop", "pg"); err == nil {
				t.Fatal("accepted invalid primary")
			}
			if lastRequest(f).method == "PATCH" {
				t.Fatal("patched after refusal")
			}
		})
	}
}
func TestCNPGHibernateAnnotation(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		c := lifecycleCluster
		if !enabled {
			c = strings.Replace(c, `"name":"pg","namespace"`, `"annotations":{"cnpg.io/hibernation":"on","keep":"value"},"name":"pg","namespace"`, 1)
		}
		f, k := lifecycleAPI(t, c, lifecyclePrimary)
		if err := hibernateCNPG(context.Background(), k, "shop", "pg", enabled); err != nil {
			t.Fatal(err)
		}
		req := lastRequest(f)
		want := "off"
		if enabled {
			want = "on"
		}
		if req.method != "PATCH" || strings.HasSuffix(req.path, "/status") || !strings.Contains(req.body, `"resourceVersion":"7"`) || !strings.Contains(req.body, `"cnpg.io/hibernation":"`+want+`"`) {
			t.Fatalf("%+v", req)
		}
	}
}
func TestCNPGHibernateDuplicateAndConflict(t *testing.T) {
	f, k := lifecycleAPI(t, lifecycleCluster, lifecyclePrimary)
	if err := hibernateCNPG(context.Background(), k, "shop", "pg", false); err == nil {
		t.Fatal("duplicate wake accepted")
	}
	f.answerWith("PATCH /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg", 409, `{"kind":"Status","code":409,"reason":"Conflict"}`)
	if err := hibernateCNPG(context.Background(), k, "shop", "pg", true); kubeCode(err) != 409 {
		t.Fatalf("%v", err)
	}
}
func TestCNPGLifecycleDemoRefused(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := KubeCNPGRestartPrimary(cfg, "Demo cluster", "", "demo", "auth-db"); err == nil {
		t.Fatal("demo restart accepted")
	}
	if err := KubeCNPGHibernate(cfg, "Demo cluster", "", "demo", "auth-db", true); err == nil {
		t.Fatal("demo hibernation accepted")
	}
}

func TestCNPGLifecycleMissingOperatorAndDenied(t *testing.T) {
	f, k := lifecycleAPI(t, lifecycleCluster, lifecyclePrimary)
	f.answerWith("GET /apis", 200, `{"groups":[]}`)
	if _, err := restartCNPGPrimary(context.Background(), k, "shop", "pg"); err == nil {
		t.Fatal("missing operator accepted")
	}
	f.answerWith("GET /apis", 200, `{"groups":[{"name":"postgresql.cnpg.io","preferredVersion":{"version":"v1"}}]}`)
	for _, code := range []int{403, 409} {
		f.answerWith("PATCH /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg/status", code, `{"kind":"Status","reason":"Denied"}`)
		if _, err := restartCNPGPrimary(context.Background(), k, "shop", "pg"); kubeCode(err) != code {
			t.Fatalf("wanted %d: %v", code, err)
		}
	}
}
