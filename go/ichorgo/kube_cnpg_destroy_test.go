package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const (
	destroyClusterJSON = `{"metadata":{"name":"pg","namespace":"shop","uid":"cluster-uid","resourceVersion":"7"},"status":{"currentPrimary":"pg-1","targetPrimary":"pg-1"}}`
	destroyPodJSON     = `{"metadata":{"name":"pg-2","namespace":"shop","uid":"pod-uid","labels":{"cnpg.io/cluster":"pg"},"ownerReferences":[{"kind":"Cluster","name":"pg","uid":"cluster-uid"}]},"status":{"phase":"Pending"}}`
	destroyPVCsPath    = "GET /api/v1/namespaces/shop/persistentvolumeclaims"
)

func destroyPVC(name, instance, role, volume, rv, owner string) string {
	return `{"metadata":{"name":"` + name + `","uid":"` + name + `-uid","resourceVersion":"` + rv + `","labels":{"cnpg.io/cluster":"pg","cnpg.io/instanceName":"` + instance + `","cnpg.io/pvcRole":"` + role + `"},` +
		`"ownerReferences":[{"kind":"Cluster","uid":"` + owner + `"}]},"spec":{"volumeName":"` + volume + `"}}`
}

func destroyPV(node string) string {
	return `{"spec":{"nodeAffinity":{"required":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"kubernetes.io/hostname","operator":"In","values":["` + node + `"]}]}]}}}}`
}

func destroyPVCList(items ...string) string {
	return `{"metadata":{},"items":[` + strings.Join(items, ",") + `]}`
}

// destroyAPI: pg-1 primary on worker-a, pg-2 a replica whose data and WAL volumes are pinned
// to worker-gone, a node that no longer exists.
func destroyAPI(t *testing.T) (*fakeKubeAPI, *kubeClient) {
	t.Helper()
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"postgresql.cnpg.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg": destroyClusterJSON,
		"GET /api/v1/namespaces/shop/pods/pg-2":                       destroyPodJSON,
		destroyPVCsPath: destroyPVCList(
			destroyPVC("pg-1", "pg-1", "PG_DATA", "pv-1", "11", "cluster-uid"),
			destroyPVC("pg-2", "pg-2", "PG_DATA", "pv-2", "21", "cluster-uid"),
			destroyPVC("pg-2-wal", "pg-2", "PG_WAL", "pv-2w", "22", "cluster-uid"),
		),
		"GET /api/v1/persistentvolumes/pv-1":                             destroyPV("worker-a"),
		"GET /api/v1/persistentvolumes/pv-2":                             destroyPV("worker-gone"),
		"GET /api/v1/persistentvolumes/pv-2w":                            destroyPV("worker-gone"),
		"GET /api/v1/nodes/worker-a":                                     `{}`,
		"DELETE /api/v1/namespaces/shop/persistentvolumeclaims/pg-2":     `{}`,
		"DELETE /api/v1/namespaces/shop/persistentvolumeclaims/pg-2-wal": `{}`,
		"DELETE /api/v1/namespaces/shop/pods/pg-2":                       `{}`,
	})
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return f, k
}

func deletes(f *fakeKubeAPI) []fakeKubeRequest {
	out := []fakeKubeRequest{}
	for _, r := range f.recorded() {
		if r.method == "DELETE" {
			out = append(out, r)
		}
	}
	return out
}

func TestCNPGDestroyPreviewStrandedReplica(t *testing.T) {
	_, k := destroyAPI(t)
	r, err := readCNPGDestroyPreview(context.Background(), k, "shop", "pg", "pg-2")
	if err != nil || !r.CanDestroy || len(r.Blockers) != 0 || len(r.Warnings) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.PodPhase != "Pending" || r.PodNode != "" || r.CurrentPrimary != "pg-1" || len(r.Volumes) != 2 {
		t.Fatalf("%+v", r)
	}
	for _, v := range r.Volumes {
		if v.Node != "worker-gone" || v.NodeState != cnpgNodeGone {
			t.Fatalf("%+v", v)
		}
	}
	if r.Volumes[1].Role != "PG_WAL" {
		t.Fatalf("%+v", r.Volumes)
	}
}

