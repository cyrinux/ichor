package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	veleroNS = "/apis/velero.io/v1/namespaces/velero/"

	veleroDailyScheduleJSON = `{"metadata":{"name":"apps-daily","namespace":"velero","uid":"5c1e0f7a-0000-4000-8000-000000000001",
  "labels":{"team":"platform","tier":"gold"},"annotations":{"owner":"ops"}},
 "spec":{"schedule":"0 2 * * *","useOwnerReferencesInBackup":true,
  "template":{"includedNamespaces":["demo","monitoring"],"storageLocation":"default","ttl":"720h0m0s",
   "metadata":{"labels":{"tier":"silver","backup-kind":"nightly"}}}},
 "status":{"phase":"Enabled"}}`

	veleroScheduleBackupsJSON = `{"items":[
  {"metadata":{"name":"apps-daily-20261002020000","namespace":"velero","labels":{"velero.io/schedule-name":"apps-daily"}},
   "spec":{"storageLocation":"default","includedNamespaces":["demo"]},
   "status":{"phase":"Completed","startTimestamp":"2026-10-02T02:00:00Z","completionTimestamp":"2026-10-02T02:05:00Z","expiration":"2026-11-01T02:00:00Z",
    "progress":{"totalItems":120,"itemsBackedUp":120},"volumeSnapshotsAttempted":2,"volumeSnapshotsCompleted":2,"csiVolumeSnapshotsCompleted":1}},
  {"metadata":{"name":"apps-daily-20261003020000","namespace":"velero","labels":{"velero.io/schedule-name":"apps-daily"}},
   "status":{"phase":"PartiallyFailed","startTimestamp":"2026-10-03T02:00:00Z","completionTimestamp":"2026-10-03T02:06:00Z","errors":2,"warnings":1}},
  {"metadata":{"name":"apps-daily-20261001020000","namespace":"velero","labels":{"velero.io/schedule-name":"apps-daily"}},
   "status":{"phase":"Completed","startTimestamp":"2026-10-01T02:00:00Z","completionTimestamp":"2026-10-01T02:05:00Z"}},
  {"metadata":{"name":"apps-daily-20260930020000","namespace":"velero","labels":{"velero.io/schedule-name":"apps-daily"}},
   "status":{"phase":"Completed","startTimestamp":"2026-09-30T02:00:00Z","completionTimestamp":"2026-09-30T02:05:00Z"}}]}`

	veleroDeleteRequestsJSON = `{"items":[
  {"metadata":{"name":"apps-daily-20261001020000-x7k2p","creationTimestamp":"2026-10-03T09:00:00Z"},"spec":{"backupName":"apps-daily-20261001020000"},"status":{"phase":"InProgress"}},
  {"metadata":{"name":"apps-daily-20260930020000-a1","creationTimestamp":"2026-10-02T09:00:00Z"},"spec":{"backupName":"apps-daily-20260930020000"},
   "status":{"phase":"Processed","errors":["old error"]}},
  {"metadata":{"name":"apps-daily-20260930020000-b2","creationTimestamp":"2026-10-03T09:00:00Z"},"spec":{"backupName":"apps-daily-20260930020000"},
   "status":{"phase":"Processed","errors":["backup deletion failed: backup storage location default is read-only"]}}]}`
)

