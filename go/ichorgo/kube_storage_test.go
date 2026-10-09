package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	storagePVCsBody = `{"items":[
		{"metadata":{"name":"data-pg-1","namespace":"db","labels":{"cnpg.io/cluster":"pg"}},
		 "spec":{"storageClassName":"longhorn","volumeName":"pv-pg","accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"10Gi"}}},
		 "status":{"phase":"Bound","capacity":{"storage":"10Gi"}}},
		{"metadata":{"name":"uploads","namespace":"db"},
		 "spec":{"storageClassName":"ceph-rbd","volumeName":"pv-up","accessModes":["ReadWriteOnce"]},
		 "status":{"phase":"Bound","capacity":{"storage":"20Gi"}}},
		{"metadata":{"name":"gone","namespace":"db"},
		 "spec":{"storageClassName":"local","volumeName":"pv-gone"},
		 "status":{"phase":"Lost"}},
		{"metadata":{"name":"old","namespace":"db","deletionTimestamp":"2026-10-09T10:00:00Z"},
		 "spec":{"storageClassName":"local"},
		 "status":{"phase":"Bound","capacity":{"storage":"1Gi"}}}]}`
	storagePodsBody = `{"items":[
		{"metadata":{"name":"pg-1","namespace":"db"},"spec":{"volumes":[{"persistentVolumeClaim":{"claimName":"data-pg-1"}},{"emptyDir":{}}]}},
		{"metadata":{"name":"web-a","namespace":"db"},"spec":{"volumes":[{"persistentVolumeClaim":{"claimName":"uploads"}}]}},
		{"metadata":{"name":"web-b","namespace":"db"},"spec":{"volumes":[{"persistentVolumeClaim":{"claimName":"uploads"}}]}}]}`
	storagePVsBody = `{"items":[
		{"metadata":{"name":"pv-pg"},"spec":{"storageClassName":"longhorn","csi":{"driver":"driver.longhorn.io"},"persistentVolumeReclaimPolicy":"Delete"}},
		{"metadata":{"name":"pv-up"},"spec":{"storageClassName":"ceph-rbd","csi":{"driver":"rook-ceph.rbd.csi.ceph.com"},"persistentVolumeReclaimPolicy":"Retain"}}]}`
	storageClassesBody = `{"items":[
		{"metadata":{"name":"longhorn"},"provisioner":"driver.longhorn.io"},
		{"metadata":{"name":"ceph-rbd"},"provisioner":"rook-ceph.rbd.csi.ceph.com"},
		{"metadata":{"name":"local"},"provisioner":"rancher.io/local-path"}]}`
	storageNodesBody   = `{"items":[{"metadata":{"name":"w1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`
	storageSummaryBody = `{"pods":[{"podRef":{"name":"pg-1","namespace":"db"},"volume":[
		{"pvcRef":{"name":"data-pg-1","namespace":"db"},"usedBytes":9663676416,"capacityBytes":10737418240,"inodes":1000,"inodesUsed":100}]}]}`
)

func storageAPI(t *testing.T) *fakeKubeAPI {
	t.Helper()

	return newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/db/persistentvolumeclaims": storagePVCsBody,
		"GET /api/v1/namespaces/db/pods":                   storagePodsBody,
		"GET /api/v1/persistentvolumes":                    storagePVsBody,
		"GET /apis/storage.k8s.io/v1/storageclasses":       storageClassesBody,
		"GET /api/v1/nodes":                                storageNodesBody,
		"GET /api/v1/nodes/w1/proxy/stats/summary":         storageSummaryBody,
	})
}

func claimNamed(t *testing.T, s kubeStorage, name string) storageClaim {
	t.Helper()

	for _, c := range s.Claims {
		if c.Name == name {
			return c
		}
	}

	t.Fatalf("no claim %q in %+v", name, s.Claims)

	return storageClaim{}
}

