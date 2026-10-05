package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const testCronJobs = `{"items":[
{"metadata":{"name":"db-backup","namespace":"shop","uid":"u1","creationTimestamp":"2026-01-01T00:00:00Z",
  "labels":{"ichor.levis.name/icon":"postgresql"},
  "annotations":{"ichor.levis.name/title":"Database backup","ichor.levis.name/description":"Dumps the shop database to S3"}},
 "spec":{"schedule":"0 3 * * *","timeZone":"UTC","jobTemplate":{"spec":{"template":{"spec":{"containers":[{"image":"postgres:17"}]}}}}},
 "status":{"lastScheduleTime":"2026-10-03T03:00:00Z","lastSuccessfulTime":"2026-10-03T03:02:00Z"}},
{"metadata":{"name":"cleanup","namespace":"shop","uid":"u2","labels":{"ichor.levis.name/trigger":"false"}},
 "spec":{"schedule":"*/30 * * * *","suspend":true,"jobTemplate":{"spec":{"template":{"spec":{"containers":[{"image":"busybox:1.37"}]}}}}},
 "status":{}},
{"metadata":{"name":"renew","namespace":"infra","uid":"u3"},
 "spec":{"schedule":"@hourly","jobTemplate":{"spec":{"template":{"spec":{"containers":[{"image":"ghcr.io/example/velero-plugin:1"}]}}}}},
 "status":{"active":[{"name":"renew-29000000"}]}}
]}`

const testJobs = `{"items":[
{"metadata":{"name":"db-backup-29000001","namespace":"shop","creationTimestamp":"2026-10-03T03:00:00Z","ownerReferences":[{"kind":"CronJob","name":"db-backup"}]},
 "status":{"startTime":"2026-10-03T03:00:01Z","completionTime":"2026-10-03T03:02:00Z","succeeded":1,"conditions":[{"type":"Complete","status":"True"}]}},
{"metadata":{"name":"db-backup-manual-abcde","namespace":"shop","creationTimestamp":"2026-10-03T10:00:00Z",
  "annotations":{"cronjob.kubernetes.io/instantiate":"manual"},"ownerReferences":[{"kind":"CronJob","name":"db-backup"}]},
 "status":{"startTime":"2026-10-03T10:00:01Z","failed":1,"conditions":[{"type":"Failed","status":"True","lastTransitionTime":"2026-10-03T10:01:00Z"}]}},
{"metadata":{"name":"renew-29000000","namespace":"infra","creationTimestamp":"2026-10-03T12:00:00Z","ownerReferences":[{"kind":"CronJob","name":"renew"}]},
 "status":{"startTime":"2026-10-03T12:00:01Z","active":1}},
{"metadata":{"name":"standalone","namespace":"shop"},"status":{"succeeded":1}}
]}`

func TestListCronJobs(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/batch/v1/cronjobs": testCronJobs,
		"GET /apis/batch/v1/jobs":     testJobs,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 3, 12, 30, 0, 0, time.UTC)

	list, err := listCronJobs(context.Background(), k, now)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.CronJobs) != 3 {
		t.Fatalf("got %d cronjobs", len(list.CronJobs))
	}

	// Sorted by namespace then name.
	renew, cleanup, backup := list.CronJobs[0], list.CronJobs[1], list.CronJobs[2]
	if renew.Name != "renew" || cleanup.Name != "cleanup" || backup.Name != "db-backup" {
		t.Fatalf("order %s %s %s", renew.Name, cleanup.Name, backup.Name)
	}

	if backup.Title != "Database backup" || backup.Description != "Dumps the shop database to S3" || backup.Icon != "postgresql" || !backup.Triggerable {
		t.Fatalf("backup labels %+v", backup)
	}

	if backup.State != cronRunFailed || len(backup.Runs) != 2 || !backup.Runs[0].Manual || backup.Runs[0].State != cronRunFailed || backup.Runs[1].State != cronRunSucceeded {
		t.Fatalf("backup runs %+v", backup)
	}

	if backup.Runs[1].Finished != time.Date(2026, 10, 3, 3, 2, 0, 0, time.UTC).UnixMilli() || backup.Runs[0].Finished != time.Date(2026, 10, 3, 10, 1, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("finish times %+v", backup.Runs)
	}

	if backup.NextRun != time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("next run %d", backup.NextRun)
	}

	// No icon label: the default (the base image names nothing), not triggerable, suspended: no next run.
	if cleanup.Triggerable || !cleanup.Suspended || cleanup.NextRun != 0 || cleanup.Icon != "" || cleanup.RemoteIcon != "" || cleanup.State != cronRunNever {
		t.Fatalf("cleanup %+v", cleanup)
	}

	// No icon label: guessed from the image.
	if renew.Icon != "velero" || renew.State != cronRunRunning || renew.Active != 1 {
		t.Fatalf("renew %+v", renew)
	}
}