// veleroClient is a kube client over a fake API answering the given paths.
func veleroClient(t *testing.T, answers map[string]string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func lastPost(t *testing.T, f *fakeKubeAPI) fakeKubeRequest {
	t.Helper()

	for _, r := range slices.Backward(f.recorded()) {
		if r.method == http.MethodPost {
			return r
		}
	}

	t.Fatal("nothing posted")

	return fakeKubeRequest{}
}

func TestVeleroBackupsList(t *testing.T) {
	f, k := veleroClient(t, map[string]string{
		"GET " + veleroNS + "backups":              veleroScheduleBackupsJSON,
		"GET " + veleroNS + "deletebackuprequests": veleroDeleteRequestsJSON,
	})

	r, err := readVeleroBackups(context.Background(), k, "velero", "apps-daily")
	if err != nil || r.Error != "" {
		t.Fatalf("%+v %v", r, err)
	}

	var names []string
	for _, b := range r.Backups {
		names = append(names, b.Name)
	}
	if !slices.Equal(names, []string{"apps-daily-20261003020000", "apps-daily-20261002020000", "apps-daily-20261001020000", "apps-daily-20260930020000"}) {
		t.Fatalf("newest first: %v", names)
	}

	full := r.Backups[1]
	if full.ExpiresAt != time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC).UnixMilli() || full.TotalItems != 120 || full.ItemsBackedUp != 120 ||
		full.VolumeSnapshots != 2 || full.CSISnapshots != 1 || !full.Finished || full.Running || full.Deleting ||
		!slices.Equal(full.IncludedNamespaces, []string{"demo"}) || full.StorageLocation != "default" {
		t.Errorf("full %+v", full)
	}

	if p := r.Backups[0]; p.Phase != "PartiallyFailed" || p.Errors != 2 || p.ExpiresAt != 0 || p.IncludedNamespaces == nil {
		t.Errorf("partial %+v", p)
	}

	// A pending request marks the backup deleting; the latest failed one gives its errors.
	if d := r.Backups[2]; !d.Deleting || len(d.DeleteErrors) != 0 {
		t.Errorf("deleting %+v", d)
	}
	if d := r.Backups[3]; d.Deleting || !slices.Equal(d.DeleteErrors, []string{"backup deletion failed: backup storage location default is read-only"}) {
		t.Errorf("failed deletion %+v", d)
	}

	// The two lists are read in parallel: find the backups one by its path.
	selected := slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool {
		return strings.HasSuffix(r.path, "/backups") && strings.Contains(r.query, "velero.io%2Fschedule-name%3Dapps-daily")
	})
	if !selected {
		t.Errorf("schedule selector missing in %+v", f.recorded())
	}
}

// TestVeleroBackupsAdhocAndRefusedRequests: no schedule selects the backups without the
// label; deletion requests off limits leave the list with an error.
func TestVeleroBackupsAdhocAndRefusedRequests(t *testing.T) {
	f, k := veleroClient(t, map[string]string{"GET " + veleroNS + "backups": `{"items":[]}`})
	f.answerWith("GET "+veleroNS+"deletebackuprequests", http.StatusForbidden, forbiddenStatus)

	r, err := readVeleroBackups(context.Background(), k, "velero", "")
	if err != nil || r.Error == "" || r.Backups == nil {
		t.Fatalf("%+v %v", r, err)
	}

	found := false
	for _, req := range f.recorded() {
		if strings.HasSuffix(req.path, "/backups") && strings.Contains(req.query, "%21velero.io%2Fschedule-name") {
			found = true
		}
	}
	if !found {
		t.Errorf("no not-scheduled selector in %+v", f.recorded())
	}

	// No Velero: the list read fails.
	_, k2 := veleroClient(t, map[string]string{})
	if _, err := readVeleroBackups(context.Background(), k2, "velero", "apps-daily"); err == nil {
		t.Error("missing Velero read")
	}
}

