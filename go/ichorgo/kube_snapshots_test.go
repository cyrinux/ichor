package ichorgo

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestVolumeSnapshotsPagingAndWatchedClaims(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	api, k := newPagingKubeAPI(t, func(w http.ResponseWriter, path string, q url.Values) string {
		switch path {
		case "/apis":
			return `{"groups":[{"name":"snapshot.storage.k8s.io","preferredVersion":{"version":"v1"}}]}`
		case "/apis/snapshot.storage.k8s.io/v1/volumesnapshots":
			if q.Get("continue") == "next" {
				return `{"items":[{"metadata":{"name":"broken","namespace":"shop"},"spec":{"source":{"persistentVolumeClaimName":"old"}},"status":{"readyToUse":false,"error":{"message":"unavailable"}}}]}`
			}
			return `{"metadata":{"continue":"next"},"items":[{"metadata":{"name":"recent","namespace":"shop"},"spec":{"source":{"persistentVolumeClaimName":"orders"}},"status":{"readyToUse":true,"creationTime":"2026-10-01T06:00:00Z"}}]}`
		case "/api/v1/persistentvolumeclaims":
			return `{"items":[{"metadata":{"name":"orders","namespace":"shop"},"status":{"phase":"Bound"}},{"metadata":{"name":"old","namespace":"shop"},"status":{"phase":"Bound"}},{"metadata":{"name":"unwatched","namespace":"shop"},"status":{"phase":"Bound"}},{"metadata":{"name":"opt-in","namespace":"shop","annotations":{"ichor.levis.name/backup":"true"}},"status":{"phase":"Bound"}}]}`
		case "/apis/snapshot.storage.k8s.io/v1/volumesnapshotcontents":
			w.WriteHeader(403)
			return `{"kind":"Status","code":403,"reason":"Forbidden","message":"not allowed"}`
		default:
			return `{"items":[]}`
		}
	})
	r := readVolumeSnapshots(context.Background(), k, "", now)
	if len(r.Snapshots) != 2 || len(r.Claims) != 4 || r.Error != "" || r.ContentsError == "" {
		t.Fatalf("%+v", r)
	}
	for _, c := range r.Claims {
		switch c.Name {
		case "orders":
			if !c.Watched || c.Stale || c.AgeSeconds != 21600 {
				t.Errorf("%+v", c)
			}
		case "old", "opt-in":
			if !c.Watched || !c.Stale {
				t.Errorf("%+v", c)
			}
		case "unwatched":
			if c.Watched || c.Stale {
				t.Errorf("%+v", c)
			}
		}
	}
	found := false
	for _, req := range api.requests() {
		if req.query.Get("continue") == "next" {
			found = true
		}
	}
	if !found {
		t.Fatal("did not read continuation")
	}
}

func TestCreateVolumeSnapshotDefaultClass(t *testing.T) {
	for _, tc := range []struct {
		name, classes string
		wantErr       bool
	}{
		{"matching", `{"items":[{"metadata":{"name":"other","annotations":{"snapshot.storage.kubernetes.io/is-default-class":"true"}},"driver":"other.example.io"},{"metadata":{"name":"matching","annotations":{"snapshot.storage.kubernetes.io/is-default-class":"true"}},"driver":"csi.example.io"}]}`, false},
		{"missing", `{"items":[]}`, true},
		{"ambiguous", `{"items":[{"metadata":{"name":"a","annotations":{"snapshot.storage.kubernetes.io/is-default-class":"true"}},"driver":"csi.example.io"},{"metadata":{"name":"b","annotations":{"snapshot.storage.kubernetes.io/is-default-class":"true"}},"driver":"csi.example.io"}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeKubeAPI(t, map[string]string{
				"GET /apis": `{"groups":[{"name":"snapshot.storage.k8s.io","preferredVersion":{"version":"v1"}}]}`,
				"GET /api/v1/namespaces/shop/persistentvolumeclaims/orders":             `{"spec":{"storageClassName":"csi"},"status":{"phase":"Bound"}}`,
				"GET /apis/storage.k8s.io/v1/storageclasses/csi":                        `{"provisioner":"csi.example.io"}`,
				"GET /apis/snapshot.storage.k8s.io/v1/volumesnapshotclasses":            tc.classes,
				"POST /apis/snapshot.storage.k8s.io/v1/namespaces/shop/volumesnapshots": `{"metadata":{"name":"ichor-snapshot-test"}}`,
			})
			k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := createVolumeSnapshot(context.Background(), k, "shop", "orders")
			if (err != nil) != tc.wantErr {
				t.Fatalf("got %q %v", got, err)
			}
			if !tc.wantErr {
				req := lastRequest(f)
				if got != "ichor-snapshot-test" || req.method != "POST" || !strings.Contains(req.body, `"volumeSnapshotClassName":"matching"`) || !strings.Contains(req.body, `"persistentVolumeClaimName":"orders"`) {
					t.Fatalf("%q %+v", got, req)
				}
			} else if lastRequest(f).method == "POST" {
				t.Fatal("posted after refusal")
			}
		})
	}
}

func TestVolumeSnapshotsAbsent(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`})
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	r := readVolumeSnapshots(context.Background(), k, "", time.Now())
	if r.Installed || r.Error != "" {
		t.Fatalf("%+v", r)
	}
}