func TestCNPGDestroyDeletesVolumesThenPod(t *testing.T) {
	f, k := destroyAPI(t)
	if err := destroyCNPGInstance(context.Background(), k, "shop", "pg", "pg-2"); err != nil {
		t.Fatal(err)
	}
	got := deletes(f)
	want := []string{"/api/v1/namespaces/shop/persistentvolumeclaims/pg-2", "/api/v1/namespaces/shop/persistentvolumeclaims/pg-2-wal", "/api/v1/namespaces/shop/pods/pg-2"}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i, r := range got {
		if r.path != want[i] {
			t.Fatalf("delete %d: %s, want %s", i, r.path, want[i])
		}
	}
	var opts struct {
		Preconditions struct{ ResourceVersion string }
	}
	if err := json.Unmarshal([]byte(got[0].body), &opts); err != nil || opts.Preconditions.ResourceVersion != "21" {
		t.Fatalf("%s %v", got[0].body, err)
	}
}

func TestCNPGDestroyRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, instance, blocker string
		edit                    func(f *fakeKubeAPI)
	}{
		{"current primary", "pg-1", cnpgDestroyPrimary, func(f *fakeKubeAPI) {
			f.answerWith("GET /api/v1/namespaces/shop/pods/pg-1", 200, strings.ReplaceAll(destroyPodJSON, "pg-2", "pg-1"))
		}},
		{"target primary", "pg-2", cnpgDestroyPrimary, func(f *fakeKubeAPI) {
			f.answerWith("GET /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg", 200, strings.Replace(destroyClusterJSON, `"targetPrimary":"pg-1"`, `"targetPrimary":"pg-2"`, 1))
		}},
		{"primary label", "pg-2", cnpgDestroyPrimary, func(f *fakeKubeAPI) {
			f.answerWith("GET /api/v1/namespaces/shop/pods/pg-2", 200, strings.Replace(destroyPodJSON, `"cnpg.io/cluster":"pg"`, `"cnpg.io/cluster":"pg","cnpg.io/instanceRole":"primary"`, 1))
		}},
		{"no known primary", "pg-2", cnpgDestroyPrimary, func(f *fakeKubeAPI) {
			f.answerWith("GET /apis/postgresql.cnpg.io/v1/namespaces/shop/clusters/pg", 200, strings.ReplaceAll(destroyClusterJSON, `"pg-1"`, `""`))
		}},
		{"primary keeps no volume", "pg-2", cnpgDestroyNoOtherData, func(f *fakeKubeAPI) {
			f.answerWith(destroyPVCsPath, 200, destroyPVCList(destroyPVC("pg-2", "pg-2", "PG_DATA", "pv-2", "21", "cluster-uid")))
		}},
		{"primary volume of another cluster", "pg-2", cnpgDestroyNoOtherData, func(f *fakeKubeAPI) {
			f.answerWith(destroyPVCsPath, 200, destroyPVCList(
				destroyPVC("pg-1", "pg-1", "PG_DATA", "pv-1", "11", "other-uid"),
				destroyPVC("pg-2", "pg-2", "PG_DATA", "pv-2", "21", "cluster-uid")))
		}},
		{"nothing left", "pg-3", cnpgDestroyNotFound, func(*fakeKubeAPI) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, k := destroyAPI(t)
			tc.edit(f)
			r, err := readCNPGDestroyPreview(context.Background(), k, "shop", "pg", tc.instance)
			if err != nil || r.CanDestroy || !slices.Contains(r.Blockers, tc.blocker) {
				t.Fatalf("%+v %v", r, err)
			}
			if err := destroyCNPGInstance(context.Background(), k, "shop", "pg", tc.instance); err == nil {
				t.Fatal("removed an unsafe instance")
			}
			if d := deletes(f); len(d) != 0 {
				t.Fatalf("deleted after a refusal: %+v", d)
			}
		})
	}
}

func TestCNPGDestroyForeignPod(t *testing.T) {
	f, k := destroyAPI(t)
	f.answerWith("GET /api/v1/namespaces/shop/pods/pg-2", 200, strings.Replace(destroyPodJSON, `"uid":"cluster-uid"`, `"uid":"other-uid"`, 1))
	if err := destroyCNPGInstance(context.Background(), k, "shop", "pg", "pg-2"); err == nil || len(deletes(f)) != 0 {
		t.Fatalf("%v %+v", err, deletes(f))
	}
}

