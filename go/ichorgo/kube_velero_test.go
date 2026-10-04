package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// Shaped like Velero's objects on a real cluster (fixtureNow is 2026-10-03 12:00 UTC): daily,
// weekly and hourly schedules, the backups they made (labelled velero.io/schedule-name) and
// some taken by hand, two storage locations.
const (
	veleroSchedulesFixture = `{"items":[
  {"metadata":{"name":"daily","namespace":"velero","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"schedule":"0 2 * * *","template":{"includedNamespaces":["app"]}},"status":{"phase":"Enabled","lastBackup":"2026-10-03T02:00:00Z"}},
  {"metadata":{"name":"partial","namespace":"velero","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"schedule":"@daily","template":{"storageLocation":"default"}},"status":{"phase":"Enabled"}},
  {"metadata":{"name":"failing","namespace":"velero","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"schedule":"0 3 * * *"},"status":{"phase":"Enabled"}},
  {"metadata":{"name":"offsite","namespace":"velero","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"schedule":"0 4 * * 0","template":{"storageLocation":"offsite"}},"status":{"phase":"Enabled"}},
  {"metadata":{"name":"stale","namespace":"velero","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"schedule":"0 1 * * *"},"status":{"phase":"Enabled"}},
  {"metadata":{"name":"fresh","namespace":"velero","creationTimestamp":"2026-10-03T10:00:00Z"},
   "spec":{"schedule":"0 1 * * *"},"status":{"phase":"Enabled"}},
  {"metadata":{"name":"invalid","namespace":"velero","creationTimestamp":"2026-10-03T10:00:00Z"},
   "spec":{"schedule":"not a cron"},"status":{"phase":"FailedValidation","validationErrors":["invalid schedule: expected 5 fields"]}},
  {"metadata":{"name":"paused","namespace":"velero","creationTimestamp":"2026-01-01T00:00:00Z"},
   "spec":{"schedule":"0 1 * * *","paused":true},"status":{"phase":"Enabled"}}]}`

	veleroBackupsFixture = `{"items":[
  {"metadata":{"name":"daily-20261002020000","namespace":"velero","labels":{"velero.io/schedule-name":"daily"}},
   "status":{"phase":"Completed","startTimestamp":"2026-10-02T02:00:00Z","completionTimestamp":"2026-10-02T02:05:00Z"}},
  {"metadata":{"name":"daily-20261003020000","namespace":"velero","labels":{"velero.io/schedule-name":"daily"}},
   "status":{"phase":"Completed","startTimestamp":"2026-10-03T02:00:00Z","completionTimestamp":"2026-10-03T02:04:00Z","warnings":1}},
  {"metadata":{"name":"daily-20261003110000","namespace":"velero","labels":{"velero.io/schedule-name":"daily"}},
   "status":{"phase":"InProgress","startTimestamp":"2026-10-03T11:00:00Z"}},
  {"metadata":{"name":"partial-20261003000000","namespace":"velero","labels":{"velero.io/schedule-name":"partial"}},
   "status":{"phase":"PartiallyFailed","startTimestamp":"2026-10-03T00:00:00Z","completionTimestamp":"2026-10-03T00:10:00Z","errors":2,"warnings":3}},
  {"metadata":{"name":"partial-20261002000000","namespace":"velero","labels":{"velero.io/schedule-name":"partial"}},
   "status":{"phase":"Completed","startTimestamp":"2026-10-02T00:00:00Z","completionTimestamp":"2026-10-02T00:10:00Z"}},
  {"metadata":{"name":"failing-20261003030000","namespace":"velero","labels":{"velero.io/schedule-name":"failing"}},
   "status":{"phase":"Failed","startTimestamp":"2026-10-03T03:00:00Z","completionTimestamp":"2026-10-03T03:01:00Z","failureReason":"error getting backup store"}},
  {"metadata":{"name":"failing-20261002030000","namespace":"velero","labels":{"velero.io/schedule-name":"failing"}},
   "status":{"phase":"Completed","startTimestamp":"2026-10-02T03:00:00Z","completionTimestamp":"2026-10-02T03:02:00Z"}},
  {"metadata":{"name":"failing-20261001030000","namespace":"velero","labels":{"velero.io/schedule-name":"failing"}},
   "status":{"phase":"Deleting","startTimestamp":"2026-10-01T03:00:00Z","completionTimestamp":"2026-10-01T03:02:00Z"}},
  {"metadata":{"name":"offsite-20260927040000","namespace":"velero","labels":{"velero.io/schedule-name":"offsite"}},
   "status":{"phase":"Completed","startTimestamp":"2026-09-27T04:00:00Z","completionTimestamp":"2026-09-27T04:30:00Z"}},
  {"metadata":{"name":"stale-20260928010000","namespace":"velero","labels":{"velero.io/schedule-name":"stale"}},
   "status":{"phase":"Completed","startTimestamp":"2026-09-28T01:00:00Z","completionTimestamp":"2026-09-28T01:03:00Z"}},
  {"metadata":{"name":"before-upgrade","namespace":"velero"},
   "status":{"phase":"PartiallyFailed","startTimestamp":"2026-10-01T09:00:00Z","completionTimestamp":"2026-10-01T09:20:00Z","errors":1}},
  {"metadata":{"name":"manual-ok","namespace":"velero"},
   "status":{"phase":"Completed","startTimestamp":"2026-10-02T09:00:00Z","completionTimestamp":"2026-10-02T09:20:00Z"}},
  {"metadata":{"name":"manual-old","namespace":"velero"},
   "status":{"phase":"Failed","startTimestamp":"2026-09-01T09:00:00Z","completionTimestamp":"2026-09-01T09:01:00Z"}},
  {"metadata":{"name":"manual-invalid","namespace":"velero","creationTimestamp":"2026-10-03T08:00:00Z"},
   "spec":{"storageLocation":"missing"},"status":{"phase":"FailedValidation"}}]}`

	veleroLocationsFixture = `{"items":[
  {"metadata":{"name":"default","namespace":"velero"},
   "spec":{"provider":"aws","default":true,"objectStorage":{"bucket":"backups"}},
   "status":{"phase":"Available","lastValidationTime":"2026-10-03T11:59:00Z"}},
  {"metadata":{"name":"offsite","namespace":"velero"},
   "spec":{"provider":"aws","objectStorage":{"bucket":"offsite"}},
   "status":{"phase":"Unavailable","lastValidationTime":"2026-10-03T11:58:00Z","message":"BackupStorageLocation \"offsite\" is unavailable: rpc error"}}]}`
)

