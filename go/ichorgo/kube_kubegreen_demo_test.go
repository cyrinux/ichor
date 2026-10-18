package ichorgo

import (
	"testing"
	"time"
)

// The demo's namespace reads (the schedule editor, the workload sheets) hold the same
// SleepInfos as the kube-green plugin's samples the demo's plugin tab shows.
func TestDemoKubeGreenNamespaceMatchesThePluginSamples(t *testing.T) {
	now := time.Now()

	cases := []struct{ namespace, name, sleepAt, wakeUpAt, weekdays string }{
		{"demo-shop", "nightly", "20:00", "08:00", "1-5"},
		{"demo-lab", "weekend", "00:00", "", "6,0"},
		{"demo-preview", "previews", "22:30", "06:00", "*"},
	}
	for _, c := range cases {
		got := demoKubeGreenNamespace(c.namespace, now)
		if !got.Installed || len(got.SleepInfos) != 1 {
			t.Fatalf("%s: %+v", c.namespace, got)
		}

		s := got.SleepInfos[0]
		if s.Namespace != c.namespace || s.Name != c.name || s.SleepAt != c.sleepAt || s.WakeUpAt != c.wakeUpAt || s.Weekdays != c.weekdays {
			t.Errorf("%s: got %+v", c.namespace, s)
		}
	}

	if got := demoKubeGreenNamespace("demo-lab", now); !got.Asleep {
		t.Errorf("demo-lab sleeps in the samples: %+v", got)
	}

	if got := demoKubeGreenNamespace("staging", now); len(got.SleepInfos) != 1 || got.SleepInfos[0].Name != "nights" {
		t.Errorf("staging: %+v", got)
	}

	if got := demoKubeGreenNamespace("elsewhere", now); !got.Installed || got.SleepInfos == nil || len(got.SleepInfos) != 0 {
		t.Errorf("elsewhere: %+v", got)
	}
}