func TestCNPGDestroyInstanceWithoutPod(t *testing.T) {
	f, k := destroyAPI(t)
	f.answerWith("GET /api/v1/namespaces/shop/pods/pg-2", 404, `{"kind":"Status","code":404,"reason":"NotFound"}`)
	r, err := readCNPGDestroyPreview(context.Background(), k, "shop", "pg", "pg-2")
	if err != nil || !r.CanDestroy || r.PodPhase != "" {
		t.Fatalf("%+v %v", r, err)
	}
	if err := destroyCNPGInstance(context.Background(), k, "shop", "pg", "pg-2"); err != nil {
		t.Fatal(err)
	}
	for _, d := range deletes(f) {
		if strings.Contains(d.path, "/pods/") {
			t.Fatalf("deleted a pod that is not there: %+v", d)
		}
	}
}

func TestCNPGDestroyVolumeNodeStates(t *testing.T) {
	f, k := destroyAPI(t)
	f.answerWith("GET /api/v1/namespaces/shop/pods/pg-2", 200, strings.Replace(destroyPodJSON, `"status":{"phase":"Pending"}`, `"spec":{"nodeName":"worker-a"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}`, 1))
	f.answerWith("GET /api/v1/persistentvolumes/pv-2", 200, destroyPV("worker-a"))
	f.answerWith("GET /api/v1/persistentvolumes/pv-2w", 403, `{"kind":"Status","code":403,"reason":"Forbidden"}`)
	r, err := readCNPGDestroyPreview(context.Background(), k, "shop", "pg", "pg-2")
	if err != nil || !r.CanDestroy || r.PodNode != "worker-a" {
		t.Fatalf("%+v %v", r, err)
	}
	if !slices.Contains(r.Warnings, cnpgDestroyReady) || !slices.Contains(r.Warnings, cnpgDestroyNodePresent) {
		t.Fatalf("%+v", r.Warnings)
	}
	if r.Volumes[0].NodeState != cnpgNodePresent || r.Volumes[1].NodeState != cnpgNodeUnknown {
		t.Fatalf("%+v", r.Volumes)
	}
	if got := cnpgPVNode(cnpgPVObject{}); got != "" {
		t.Fatalf("network volume pinned to %q", got)
	}
}

func TestCNPGDestroyLegacyVolumeNames(t *testing.T) {
	for name, want := range map[string]string{"pg-2": "pg-2", "pg-2-wal": "pg-2", "pg-10": "pg-10"} {
		var pvc cnpgPVCObject
		pvc.Metadata.Name = name
		if got := cnpgPVCInstance(pvc); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
}

func TestCNPGDestroyDemoAndValidation(t *testing.T) {
	stranded := demoCNPGDestroyPreview("demo", "orders-db", "orders-db-2")
	if !stranded.CanDestroy || len(stranded.Volumes) != 1 || stranded.Volumes[0].NodeState != cnpgNodeGone {
		t.Fatalf("%+v", stranded)
	}
	if primary := demoCNPGDestroyPreview("demo", "orders-db", "orders-db-1"); primary.CanDestroy || !slices.Contains(primary.Blockers, cnpgDestroyPrimary) {
		t.Fatalf("%+v", primary)
	}
	if missing := demoCNPGDestroyPreview("demo", "nope", "nope-1"); missing.CanDestroy {
		t.Fatalf("%+v", missing)
	}
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := KubeCNPGDestroy(cfg, "Demo cluster", "", "demo", "orders-db", "orders-db-2"); err == nil {
		t.Fatal("demo removed an instance")
	}
	if _, err := KubeCNPGDestroyPreview("", "", "", "shop", "pg", "../pg-2"); err == nil {
		t.Fatal("invalid name accepted")
	}
}

func TestCNPGInstanceGaps(t *testing.T) {
	pods := []cnpgPod{{Name: "pg-9"}}
	failed, missing := cnpgInstanceGaps([]string{"pg-8", "pg-2", "pg-9", "pg-2"}, []string{"pg-8"}, pods)
	if !slices.Equal(failed, []string{"pg-8"}) || !slices.Equal(missing, []string{"pg-2", "pg-8"}) {
		t.Fatalf("%v %v", failed, missing)
	}
	failed, missing = cnpgInstanceGaps(nil, nil, nil)
	if failed == nil || missing == nil {
		t.Fatal("nil slices encode as null")
	}
}