func veleroFixtures(t *testing.T) ([]veleroScheduleObject, []veleroBackupObject, []veleroLocationObject) {
	t.Helper()

	var (
		schedules kubeList[veleroScheduleObject]
		backups   kubeList[veleroBackupObject]
		locations kubeList[veleroLocationObject]
	)

	for raw, into := range map[string]any{veleroSchedulesFixture: &schedules, veleroBackupsFixture: &backups, veleroLocationsFixture: &locations} {
		if err := json.Unmarshal([]byte(raw), into); err != nil {
			t.Fatal(err)
		}
	}

	return schedules.Items, backups.Items, locations.Items
}

func TestMapVelero(t *testing.T) {
	schedules, backups, locations := veleroFixtures(t)
	out := mapVelero(schedules, backups, locations, fixtureNow)

	type summary struct {
		Name, Health, Last, Location string
		InProgress                   bool
		Reasons                      []string
	}

	var got []summary

	for _, s := range out.Schedules {
		last := ""
		if s.LastBackup != nil {
			last = s.LastBackup.Name
		}

		got = append(got, summary{s.Name, s.Health, last, s.StorageLocation, s.InProgress, s.Reasons})
	}

	want := []summary{
		{"failing", healthCritical, "failing-20261003030000", "default", false, []string{veleroReasonFailed}},
		{"offsite", healthCritical, "offsite-20260927040000", "offsite", false, []string{veleroReasonLocation}},
		{"invalid", healthWarning, "", "default", false, []string{veleroReasonInvalid}},
		{"partial", healthWarning, "partial-20261003000000", "default", false, []string{veleroReasonPartial}},
		{"stale", healthWarning, "stale-20260928010000", "default", false, []string{veleroReasonStale}},
		{"daily", healthOK, "daily-20261003020000", "default", true, []string{}},
		{"fresh", healthOK, "", "default", false, []string{}},
		{"paused", healthIdle, "", "default", false, []string{}},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("schedules differ")
	}

	// The latest finished backup, with its counts; the last success is the latest Completed one.
	partial := out.Schedules[3]
	if b := partial.LastBackup; b.Phase != "PartiallyFailed" || b.Errors != 2 || b.Warnings != 3 || b.CompletedAt != unixMilli("2026-10-03T00:10:00Z") {
		t.Errorf("partial last backup: %+v", b)
	}

	if partial.LastSuccessAt != unixMilli("2026-10-02T00:10:00Z") {
		t.Errorf("partial last success: %d", partial.LastSuccessAt)
	}

	if out.Schedules[0].LastBackup.FailureReason != "error getting backup store" {
		t.Errorf("failure reason: %+v", out.Schedules[0].LastBackup)
	}

	if !slices.Equal(out.Schedules[5].IncludedNamespaces, []string{"app"}) {
		t.Errorf("namespaces: %v", out.Schedules[5].IncludedNamespaces)
	}

	// Failed ones taken by hand in the last week, newest first; a completed or older one is left out.
	var adhoc []string
	for _, b := range out.Adhoc {
		adhoc = append(adhoc, b.Name+":"+b.Health)
	}

	if !slices.Equal(adhoc, []string{"manual-invalid:warning", "before-upgrade:warning"}) {
		t.Errorf("adhoc: %v", adhoc)
	}

	// The unavailable location first.
	if len(out.Locations) != 2 || out.Locations[0].Name != "offsite" || out.Locations[0].Health != healthCritical ||
		out.Locations[1].Health != healthOK || !out.Locations[1].Default || out.Locations[1].Bucket != "backups" {
		t.Errorf("locations: %+v", out.Locations)
	}
}

