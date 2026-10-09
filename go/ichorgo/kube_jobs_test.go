package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const jobsBody = `{"items":[
	{"metadata":{"name":"backup-29331540","namespace":"ops","creationTimestamp":"2026-10-09T03:00:00Z",
	  "ownerReferences":[{"kind":"CronJob","name":"backup"}]},
	 "status":{"startTime":"2026-10-09T03:00:00Z","completionTime":"2026-10-09T03:01:30Z","succeeded":1,
	  "conditions":[{"type":"Complete","status":"True","lastTransitionTime":"2026-10-09T03:01:30Z"}]}},
	{"metadata":{"name":"backup-manual-abcde","namespace":"ops","creationTimestamp":"2026-10-09T10:00:00Z",
	  "annotations":{"cronjob.kubernetes.io/instantiate":"manual"},"ownerReferences":[{"kind":"CronJob","name":"backup"}]},
	 "status":{"startTime":"2026-10-09T10:00:00Z",
	  "conditions":[{"type":"Failed","status":"True","reason":"BackoffLimitExceeded","lastTransitionTime":"2026-10-09T10:05:00Z"}]}},
	{"metadata":{"name":"migrate","namespace":"ops","creationTimestamp":"2026-10-09T11:50:00Z"},
	 "spec":{"completions":3},"status":{"startTime":"2026-10-09T11:50:00Z","succeeded":1}},
	{"metadata":{"name":"import","namespace":"ops","creationTimestamp":"2026-10-09T09:00:00Z"},
	 "spec":{"suspend":true},"status":{}}]}`

var jobsNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func jobNamed(t *testing.T, jobs kubeJobs, name string) jobRow {
	t.Helper()

	for _, j := range jobs.Jobs {
		if j.Name == name {
			return j
		}
	}

	t.Fatalf("no job %q in %+v", name, jobs.Jobs)

	return jobRow{}
}

func TestJobsStateDurationOwnerAndOrder(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis/batch/v1/namespaces/ops/jobs": jobsBody})

	jobs, err := readJobs(context.Background(), openFakeKube(t, f), "ops", jobsNow)
	if err != nil {
		t.Fatal(err)
	}

	done := jobNamed(t, jobs, "backup-29331540")
	if done.State != cronRunSucceeded || done.Owner != "backup" || done.Duration != 90_000 || done.Completions != "1/1" || done.Level != storageOK {
		t.Errorf("succeeded job %+v", done)
	}

	failed := jobNamed(t, jobs, "backup-manual-abcde")
	if failed.State != cronRunFailed || !failed.Manual || failed.Reason != "BackoffLimitExceeded" || failed.Duration != 300_000 || failed.Level != storageCritical {
		t.Errorf("failed job %+v", failed)
	}

	// Still running: its duration runs up to the read; no owner of its own.
	running := jobNamed(t, jobs, "migrate")
	if running.State != cronRunRunning || running.Owner != "" || running.Duration != 600_000 || running.Completions != "1/3" || running.Finished != 0 {
		t.Errorf("running job %+v", running)
	}

	if held := jobNamed(t, jobs, "import"); held.State != jobSuspended || held.Duration != 0 || held.Level != storageWarning {
		t.Errorf("suspended job %+v", held)
	}

	var order []string
	for _, j := range jobs.Jobs {
		order = append(order, j.Name)
	}

	if strings.Join(order, ",") != "backup-manual-abcde,import,migrate,backup-29331540" {
		t.Errorf("order %v", order)
	}
}

func TestJobsRefusedIsAnError(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	f.answerWith("GET /apis/batch/v1/namespaces/ops/jobs", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)

	if _, err := readJobs(context.Background(), openFakeKube(t, f), "ops", jobsNow); err == nil {
		t.Fatal("no error")
	}
}

func TestKubeJobsDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeJobs(cfg, "", "", "demo")
	if err != nil {
		t.Fatal(err)
	}

	var jobs kubeJobs
	if err := json.Unmarshal([]byte(out), &jobs); err != nil || len(jobs.Jobs) == 0 {
		t.Fatalf("demo: %v %s", err, out)
	}

	for _, j := range jobs.Jobs {
		if j.Namespace != "demo" {
			t.Errorf("job of %s in the demo namespace's list", j.Namespace)
		}
	}

	// The CronJobs demo's failed weekly report comes first.
	if first := jobs.Jobs[0]; first.Owner != "report-mailer" || first.Level != storageCritical {
		t.Errorf("demo first %+v", first)
	}

	if _, err := KubeJobs(cfg, "", "", "Bad Namespace"); err == nil {
		t.Error("a bad namespace was accepted")
	}
}
