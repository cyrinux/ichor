package ichorgo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const sleepInfoPath = "/apis/kube-green.com/v1alpha1/namespaces/shop/sleepinfos/working-hours"

func TestKubeGreenSchedulePatch(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"PATCH " + sleepInfoPath: `{}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		weekdays, sleepAt, wakeUpAt, tz, want string
	}{
		{"1-5", "20:00", "08:00", "Europe/Paris", `{"spec":{"sleepAt":"20:00","timeZone":"Europe/Paris","wakeUpAt":"08:00","weekdays":"1-5"}}`},
		// No wake-up and no zone: both fields removed (sleep until changed by hand, UTC).
		{"*", "19:30", "", "", `{"spec":{"sleepAt":"19:30","timeZone":null,"wakeUpAt":null,"weekdays":"*"}}`},
	} {
		if err := validateKubeGreenSchedule(c.weekdays, c.sleepAt, c.wakeUpAt, c.tz); err != nil {
			t.Fatalf("%+v: %v", c, err)
		}

		if err := k.patch(context.Background(), sleepInfoPath, "application/merge-patch+json", kubeGreenSchedulePatch(c.weekdays, c.sleepAt, c.wakeUpAt, c.tz), nil); err != nil {
			t.Fatal(err)
		}

		last := lastRequest(f)
		if last.method != "PATCH" || last.path != sleepInfoPath || last.contentType != "application/merge-patch+json" || last.body != c.want {
			t.Errorf("patch %+v\nwant %s", last, c.want)
		}
	}
}

func TestKubeGreenScheduleRefusals(t *testing.T) {
	for _, c := range []struct {
		weekdays, sleepAt, wakeUpAt, tz, want string
	}{
		{"mon-fri", "20:00", "08:00", "", "weekdays"},
		{"1-5", "25:00", "08:00", "", "sleep at"},
		{"1-5", "20h", "08:00", "", "sleep at"},
		{"1-5", "20:00", "8", "", "wake up at"},
		{"1-5", "20:61", "08:00", "", "sleep at"},
		{"1-5", "20:00", "08:00", "Mars/Olympus", "time zone"},
		{"8", "20:00", "08:00", "", "weekdays"},
	} {
		err := validateKubeGreenSchedule(c.weekdays, c.sleepAt, c.wakeUpAt, c.tz)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want an error about %s", c, err, c.want)
		}
	}
}

func TestKubeGreenSetScheduleDemoAndAudit(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeGreenSetSchedule(demo, "", "", "shop", "working-hours", "1-5", "20:00", "", "Europe/Paris"); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("demo: %v", err)
	}

	// A bad value is refused before any request, and recorded too.
	if err := KubeGreenSetSchedule(demo, "", "", "shop", "working-hours", "1-5", "25:00", "08:00", ""); err == nil || errors.Is(err, errDemoUnavailable) {
		t.Fatalf("bad time: %v", err)
	}

	entries := readAudit(t, "", "")
	if len(entries) != 2 || entries[0].Action != "kubegreen-set-schedule" || entries[0].Object != "SleepInfo/working-hours" {
		t.Fatalf("audit %+v", entries)
	}
}
