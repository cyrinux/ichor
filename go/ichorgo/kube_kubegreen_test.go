package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func kubeGreenObject(ns, name string) sleepInfoObject {
	var o sleepInfoObject
	o.Metadata.Namespace, o.Metadata.Name = ns, name

	return o
}

// A weekday schedule (sleep 20:00, wake 07:00, Europe/Rome) seen on Wednesday 2026-10-14
// 12:00 Rome (10:00 UTC): awake since the morning wake-up, sleeping again at 20:00.
func TestKubeGreenNextRuns(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	obj := kubeGreenObject("staging", "nights")
	obj.Spec.Weekdays, obj.Spec.SleepTime, obj.Spec.WakeUpTime, obj.Spec.TimeZone = "1-5", "20:00", "07:00", "Europe/Rome"
	obj.Status.Operation, obj.Status.LastScheduleTime = "WAKE_UP", "2026-10-14T07:00:00+02:00"

	s := mapKubeGreenSleepInfo(obj, now)

	if s.Asleep || s.Health != healthOK || len(s.Reasons) != 0 {
		t.Fatalf("awake schedule: %+v", s)
	}

	if got, want := s.NextSleepAt, time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC).UnixMilli(); got != want {
		t.Errorf("next sleep: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}

	if got, want := s.NextWakeUpAt, time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC).UnixMilli(); got != want {
		t.Errorf("next wake-up: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}

	// Saturday noon: the next sleep and wake-up are Monday's (weekdays 1-5).
	saturday := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	s = mapKubeGreenSleepInfo(obj, saturday)
	if got, want := s.NextSleepAt, time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC).UnixMilli(); got != want {
		t.Errorf("next sleep over the weekend: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}

	if got, want := s.NextWakeUpAt, time.Date(2026, 10, 19, 5, 0, 0, 0, time.UTC).UnixMilli(); got != want {
		t.Errorf("next wake-up over the weekend: got %s, want %s", time.UnixMilli(got), time.UnixMilli(want))
	}
}

func TestKubeGreenAsleepAndDefaults(t *testing.T) {
	now := time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC) // 23:00 Rome
	obj := kubeGreenObject("staging", "nights")
	obj.Spec.Weekdays, obj.Spec.SleepTime, obj.Spec.WakeUpTime, obj.Spec.TimeZone = "1-5", "20:00", "07:00", "Europe/Rome"
	obj.Status.Operation, obj.Status.LastScheduleTime = "SLEEP", "2026-10-14T20:00:00+02:00"

	s := mapKubeGreenSleepInfo(obj, now)
	if !s.Asleep || s.Health != healthOK {
		t.Fatalf("asleep schedule: %+v", s)
	}

	// Absent suspend flags are true, as upstream defaults; explicit false wins.
	if !s.SuspendDeployments || !s.SuspendStatefulSets || s.SuspendCronJobs {
		t.Errorf("defaults: %+v", s)
	}

	no := false
	obj.Spec.SuspendDeployments, obj.Spec.SuspendStatefulSets = &no, &no
	obj.Spec.SuspendCronJobs = true
	if s := mapKubeGreenSleepInfo(obj, now); s.SuspendDeployments || s.SuspendStatefulSets || !s.SuspendCronJobs {
		t.Errorf("explicit flags: %+v", s)
	}
}

func TestKubeGreenReasons(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)

	// No wake-up time: sleeps until changed by hand, never a next wake-up.
	obj := kubeGreenObject("sandbox", "until-changed")
	obj.Spec.Weekdays, obj.Spec.SleepTime = "1-5", "19:00"
	obj.Status.Operation, obj.Status.LastScheduleTime = "SLEEP", "2026-10-13T19:00:00Z" // Tuesday 19:00 UTC
	if s := mapKubeGreenSleepInfo(obj, now); s.Health != healthWarning || !slices.Equal(s.Reasons, []string{kubeGreenReasonNoWakeUp}) ||
		s.NextWakeUpAt != 0 || s.NextSleepAt == 0 {
		t.Errorf("no wake-up: %+v", s)
	}

	// A part that does not parse: the schedule is invalid, no next runs.
	for _, bad := range []struct{ weekdays, sleepAt, wakeUpAt, timeZone string }{
		{"9", "20:00", "07:00", ""}, {"1-5", "25:00", "07:00", ""}, {"1-5", "2000", "07:00", ""},
		{"1-5", "20:00", "07:00", "Mars/Phobos"},
	} {
		obj := kubeGreenObject("staging", "broken")
		obj.Spec.Weekdays, obj.Spec.SleepTime, obj.Spec.WakeUpTime, obj.Spec.TimeZone = bad.weekdays, bad.sleepAt, bad.wakeUpAt, bad.timeZone
		if s := mapKubeGreenSleepInfo(obj, now); s.Health != healthWarning || !slices.Equal(s.Reasons, []string{kubeGreenReasonInvalidSchedule}) ||
			s.NextSleepAt != 0 || s.NextWakeUpAt != 0 {
			t.Errorf("invalid schedule %v: %+v", bad, s)
		}
	}

	// The controller missed its last expected run (Wednesday 07:00 Rome): overdue.
	obj = kubeGreenObject("staging", "nights")
	obj.Spec.Weekdays, obj.Spec.SleepTime, obj.Spec.WakeUpTime, obj.Spec.TimeZone = "1-5", "20:00", "07:00", "Europe/Rome"
	obj.Status.Operation, obj.Status.LastScheduleTime = "WAKE_UP", "2026-10-12T07:00:00+02:00"
	if s := mapKubeGreenSleepInfo(obj, now); s.Health != healthWarning || !slices.Equal(s.Reasons, []string{kubeGreenReasonOverdue}) {
		t.Errorf("overdue: %+v", s)
	}

	// The same schedule that ran this morning is fine.
	obj.Status.LastScheduleTime = "2026-10-14T07:00:00+02:00"
	if s := mapKubeGreenSleepInfo(obj, now); s.Health != healthOK || len(s.Reasons) != 0 {
		t.Errorf("ran this morning: %+v", s)
	}

	// Never ran: idle, not a warning.
	obj = kubeGreenObject("staging", "fresh")
	obj.Spec.Weekdays, obj.Spec.SleepTime, obj.Spec.WakeUpTime = "1-5", "20:00", "07:00"
	if s := mapKubeGreenSleepInfo(obj, now); s.Health != healthIdle || !slices.Equal(s.Reasons, []string{kubeGreenReasonNeverRan}) {
		t.Errorf("never ran: %+v", s)
	}
}

func TestReadDataServicesKubeGreen(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kube-green.com","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/kube-green.com/v1alpha1/sleepinfos": `{"items":[
			{"metadata":{"name":"nights","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00","timeZone":"Europe/Rome"},
			 "status":{"lastScheduleTime":"2026-10-14T07:00:00+02:00","operation":"WAKE_UP"}}]}`,
		// The controller Deployment is not readable with these answers: version "".
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints(""), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.KubeGreen == nil || res.KubeGreen.Error != "" || len(res.KubeGreen.SleepInfos) != 1 {
		t.Fatalf("kube-green: %+v", res.KubeGreen)
	}

	if s := res.KubeGreen.SleepInfos[0]; s.Namespace != "staging" || s.Name != "nights" || !s.SuspendDeployments ||
		s.Health != healthOK || s.NextSleepAt == 0 {
		t.Errorf("sleep info: %+v", s)
	}

	if res.Velero != nil || res.Longhorn != nil || res.CNPG != nil {
		t.Errorf("only kube-green is installed: %+v", res)
	}
}

func TestKubeGreenDemo(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.KubeGreen == nil || len(demo.KubeGreen.SleepInfos) != 3 || demo.KubeGreen.Version != "v0.7.1" || demo.KubeGreen.Error != "" {
		t.Fatalf("demo: %+v", demo.KubeGreen)
	}

	var noWakeUp bool
	for _, s := range demo.KubeGreen.SleepInfos {
		if s.WakeUpAt == "" {
			noWakeUp = true
			if !slices.Equal(s.Reasons, []string{kubeGreenReasonNoWakeUp}) || s.NextWakeUpAt != 0 {
				t.Errorf("no wake-up demo: %+v", s)
			}
		}

		if s.NextSleepAt == 0 || s.LastScheduleAt == 0 {
			t.Errorf("demo schedule without runs: %+v", s)
		}
	}

	if !noWakeUp {
		t.Error("demo has no SleepInfo without a wake-up time")
	}

	// The JSON the apps decode carries the section under its tag.
	raw, err := json.Marshal(demo)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(jsonKeys(raw), "kubeGreen") {
		t.Error("no kubeGreen key in the demo JSON")
	}
}

// jsonKeys reads the top-level keys of a JSON object.
func jsonKeys(raw []byte) []string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}

	out := []string{}
	for k := range m {
		out = append(out, k)
	}

	return out
}