func TestStorageJoinsClaimsVolumesClassesPodsAndFill(t *testing.T) {
	s, err := readStorage(context.Background(), openFakeKube(t, storageAPI(t)), "db")
	if err != nil {
		t.Fatal(err)
	}

	pg := claimNamed(t, s, "data-pg-1")
	if pg.Volume != "pv-pg" || pg.Provisioner != "driver.longhorn.io" || pg.ReclaimPolicy != "Delete" || strings.Join(pg.AccessModes, ",") != "ReadWriteOnce" {
		t.Errorf("pg volume %+v", pg)
	}

	if !pg.Measured || pg.UsedPercent != 90 || pg.Capacity != 10<<30 || pg.Level != storageWarning {
		t.Errorf("pg fill %+v", pg)
	}

	// A CloudNativePG claim on Longhorn opens the database, not the volume.
	if pg.ManagedBy != "cloudnative-pg" || strings.Join(pg.Pods, ",") != "pg-1" {
		t.Errorf("pg managed by %q, pods %v", pg.ManagedBy, pg.Pods)
	}

	up := claimNamed(t, s, "uploads")
	if up.ManagedBy != "rook" || strings.Join(up.Pods, ",") != "web-a,web-b" || up.Measured || up.Capacity != 20<<30 || up.Level != storageOK {
		t.Errorf("uploads %+v", up)
	}

	if gone := claimNamed(t, s, "gone"); gone.Level != storageCritical {
		t.Errorf("lost claim %+v", gone)
	}

	if old := claimNamed(t, s, "old"); !old.Terminating || old.Level != storageWarning {
		t.Errorf("terminating claim %+v", old)
	}

	// Problems first, the worst first; then the fullest.
	var order []string
	for _, c := range s.Claims {
		order = append(order, c.Name)
	}

	if strings.Join(order, ",") != "gone,data-pg-1,old,uploads" {
		t.Errorf("order %v", order)
	}
}

func TestStorageWithoutClusterWideReads(t *testing.T) {
	f := storageAPI(t)
	for _, key := range []string{"GET /api/v1/persistentvolumes", "GET /api/v1/nodes", "GET /apis/storage.k8s.io/v1/storageclasses"} {
		f.answerWith(key, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)
	}

	s, err := readStorage(context.Background(), openFakeKube(t, f), "db")
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Claims) != 4 || !s.PartialAccess {
		t.Fatalf("claims %d, partial %v", len(s.Claims), s.PartialAccess)
	}

	// Without the class, the claim's own label still says CloudNativePG.
	if pg := claimNamed(t, s, "data-pg-1"); pg.Measured || pg.ManagedBy != "cloudnative-pg" || pg.Provisioner != "" {
		t.Errorf("pg %+v", pg)
	}
}

func TestStorageRefusedClaimsIsAnError(t *testing.T) {
	f := storageAPI(t)
	f.answerWith("GET /api/v1/namespaces/db/persistentvolumeclaims", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)

	if _, err := readStorage(context.Background(), openFakeKube(t, f), "db"); err == nil {
		t.Fatal("no error")
	}
}

func TestManagedByProvisioner(t *testing.T) {
	for provisioner, want := range map[string]string{
		"driver.longhorn.io":            "longhorn",
		"rook-ceph.rbd.csi.ceph.com":    "rook",
		"rook-ceph.cephfs.csi.ceph.com": "rook",
		"ebs.csi.aws.com":               "",
	} {
		if got := managedByOf(nil, provisioner); got != want {
			t.Errorf("%s: %q, want %q", provisioner, got, want)
		}
	}
}

func TestKubeStorageDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeStorage(cfg, "", "", "demo")
	if err != nil {
		t.Fatal(err)
	}

	var s kubeStorage
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Claims) == 0 {
		t.Fatalf("demo: %v %s", err, out)
	}

	for _, c := range s.Claims {
		if c.Namespace != "demo" {
			t.Errorf("claim of %s in the demo namespace's list", c.Namespace)
		}
	}

	if s.Claims[0].Level != storageCritical {
		t.Errorf("the demo shows no problem first: %+v", s.Claims[0])
	}

	if _, err := KubeStorage(cfg, "", "", "Bad Namespace"); err == nil {
		t.Error("a bad namespace was accepted")
	}
}