func TestVeleroBackupNow(t *testing.T) {
	f, k := veleroClient(t, map[string]string{
		"GET " + veleroNS + "schedules/apps-daily": veleroDailyScheduleJSON,
		"GET " + veleroNS + "backups":              `{"items":[]}`,
		"POST " + veleroNS + "backups":             `{"metadata":{"name":"apps-daily-20261004120000"}}`,
	})

	name, err := backupVeleroSchedule(context.Background(), k, "velero", "apps-daily", time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	if err != nil || name != "apps-daily-20261004120000" {
		t.Fatalf("%q %v", name, err)
	}

	var body struct {
		Kind     string
		Metadata struct {
			Name            string
			Labels          map[string]string
			Annotations     map[string]string
			OwnerReferences []struct {
				Kind, Name, UID string
				Controller      bool
			}
		}
		Spec map[string]any
	}
	if err := json.Unmarshal([]byte(lastPost(t, f).body), &body); err != nil {
		t.Fatal(err)
	}

	// The template sets labels: those (not the schedule's), plus the name label; it sets no
	// annotations: the schedule's.
	if body.Kind != "Backup" || body.Metadata.Name != "apps-daily-20261004120000" ||
		body.Metadata.Labels["velero.io/schedule-name"] != "apps-daily" || body.Metadata.Labels["team"] != "" ||
		body.Metadata.Labels["tier"] != "silver" || body.Metadata.Labels["backup-kind"] != "nightly" || body.Metadata.Annotations["owner"] != "ops" {
		t.Errorf("metadata %+v", body.Metadata)
	}

	if refs := body.Metadata.OwnerReferences; len(refs) != 1 || refs[0].Kind != "Schedule" || refs[0].UID == "" || !refs[0].Controller {
		t.Errorf("owner %+v", refs)
	}

	if body.Spec["storageLocation"] != "default" || body.Spec["ttl"] != "720h0m0s" || len(body.Spec["includedNamespaces"].([]any)) != 2 {
		t.Errorf("spec %+v", body.Spec)
	}
}

// TestVeleroBackupFromScheduleMetadata: without template labels the schedule's own are kept;
// without any annotations none are sent.
func TestVeleroBackupFromScheduleMetadata(t *testing.T) {
	var s veleroScheduleRaw
	s.Metadata.Name = "apps-daily"
	s.Metadata.Labels = map[string]string{"team": "platform"}
	s.Spec.Template = json.RawMessage(`{"includedNamespaces":["demo"],"metadata":{"annotations":{"reason":"nightly"}}}`)

	b, err := veleroBackupFromSchedule(s, "velero", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	meta := b["metadata"].(map[string]any)
	labels, annotations := meta["labels"].(map[string]any), meta["annotations"].(map[string]any)
	if labels["team"] != "platform" || labels["velero.io/schedule-name"] != "apps-daily" || annotations["reason"] != "nightly" {
		t.Errorf("metadata %+v", meta)
	}

	s.Spec.Template = nil
	s.Metadata.Labels = nil
	b, _ = veleroBackupFromSchedule(s, "velero", time.Now())
	if meta := b["metadata"].(map[string]any); meta["annotations"] != nil || len(meta["labels"].(map[string]any)) != 1 {
		t.Errorf("bare metadata %+v", meta)
	}
}

func TestVeleroBackupNowRefusals(t *testing.T) {
	running := `{"items":[{"metadata":{"name":"apps-daily-1","labels":{"velero.io/schedule-name":"apps-daily"}},"status":{"phase":"InProgress"}}]}`
	paused := strings.Replace(veleroDailyScheduleJSON, `"useOwnerReferencesInBackup":true`, `"paused":true`, 1)
	invalid := strings.Replace(veleroDailyScheduleJSON, `"phase":"Enabled"`, `"phase":"FailedValidation"`, 1)

	for _, tc := range []struct {
		name, schedule, backups string
		want                    error
	}{
		{"paused", paused, `{"items":[]}`, errVeleroPaused},
		{"invalid", invalid, `{"items":[]}`, errVeleroInvalid},
		{"running", veleroDailyScheduleJSON, running, errVeleroBackingUp},
	} {
		f, k := veleroClient(t, map[string]string{
			"GET " + veleroNS + "schedules/apps-daily": tc.schedule,
			"GET " + veleroNS + "backups":              tc.backups,
		})

		if _, err := backupVeleroSchedule(context.Background(), k, "velero", "apps-daily", time.Now()); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}

		for _, r := range f.recorded() {
			if r.method == http.MethodPost {
				t.Errorf("%s: posted %s", tc.name, r.path)
			}
		}
	}

	// A schedule that does not exist is the API's 404.
	_, k := veleroClient(t, map[string]string{})
	var apiErr *kubeAPIError
	if _, err := backupVeleroSchedule(context.Background(), k, "velero", "gone", time.Now()); !errors.As(err, &apiErr) || apiErr.Code != 404 {
		t.Errorf("missing schedule: %v", err)
	}
}

func TestVeleroDeleteBackup(t *testing.T) {
	f, k := veleroClient(t, map[string]string{
		"GET " + veleroNS + "backups/apps-daily-20261002020000": `{"metadata":{"name":"apps-daily-20261002020000","uid":"9b0c-uid"},"status":{"phase":"Completed"}}`,
		"GET " + veleroNS + "deletebackuprequests":              `{"items":[]}`,
		"POST " + veleroNS + "deletebackuprequests":             `{"metadata":{"name":"apps-daily-20261002020000-q8w4z"}}`,
	})

	name, err := deleteVeleroBackup(context.Background(), k, "velero", "apps-daily-20261002020000")
	if err != nil || name != "apps-daily-20261002020000-q8w4z" {
		t.Fatalf("%q %v", name, err)
	}

	var body struct {
		Kind     string
		Metadata struct {
			GenerateName string
			Labels       map[string]string
		}
		Spec struct{ BackupName string }
	}
	if err := json.Unmarshal([]byte(lastPost(t, f).body), &body); err != nil {
		t.Fatal(err)
	}

	if body.Kind != "DeleteBackupRequest" || body.Metadata.GenerateName != "apps-daily-20261002020000-" ||
		body.Metadata.Labels["velero.io/backup-name"] != "apps-daily-20261002020000" || body.Metadata.Labels["velero.io/backup-uid"] != "9b0c-uid" ||
		body.Spec.BackupName != "apps-daily-20261002020000" {
		t.Errorf("request %+v", body)
	}
}

func TestVeleroDeleteBackupRefusals(t *testing.T) {
	pendingReq := `{"items":[{"metadata":{"name":"r1"},"spec":{"backupName":"b1"},"status":{"phase":"New"}}]}`
	failedReq := `{"items":[{"metadata":{"name":"r1"},"spec":{"backupName":"b1"},"status":{"phase":"Processed","errors":["x"]}}]}`

	for _, tc := range []struct {
		name, phase, requests string
		want                  error
	}{
		{"running", "InProgress", `{"items":[]}`, errVeleroStillRunning},
		{"finalizing", "FinalizingPartiallyFailed", `{"items":[]}`, errVeleroStillRunning},
		{"deleting", "Deleting", `{"items":[]}`, errVeleroDeleting},
		{"pending request", "Failed", pendingReq, errVeleroDeleting},
		{"failed request retried", "Failed", failedReq, nil},
	} {
		f, k := veleroClient(t, map[string]string{
			"GET " + veleroNS + "backups/b1":            `{"metadata":{"uid":"u1"},"status":{"phase":"` + tc.phase + `"}}`,
			"GET " + veleroNS + "deletebackuprequests":  tc.requests,
			"POST " + veleroNS + "deletebackuprequests": `{"metadata":{"name":"b1-z"}}`,
		})

		_, err := deleteVeleroBackup(context.Background(), k, "velero", "b1")
		if !errors.Is(err, tc.want) && (tc.want != nil || err != nil) {
			t.Errorf("%s: %v", tc.name, err)
		}

		posted := slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.method == http.MethodPost })
		if posted != (tc.want == nil) {
			t.Errorf("%s: posted %v", tc.name, posted)
		}
	}
}