func TestVeleroStaleNeedsTwoIntervals(t *testing.T) {
	daily := veleroSchedule{Schedule: "0 2 * * *", LastSuccessAt: fixtureNow.Add(-47 * time.Hour).UnixMilli()}
	if h, r := veleroScheduleHealth(daily, false, 0, fixtureNow); h != healthOK || len(r) != 0 {
		t.Errorf("47h on a daily schedule: %s %v", h, r)
	}

	daily.LastSuccessAt = fixtureNow.Add(-49 * time.Hour).UnixMilli()
	if h, r := veleroScheduleHealth(daily, false, 0, fixtureNow); h != healthWarning || !slices.Equal(r, []string{veleroReasonStale}) {
		t.Errorf("49h on a daily schedule: %s %v", h, r)
	}

	weekly := veleroSchedule{Schedule: "@weekly", LastSuccessAt: fixtureNow.Add(-10 * 24 * time.Hour).UnixMilli()}
	if h, _ := veleroScheduleHealth(weekly, false, 0, fixtureNow); h != healthOK {
		t.Errorf("10 days on a weekly schedule: %s", h)
	}
}

func TestReadDataServicesVelero(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		// v2alpha1 preferred: the reader still asks v1.
		"GET /apis":                                     `{"groups":[{"name":"velero.io","preferredVersion":{"version":"v2alpha1"}}]}`,
		"GET /apis/velero.io/v1/schedules":              veleroSchedulesFixture,
		"GET /apis/velero.io/v1/backups":                veleroBackupsFixture,
		"GET /apis/velero.io/v1/backupstoragelocations": veleroLocationsFixture,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints("velero"), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.Velero == nil || res.Velero.Error != "" || res.Velero.Version != "v1" || len(res.Velero.Schedules) != 8 ||
		len(res.Velero.Adhoc) != 2 || len(res.Velero.Locations) != 2 {
		t.Fatalf("velero: %+v", res.Velero)
	}

	if res.Longhorn != nil || res.CNPG != nil || res.Garage != nil || res.Dragonfly != nil {
		t.Errorf("only Velero is installed: %+v", res)
	}
}

func TestVeleroDemoHasEveryState(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.Velero == nil || len(demo.Velero.Schedules) < 3 || len(demo.Velero.Locations) < 2 || len(demo.Velero.Adhoc) == 0 {
		t.Fatal("demo misses Velero")
	}

	states := map[string]bool{}
	for _, s := range demo.Velero.Schedules {
		states[s.Health] = true
	}

	for _, h := range []string{healthCritical, healthWarning, healthOK, healthIdle} {
		if !states[h] {
			t.Errorf("demo has no %s schedule: %v", h, states)
		}
	}
}

func TestVeleroExpiredBackupsAreNotStale(t *testing.T) {
	// A monthly schedule whose backups all expired (TTL shorter than the month) but that ran
	// last week: its run is the reference, not its creation a year ago.
	obj := veleroScheduleObject{}
	obj.Metadata.CreationTimestamp = fixtureNow.Add(-365 * 24 * time.Hour).Format(time.RFC3339)
	obj.Spec.Schedule = "@monthly"
	obj.Status.LastBackup = fixtureNow.Add(-7 * 24 * time.Hour).Format(time.RFC3339)

	if s := mapVeleroSchedule(obj, nil, nil, nil, fixtureNow); s.Health != healthOK {
		t.Errorf("ran last week: %s %v", s.Health, s.Reasons)
	}

	obj.Status.LastBackup = ""
	if s := mapVeleroSchedule(obj, nil, nil, nil, fixtureNow); !slices.Equal(s.Reasons, []string{veleroReasonStale}) {
		t.Errorf("never ran: %s %v", s.Health, s.Reasons)
	}
}