func TestCreateVolumeSnapshotRefusesInvalidAndDemo(t *testing.T) {
	if _, err := KubeCreateVolumeSnapshot("", "", "", "", "orders"); err == nil {
		t.Fatal("empty namespace accepted")
	}
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := KubeCreateVolumeSnapshot(cfg, "demo", "", "shop", "orders"); err == nil {
		t.Fatal("demo accepted")
	}
}

func TestBackupJobsReadRetainedRuns(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/batch/v1/cronjobs": `{"items":[{"metadata":{"namespace":"shop","name":"orders-backup"},"spec":{"schedule":"0 1 * * *"},"status":{"lastSuccessfulTime":"2026-09-28T01:00:00Z"}},{"metadata":{"namespace":"shop","name":"cleanup"},"spec":{"schedule":"0 1 * * *"}}]}`,
		"GET /apis/batch/v1/jobs": `{"items":[
 {"metadata":{"namespace":"shop","name":"run-1","ownerReferences":[{"kind":"CronJob","name":"orders-backup"}]},"status":{"startTime":"2026-09-29T01:00:00Z","conditions":[{"type":"Failed","status":"True"}]}},
 {"metadata":{"namespace":"shop","name":"run-2","ownerReferences":[{"kind":"CronJob","name":"orders-backup"}]},"status":{"startTime":"2026-09-30T01:00:00Z","conditions":[{"type":"Failed","status":"True"}]}},
 {"metadata":{"namespace":"shop","name":"run-3","ownerReferences":[{"kind":"CronJob","name":"orders-backup"}]},"status":{"startTime":"2026-10-01T01:00:00Z","conditions":[{"type":"Failed","status":"True"}]}},
 {"metadata":{"namespace":"shop","name":"run-4","ownerReferences":[{"kind":"CronJob","name":"orders-backup"}]},"status":{"startTime":"2026-10-02T01:00:00Z"}},
 {"metadata":{"namespace":"other","name":"unrelated","ownerReferences":[{"kind":"CronJob","name":"orders-backup"}]},"status":{"startTime":"2026-10-01T01:00:00Z","conditions":[{"type":"Complete","status":"True"}]}}
 ]}`,
	})
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	r := readBackupJobs(context.Background(), k, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	if r.Error != "" || r.JobsError != "" || len(r.Jobs) != 1 || r.Jobs[0].FailureStreak != 3 || r.Jobs[0].Health != healthCritical {
		t.Fatalf("%+v", r)
	}
}

func TestBackupJobMatch(t *testing.T) {
	for _, tc := range []struct {
		name, label, annotation string
		want                    bool
	}{
		{"nightly-backup", "", "", true}, {"BACKUP-db", "", "", true},
		{"nightly", "true", "", true}, {"nightly-backup", "true", "false", false},
		{"nightly", "false", "true", true}, {"cleanup", "", "", false},
	} {
		var c cronJobObject
		c.Metadata.Name = tc.name
		c.Metadata.Labels = map[string]string{backupMatchKey: tc.label}
		c.Metadata.Annotations = map[string]string{backupMatchKey: tc.annotation}
		if got := isBackupCronJob(c); got != tc.want {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

func TestBackupFailureStreak(t *testing.T) {
	for _, tc := range []struct {
		runs    []kubeJobRun
		success int64
		want    int
	}{
		{[]kubeJobRun{{State: cronRunFailed, Started: 3}, {State: cronRunRunning, Started: 4}, {State: cronRunFailed, Started: 2}, {State: cronRunSucceeded, Started: 1}}, 0, 2},
		{[]kubeJobRun{{State: cronRunFailed, Started: 3}, {State: cronRunFailed, Started: 2}}, 4, 0},
		{[]kubeJobRun{{State: cronRunFailed, Started: 3}, {State: cronRunFailed, Started: 2}}, 0, 2},
	} {
		if got := backupFailureStreak(tc.runs, tc.success); got != tc.want {
			t.Errorf("got %d want %d", got, tc.want)
		}
	}
}

func TestSnapshotFreshness(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	fresh := snapshotClaim{Watched: true, CreatedAt: now.Add(-time.Hour).UnixMilli()}
	setSnapshotFreshness(&fresh, now)
	if fresh.Stale {
		t.Fatal("new PVC alerted before 48h")
	}
	for _, tc := range []struct {
		at      int64
		watched bool
		stale   bool
	}{
		{now.Add(-49 * time.Hour).UnixMilli(), true, true}, {now.Add(-48 * time.Hour).UnixMilli(), true, true},
		{now.Add(-time.Hour).UnixMilli(), true, false}, {0, true, true}, {0, false, false},
	} {
		row := snapshotClaim{Watched: tc.watched, LastGoodAt: tc.at}
		setSnapshotFreshness(&row, now)
		if row.Stale != tc.stale {
			t.Errorf("%+v => %+v", tc, row)
		}
	}
}
