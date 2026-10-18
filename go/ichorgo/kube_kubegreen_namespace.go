package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// kubeGreenNamespace is what kube-green has done to one namespace, for a workload row or a
// CronJob row to say it is asleep before the user scales or resumes by hand, and for the Go
// warning a scale carries. The booleans are the OR over the namespace's SleepInfos.
type kubeGreenNamespace struct {
	Installed          bool                 `json:"installed"`
	Asleep             bool                 `json:"asleep"`
	SleepInfos         []kubeGreenSleepInfo `json:"sleepInfos"`
	SleepsDeployments  bool                 `json:"sleepsDeployments"`
	SleepsStatefulSets bool                 `json:"sleepsStatefulSets"`
	SleepsCronJobs     bool                 `json:"sleepsCronJobs"`
	NextWakeUpAt       int64                `json:"nextWakeUpAt"` // unix ms, 0 when none
	NextSleepAt        int64                `json:"nextSleepAt"`  // unix ms, 0 when none
	// Filtered: a SleepInfo has excludeRef or includeRef, so which objects it touches cannot be
	// told from the list alone: the apps word it "may be".
	Filtered bool `json:"filtered"`
}

// KubeGreenNamespace reads what kube-green has done to one namespace (os:admin): its SleepInfos
// and what they suspend, in one list request. A namespace no SleepInfo covers, or a cluster
// without kube-green (the list answers 404), is installed:false with no error, so the apps show
// nothing and a kubeconfig that may not list sleepinfos never blocks. kubeServer: see KubePods.
func KubeGreenNamespace(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeGreenNamespace { return demoKubeGreenNamespace(namespace, time.Now()) },
		func(ctx context.Context, k *kubeClient) (kubeGreenNamespace, error) {
			return readKubeGreenNamespace(ctx, k, namespace, time.Now()), nil
		})
}

// readKubeGreenNamespace lists the namespace's SleepInfos in one request. A 404 (the group is
// not served, or no such namespace) means nothing to say: installed stays false, no error.
func readKubeGreenNamespace(ctx context.Context, k *kubeClient, namespace string, now time.Time) kubeGreenNamespace {
	var list kubeList[sleepInfoObject]

	path := "/apis/" + groupKubeGreen + "/" + kubeGreenVersion + "/namespaces/" + url.PathEscape(namespace) + "/sleepinfos"
	if err := getList(ctx, k, path, &list); err != nil {
		return kubeGreenNamespace{SleepInfos: []kubeGreenSleepInfo{}}
	}

	return mapKubeGreenNamespace(list.Items, now)
}

// mapKubeGreenNamespace folds the SleepInfos of one namespace into what a row has to show.
func mapKubeGreenNamespace(objs []sleepInfoObject, now time.Time) kubeGreenNamespace {
	infos := mapKubeGreen(objs, now)

	out := kubeGreenNamespace{Installed: true, SleepInfos: infos}

	for _, s := range infos {
		out.Asleep = out.Asleep || s.Asleep
		out.SleepsDeployments = out.SleepsDeployments || s.SuspendDeployments
		out.SleepsStatefulSets = out.SleepsStatefulSets || s.SuspendStatefulSets
		out.SleepsCronJobs = out.SleepsCronJobs || s.SuspendCronJobs
		out.Filtered = out.Filtered || s.ExcludeCount > 0 || s.IncludeCount > 0

		// The wake-up only of the schedules asleep now: one awake again tomorrow is not this.
		if s.Asleep {
			out.NextWakeUpAt = sooner(out.NextWakeUpAt, s.NextWakeUpAt)
		}

		out.NextSleepAt = sooner(out.NextSleepAt, s.NextSleepAt)
	}

	return out
}

// sooner is the earliest of two unix ms, 0 meaning none.
func sooner(a, b int64) int64 {
	if a == 0 {
		return b
	}

	if b == 0 {
		return a
	}

	return min(a, b)
}

// kubeGreenWarningText is the warning a scale or a resume should carry, "" when kube-green will
// not touch the namespace: asleep now, or sleeping within a day. now bounds the second case.
func kubeGreenWarningText(s kubeGreenNamespace, now time.Time) string {
	switch {
	case s.Asleep:
		if s.NextWakeUpAt > 0 {
			return fmt.Sprintf("kube-green sleeps this namespace: the replicas are restored at the next wake-up, %s",
				kubeGreenAt(s.NextWakeUpAt, kubeGreenZoneFor(s, s.NextWakeUpAt, true)))
		}

		return "kube-green sleeps this namespace: the replicas are restored at the next wake-up (no wake-up scheduled: until changed by hand)"
	case s.NextSleepAt > 0 && !time.UnixMilli(s.NextSleepAt).After(now.Add(24*time.Hour)):
		return fmt.Sprintf("kube-green sleeps this namespace at %s: a change made now is undone at the next sleep",
			kubeGreenAt(s.NextSleepAt, kubeGreenZoneFor(s, s.NextSleepAt, false)))
	default:
		return ""
	}
}

// kubeGreenZoneFor is the IANA zone of the sleep schedule one of the namespace's next runs
// comes from, "" (UTC) when it cannot be found.
func kubeGreenZoneFor(s kubeGreenNamespace, ms int64, asleep bool) string {
	for _, info := range s.SleepInfos {
		if asleep && info.Asleep && info.NextWakeUpAt == ms {
			return info.TimeZone
		}

		if !asleep && info.NextSleepAt == ms {
			return info.TimeZone
		}
	}

	return ""
}

// kubeGreenAt renders a run in its schedule's zone ("Mon 15:04"), UTC when the zone is unknown.
func kubeGreenAt(ms int64, zone string) string {
	loc, err := time.LoadLocation(orDefault(zone, "UTC"))
	if err != nil {
		loc = time.UTC
	}

	return time.UnixMilli(ms).In(loc).Format("Mon 15:04")
}