func TestCronJobIcon(t *testing.T) {
	cases := []struct {
		label, icon, remote string
	}{
		{"longhorn", "longhorn", ""},
		{" Longhorn ", "longhorn", ""},
		{"kimai", "", "kimai"},    // a dashboard-icons slug without a bundled icon
		{"../etc/passwd", "", ""}, // not a slug: ignored, the default icon
		{"https://x.invalid/a.png", "", ""},
	}

	for _, c := range cases {
		icon, remote := cronJobIcon(c.label, "", "", nil)
		if icon != c.icon || remote != c.remote {
			t.Errorf("%q: got %q %q", c.label, icon, remote)
		}
	}
}

func TestTriggerCronJob(t *testing.T) {
	const cronPath = "/apis/batch/v1/namespaces/shop/cronjobs/db-backup"

	f := newFakeKubeAPI(t, map[string]string{
		"GET " + cronPath: `{"metadata":{"name":"db-backup","namespace":"shop","uid":"u1"},
		  "spec":{"schedule":"0 3 * * *","suspend":true,"jobTemplate":{
		    "metadata":{"labels":{"app":"backup"},"annotations":{"note":"x"}},
		    "spec":{"backoffLimit":2,"template":{"spec":{"restartPolicy":"Never","containers":[{"name":"dump","image":"postgres:17"}]}}}}}}`,
		"POST /apis/batch/v1/namespaces/shop/jobs": `{"metadata":{"name":"db-backup-manual-x7k2p"}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	job, err := triggerCronJob(context.Background(), k, "shop", "db-backup")
	if err != nil || job != "db-backup-manual-x7k2p" {
		t.Fatalf("got %q %v", job, err)
	}

	reqs := f.recorded()
	post := reqs[len(reqs)-1]

	var body struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			GenerateName    string            `json:"generateName"`
			Namespace       string            `json:"namespace"`
			Labels          map[string]string `json:"labels"`
			Annotations     map[string]string `json:"annotations"`
			OwnerReferences []struct {
				APIVersion string `json:"apiVersion"`
				Kind       string `json:"kind"`
				Name       string `json:"name"`
				UID        string `json:"uid"`
				Controller bool   `json:"controller"`
			} `json:"ownerReferences"`
		} `json:"metadata"`
		Spec struct {
			BackoffLimit int `json:"backoffLimit"`
			Template     struct {
				Spec struct {
					Containers []struct{ Image string } `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(post.body), &body); err != nil {
		t.Fatal(err)
	}

	m := body.Metadata
	if body.APIVersion != "batch/v1" || body.Kind != "Job" || m.GenerateName != "db-backup-manual-" || m.Namespace != "shop" {
		t.Fatalf("job %s", post.body)
	}

	if m.Labels["app"] != "backup" || m.Annotations["note"] != "x" || m.Annotations[cronInstantiateAnnotation] != "manual" {
		t.Fatalf("metadata %s", post.body)
	}

	if len(m.OwnerReferences) != 1 || m.OwnerReferences[0].UID != "u1" || m.OwnerReferences[0].Kind != "CronJob" || !m.OwnerReferences[0].Controller {
		t.Fatalf("owner %s", post.body)
	}

	if body.Spec.BackoffLimit != 2 || len(body.Spec.Template.Spec.Containers) != 1 || body.Spec.Template.Spec.Containers[0].Image != "postgres:17" {
		t.Fatalf("spec %s", post.body)
	}
}

func TestTriggerCronJobRefusesDisabled(t *testing.T) {
	const cronPath = "/apis/batch/v1/namespaces/shop/cronjobs/wipe"

	f := newFakeKubeAPI(t, map[string]string{
		"GET " + cronPath:                          `{"metadata":{"name":"wipe","uid":"u","annotations":{"ichor.levis.name/trigger":"false"}},"spec":{"jobTemplate":{"spec":{}}}}`,
		"POST /apis/batch/v1/namespaces/shop/jobs": `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := triggerCronJob(context.Background(), k, "shop", "wipe"); !errors.Is(err, errCronJobTriggerDisabled) {
		t.Fatalf("got %v", err)
	}

	for _, r := range f.recorded() {
		if r.method == "POST" {
			t.Fatal("a disabled cronjob was triggered")
		}
	}
}

func TestManualJobPrefix(t *testing.T) {
	long := strings.Repeat("a", 60)
	if got := manualJobPrefix(long); len(got)+5 > 63 || !strings.HasSuffix(got, "-manual-") {
		t.Fatalf("got %q", got)
	}

	if got := manualJobPrefix(strings.Repeat("a", 49) + "-b"); got != strings.Repeat("a", 49)+"-manual-" {
		t.Fatalf("got %q", got)
	}
}

func TestKubeCronJobsDemoAndValidation(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeCronJobs(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var list kubeCronJobList
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.CronJobs) == 0 {
		t.Fatalf("demo cronjobs: %v %s", err, out)
	}

	if _, err := KubeTriggerCronJob(cfg, "", "", "demo", "db-backup"); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}

	if _, err := KubeTriggerCronJob("", "", "", "../x", "y"); err == nil {
		t.Fatal("expected an invalid name to be refused")
	}
}