func TestVeleroLabelValue(t *testing.T) {
	if got := veleroLabelValue("apps-daily"); got != "apps-daily" {
		t.Errorf("short %q", got)
	}

	long := strings.Repeat("a", 70)
	if got := veleroLabelValue(long); len(got) != kubeLabelValueMax || !strings.HasPrefix(got, strings.Repeat("a", 57)) || got == long[:63] {
		t.Errorf("long %q", got)
	}
}

func TestVeleroActionsDemoAndValidation(t *testing.T) {
	cfg, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubeVeleroBackupNow(cfg, demoKubeContext, "", "velero", "apps-daily"); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo backup now: %v", err)
	}

	if _, err := KubeVeleroDeleteBackup(cfg, demoKubeContext, "", "velero", "apps-daily-20261003020000"); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo delete: %v", err)
	}

	out, err := KubeVeleroBackups(cfg, demoKubeContext, "", "velero", "apps-daily")
	if err != nil {
		t.Fatal(err)
	}

	var list veleroBackupList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	deleting := slices.ContainsFunc(list.Backups, func(b veleroBackupDetail) bool { return b.Deleting })
	if len(list.Backups) < 3 || !deleting || list.Schedule != "apps-daily" {
		t.Errorf("demo list %+v", list)
	}

	for _, bad := range [][2]string{{"velero", "../x"}, {"", "apps-daily"}} {
		if _, err := KubeVeleroBackupNow("", "", "", bad[0], bad[1]); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
