package ichorgo

import (
	"reflect"
	"strings"
	"testing"
)

func TestAlertRulesFlapping(t *testing.T) {
	const flapOn = `{"default":{"flapOpens":3}}`

	warn := func(flaps int, problem bool) alertIn {
		return alertIn{Cluster: rulesProd, Key: "kubespan:10.0.0.2", Kind: "kubespan", Severity: "warning", Problem: problem, Flaps: flaps}
	}
	now := rulesAt(t, "2026-06-01T12:00:00Z")

	cases := []struct {
		name  string
		rules string
		alert alertIn
		want  alertDecision
	}{
		{"third open of a warning is folded", flapOn, warn(3, true), alertDecision{Action: alertDigest, Reason: alertReasonFlapping, Flaps: 3}},
		{"its recovery is folded too", flapOn, warn(4, false), alertDecision{Action: alertDigest, Reason: alertReasonFlapping, Flaps: 4}},
		{"below the threshold notifies", flapOn, warn(2, true), alertDecision{Action: alertNotify}},
		{"a flapping critical still notifies", flapOn,
			alertIn{Cluster: rulesProd, Key: "node:10.0.0.3", Kind: "node", Problem: true, Flaps: 9}, alertDecision{Action: alertNotify}},
		{"off by default", ``, warn(9, true), alertDecision{Action: alertNotify}},
		{"zero turns it off", `{"default":{"flapOpens":0}}`, warn(9, true), alertDecision{Action: alertNotify}},
		{"a cluster turns it off", `{"default":{"flapOpens":3},"clusters":{"` + rulesProd + `":{"flapOpens":0}}}`,
			warn(9, true), alertDecision{Action: alertNotify}},
		{"a cluster turns it on", `{"clusters":{"` + rulesProd + `":{"flapOpens":2}}}`,
			warn(2, true), alertDecision{Action: alertDigest, Reason: alertReasonFlapping, Flaps: 2}},
		{"snoozed comes first", flapOn,
			alertIn{Cluster: rulesProd, Key: "k", Kind: "am", Severity: "warning", Problem: true, Snoozed: true, Flaps: 5},
			alertDecision{Action: alertMute, Reason: alertReasonSnoozed}},
		{"the severity floor comes first", `{"default":{"flapOpens":3,"minSeverity":"critical"}}`, warn(5, true),
			alertDecision{Action: alertMute, Reason: alertReasonSeverity}},
		{"flapping comes before quiet hours",
			`{"default":{"flapOpens":3,"quietHours":{"start":"11:00","end":"13:00","tz":"UTC"}}}`, warn(3, true),
			alertDecision{Action: alertDigest, Reason: alertReasonFlapping, Flaps: 3}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runAlertRules(t, c.rules, []alertIn{c.alert}, now)
			c.want.Cluster, c.want.Key = c.alert.Cluster, c.alert.Key

			if !reflect.DeepEqual(got.Decisions[0], c.want) {
				t.Errorf("decision = %+v, want %+v", got.Decisions[0], c.want)
			}
		})
	}
}

func TestAlertRulesFlappingDigest(t *testing.T) {
	now := rulesAt(t, "2026-06-01T12:00:00Z")
	flapping := alertIn{Cluster: rulesProd, Key: "kubespan:10.0.0.2", Kind: "kubespan", Severity: "warning", Problem: true, Flaps: 3}
	slow := alertIn{Cluster: rulesProd, Key: "am:slow", Kind: "am", Severity: "warning", Problem: true}

	// The digest counts the flapping alerts apart, with their keys, digest rule or not.
	for _, rules := range []string{`{"default":{"flapOpens":3,"digest":true}}`, `{"default":{"flapOpens":3}}`} {
		got := runAlertRules(t, rules, []alertIn{flapping, slow}, now)

		want := []alertDigestGroup{{Cluster: rulesProd, Count: 1, Warning: 1, FirstKeys: []string{"kubespan:10.0.0.2"},
			Flapping: 1, FlappingKeys: []string{"kubespan:10.0.0.2"}}}
		if strings.Contains(rules, "digest") {
			want[0].Count, want[0].Warning, want[0].FirstKeys = 2, 2, []string{"kubespan:10.0.0.2", "am:slow"}
		}

		if !reflect.DeepEqual(got.Digests, want) {
			t.Errorf("%s: digests = %+v, want %+v", rules, got.Digests, want)
		}
	}

	for _, bad := range []string{`{"default":{"flapOpens":-1}}`, `{"default":{"flapOpens":101}}`} {
		if _, err := AlertRulesApply(bad, `[]`, now); err == nil || !strings.Contains(err.Error(), "flapOpens") {
			t.Errorf("%s: %v, want a flapOpens error", bad, err)
		}
	}
}
