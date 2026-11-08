package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Moving a kube-green sleep schedule: push tonight's sleep back, wake a namespace earlier, give
// a "sleep forever" SleepInfo a wake-up. kube-green has no manual trigger, so the schedule is
// the only thing changed: a merge patch of the SleepInfo's weekdays, sleepAt, wakeUpAt and
// timeZone. The values are checked here the way the CRD's patterns and the controller read
// them, so a bad one is refused before any request.

var (
	// kubeGreenWeekdaysPattern and kubeGreenTimePattern mirror the SleepInfo CRD's validation.
	kubeGreenWeekdaysPattern = regexp.MustCompile(`^[0-6*,/-]+$`)
	kubeGreenTimePattern     = regexp.MustCompile(`^[0-9*,/-]+:[0-9*,/-]+$`)
)

// KubeGreenSetSchedule sets the kube-green SleepInfo namespace/name to sleep at sleepAt
// ("HH:mm", each part cron syntax) and wake up at wakeUpAt ("" for never: it sleeps until
// changed by hand) on weekdays (cron day-of-week, "1-5"), in timeZone (IANA, "" for UTC)
// (os:admin; patch on sleepinfos.kube-green.com). Refused on demo contexts.
// kubeServer: see KubePods.
func KubeGreenSetSchedule(configYAML, contextName, kubeServer, namespace, name, weekdays, sleepAt, wakeUpAt, timeZone string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	weekdays, sleepAt = strings.TrimSpace(weekdays), strings.TrimSpace(sleepAt)
	wakeUpAt, timeZone = strings.TrimSpace(wakeUpAt), strings.TrimSpace(timeZone)

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "kubegreen-set-schedule", Namespace: namespace, Object: "SleepInfo/" + name,
		Params: strings.TrimSpace(fmt.Sprintf("%s %s-%s %s", weekdays, sleepAt, wakeUpAt, timeZone)),
	})

	if err := validateKubeName("SleepInfo", namespace, name); err != nil {
		return err
	}

	if err := validateKubeGreenSchedule(weekdays, sleepAt, wakeUpAt, timeZone); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		path := "/apis/" + groupKubeGreen + "/" + kubeGreenVersion + "/namespaces/" + url.PathEscape(namespace) +
			"/sleepinfos/" + url.PathEscape(name)

		return k.patch(ctx, path, "application/merge-patch+json", kubeGreenSchedulePatch(weekdays, sleepAt, wakeUpAt, timeZone), nil)
	})
}

// validateKubeGreenSchedule refuses what the CRD or the controller would: weekdays and times
// outside their patterns, a time or weekday list cron does not parse, an unknown time zone.
func validateKubeGreenSchedule(weekdays, sleepAt, wakeUpAt, timeZone string) error {
	if !kubeGreenWeekdaysPattern.MatchString(weekdays) {
		return fmt.Errorf("weekdays %q: a cron day-of-week, such as 1-5 or *", weekdays)
	}

	for _, t := range []struct{ label, at string }{{"sleep at", sleepAt}, {"wake up at", wakeUpAt}} {
		if t.at == "" && t.label == "wake up at" {
			continue // no wake-up: it sleeps until changed by hand
		}

		if !kubeGreenTimePattern.MatchString(t.at) {
			return fmt.Errorf("%s %q: an hour and a minute, such as 20:00", t.label, t.at)
		}

		if _, err := kubeGreenCron(t.at, weekdays); err != nil {
			return fmt.Errorf("%s %q on %q: %w", t.label, t.at, weekdays, err)
		}
	}

	if timeZone != "" {
		if _, err := time.LoadLocation(timeZone); err != nil {
			return fmt.Errorf("time zone %q is unknown", timeZone)
		}
	}

	return nil
}

// kubeGreenSchedulePatch is the merge patch: an empty wakeUpAt or timeZone is null, which
// removes the field (no wake-up, UTC).
func kubeGreenSchedulePatch(weekdays, sleepAt, wakeUpAt, timeZone string) map[string]any {
	orNull := func(s string) any {
		if s == "" {
			return nil
		}

		return s
	}

	return map[string]any{"spec": map[string]any{
		"weekdays": weekdays,
		"sleepAt":  sleepAt,
		"wakeUpAt": orNull(wakeUpAt),
		"timeZone": orNull(timeZone),
	}}
}
