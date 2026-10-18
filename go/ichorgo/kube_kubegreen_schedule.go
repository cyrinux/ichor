package ichorgo

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// kube-green puts a namespace's Deployments, StatefulSets and CronJobs to sleep on a schedule
// (SleepInfo, kube-green.com/v1alpha1) and wakes them up later. The Data services list is the
// built-in plugin go/plugin/builtin/kube-green.yaml; this mapping computes the next sleep and
// wake-up of one SleepInfo in its schedule's time zone for the workload rows
// (KubeGreenNamespace) and the schedule edit (KubeGreenSetSchedule). What the controller did
// last comes from status, never from its Secret. plugin_kubegreen_test.go holds the plugin
// and this mapping to the same answers.

const (
	groupKubeGreen   = "kube-green.com"
	kubeGreenVersion = "v1alpha1"
)

type kubeGreenSleepInfo struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Weekdays  string `json:"weekdays"` // cron day-of-week, as written ("1-5")
	SleepAt   string `json:"sleepAt"`  // "HH:mm", each part cron syntax
	WakeUpAt  string `json:"wakeUpAt"` // "" sleeps until changed by hand
	TimeZone  string `json:"timeZone"` // IANA, "" is UTC
	// What the schedule suspends (absent booleans are true, as upstream defaults).
	SuspendDeployments  bool `json:"suspendDeployments"`
	SuspendStatefulSets bool `json:"suspendStatefulSets"`
	SuspendCronJobs     bool `json:"suspendCronJobs"`
	ExcludeCount        int  `json:"excludeCount"`
	IncludeCount        int  `json:"includeCount"`
	PatchCount          int  `json:"patchCount"`
	// Operation is what the controller did last (SLEEP, WAKE_UP, "" before the first run).
	Operation      string   `json:"operation"`
	LastScheduleAt int64    `json:"lastScheduleAt"` // unix ms, 0 when none
	NextSleepAt    int64    `json:"nextSleepAt"`    // unix ms, 0 when the schedule does not parse
	NextWakeUpAt   int64    `json:"nextWakeUpAt"`   // unix ms, 0 when wakeUpAt is empty
	Asleep         bool     `json:"asleep"`         // operation == SLEEP
	Health         string   `json:"health"`         // ok|warning|idle, never critical
	Reasons        []string `json:"reasons"`
}

// Reasons a SleepInfo is not ok, for the apps to word. A sleeping namespace is not an
// incident: never critical.
const (
	kubeGreenReasonNoWakeUp        = "noWakeUp"        // wakeUpAt empty: sleeps until changed by hand
	kubeGreenReasonInvalidSchedule = "invalidSchedule" // weekdays, sleepAt, wakeUpAt or timeZone do not parse
	kubeGreenReasonOverdue         = "overdue"         // the controller missed its last expected run
	kubeGreenReasonNeverRan        = "neverRan"        // no lastScheduleTime yet
)

type sleepInfoObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Weekdays            string `json:"weekdays"`
		SleepTime           string `json:"sleepAt"`
		WakeUpTime          string `json:"wakeUpAt"`
		TimeZone            string `json:"timeZone"`
		SuspendCronJobs     bool   `json:"suspendCronJobs"`
		SuspendDeployments  *bool  `json:"suspendDeployments"`
		SuspendStatefulSets *bool  `json:"suspendStatefulSets"`
		ExcludeRef          []struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Name       string `json:"name"`
		} `json:"excludeRef"`
		IncludeRef []struct {
			APIVersion  string            `json:"apiVersion"`
			Kind        string            `json:"kind"`
			Name        string            `json:"name"`
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"includeRef"`
		Patches []struct {
			Target struct {
				Group string `json:"group"`
				Kind  string `json:"kind"`
			} `json:"target"`
		} `json:"patches"`
	} `json:"spec"`
	Status struct {
		LastScheduleTime string `json:"lastScheduleTime"`
		Operation        string `json:"operation"`
	} `json:"status"`
}

// mapKubeGreen maps the SleepInfos, worst health first.
func mapKubeGreen(objs []sleepInfoObject, now time.Time) []kubeGreenSleepInfo {
	out := make([]kubeGreenSleepInfo, 0, len(objs))

	for _, obj := range objs {
		out = append(out, mapKubeGreenSleepInfo(obj, now))
	}

	slices.SortFunc(out, byHealthThenKey(func(x kubeGreenSleepInfo) (string, string) {
		return x.Health, x.Namespace + "/" + x.Name
	}))

	return out
}

func mapKubeGreenSleepInfo(obj sleepInfoObject, now time.Time) kubeGreenSleepInfo {
	s := kubeGreenSleepInfo{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name,
		Weekdays: obj.Spec.Weekdays, SleepAt: obj.Spec.SleepTime, WakeUpAt: obj.Spec.WakeUpTime, TimeZone: obj.Spec.TimeZone,
		// Absent means true, as upstream defaults.
		SuspendDeployments:  obj.Spec.SuspendDeployments == nil || *obj.Spec.SuspendDeployments,
		SuspendStatefulSets: obj.Spec.SuspendStatefulSets == nil || *obj.Spec.SuspendStatefulSets,
		SuspendCronJobs:     obj.Spec.SuspendCronJobs,
		ExcludeCount:        len(obj.Spec.ExcludeRef), IncludeCount: len(obj.Spec.IncludeRef), PatchCount: len(obj.Spec.Patches),
		Operation:      obj.Status.Operation,
		LastScheduleAt: unixMilli(obj.Status.LastScheduleTime),
		Reasons:        []string{},
	}
	s.Asleep = s.Operation == "SLEEP"

	loc, locErr := time.LoadLocation(orDefault(s.TimeZone, "UTC"))

	sleep, sleepErr := kubeGreenCron(s.SleepAt, s.Weekdays)
	var wake cronSchedule
	var wakeErr error
	if s.WakeUpAt != "" {
		wake, wakeErr = kubeGreenCron(s.WakeUpAt, s.Weekdays)
	} else {
		s.Reasons = append(s.Reasons, kubeGreenReasonNoWakeUp)
	}

	// A schedule that does not parse (or a time zone the phone does not know) is the
	// operator's problem to fix: no next runs, and it wins over neverRan.
	if locErr != nil || sleepErr != nil || wakeErr != nil {
		s.Reasons = append(s.Reasons, kubeGreenReasonInvalidSchedule)
		s.Health = healthWarning

		return s
	}

	inZone := now.In(loc)
	s.NextSleepAt = sleep.next(inZone).UnixMilli()
	if s.WakeUpAt != "" {
		s.NextWakeUpAt = wake.next(inZone).UnixMilli()
	}

	if s.LastScheduleAt == 0 {
		s.Reasons = append(s.Reasons, kubeGreenReasonNeverRan)
		s.Health = healthIdle

		return s
	}

	// The controller missed its last expected run (either cron): it is not running.
	prevSleep, prevWake := kubeGreenPrevRun(sleep, true, inZone), kubeGreenPrevRun(wake, s.WakeUpAt != "", inZone)
	expected := prevSleep
	if prevWake.After(prevSleep) {
		expected = prevWake
	}

	if !expected.IsZero() && time.UnixMilli(s.LastScheduleAt).Before(expected.Add(-10*time.Minute)) {
		s.Reasons = append(s.Reasons, kubeGreenReasonOverdue)
	}

	switch {
	case len(s.Reasons) > 0:
		s.Health = healthWarning
	default:
		s.Health = healthOK
	}

	return s
}

// kubeGreenCron composes the 5-field cron of one daily time ("20:00", parts may be cron
// syntax) over the schedule's weekdays: cron is "minute hour * * weekdays".
func kubeGreenCron(at, weekdays string) (cronSchedule, error) {
	hh, mm, ok := strings.Cut(at, ":")
	if !ok {
		return cronSchedule{}, fmt.Errorf("time %q: expected HH:mm", at)
	}

	return parseCronSchedule(mm + " " + hh + " * * " + orDefault(weekdays, "*"))
}

// kubeGreenPrevRun is the last run of cron before from, walking forward from a week back
// (an HH:mm schedule runs at most daily; a week bounds a "* * * * 1" gap). zero when cron
// is nil or never ran in the window.
func kubeGreenPrevRun(cron cronSchedule, has bool, from time.Time) time.Time {
	if !has {
		return time.Time{}
	}

	var prev time.Time

	for r, i := cron.next(from.Add(-8*24*time.Hour)), 0; !r.IsZero() && r.Before(from) && i < 10; i++ {
		prev = r
		r = cron.next(r)
	}

	return prev
}
