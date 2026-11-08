package ichorgo

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Sample fingerprints: invented hex strings.
const (
	rulesProd    = "a1b2c3d4e5f60718"
	rulesStaging = "0f1e2d3c4b5a6978"
	rulesLab     = "9988776655443322"
)

func rulesAt(t *testing.T, value string) int64 {
	t.Helper()

	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}

	return at.UnixMilli()
}

func runAlertRules(t *testing.T, rules string, alerts []alertIn, now int64) alertRulesResult {
	t.Helper()

	in, err := json.Marshal(alerts)
	if err != nil {
		t.Fatal(err)
	}

	out, err := AlertRulesApply(rules, string(in), now)
	if err != nil {
		t.Fatal(err)
	}

	var got alertRulesResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}

	return got
}

const parisNight = `{"default":{"quietHours":{"start":"23:00","end":"07:00","tz":"Europe/Paris","exceptCritical":true}}}`

func TestAlertRulesDecide(t *testing.T) {
	warning := alertIn{Cluster: rulesProd, Key: "am:slow", Kind: "am", Severity: "warning", Problem: true}
	critical := alertIn{Cluster: rulesProd, Key: "am:disk", Kind: "am", Severity: "critical", Problem: true}
	nodeDown := alertIn{Cluster: rulesProd, Key: "node:10.0.0.1", Kind: "node", Problem: true}
	info := alertIn{Cluster: rulesProd, Key: "am:info", Kind: "am", Severity: "info", Problem: true}

	cases := []struct {
		name  string
		rules string
		alert alertIn
		now   string
		want  alertDecision
	}{
		{"no rules notify", ``, warning, "2026-06-01T12:00:00Z", alertDecision{Action: alertNotify}},
		{"empty object notify", `{}`, info, "2026-06-01T12:00:00Z", alertDecision{Action: alertNotify}},
		{"snoozed first", ``, alertIn{Cluster: rulesProd, Key: "k", Kind: "node", Problem: true, Snoozed: true},
			"2026-06-01T12:00:00Z", alertDecision{Action: alertMute, Reason: alertReasonSnoozed}},
		{"before midnight in the window", parisNight, warning, "2026-06-01T21:30:00Z", // 23:30 CEST
			alertDecision{Action: alertMute, Reason: alertReasonQuiet, Until: rulesAt(t, "2026-06-02T05:00:00Z")}},
		{"after midnight in the window", parisNight, warning, "2026-06-02T03:00:00Z", // 05:00 CEST
			alertDecision{Action: alertMute, Reason: alertReasonQuiet, Until: rulesAt(t, "2026-06-02T05:00:00Z")}},
		{"window end is outside", parisNight, warning, "2026-06-02T05:00:00Z", alertDecision{Action: alertNotify}},
		{"daytime", parisNight, warning, "2026-06-02T10:00:00Z", alertDecision{Action: alertNotify}},
		{"exceptCritical lets critical through", parisNight, critical, "2026-06-01T22:00:00Z", alertDecision{Action: alertNotify}},
		{"node kind defaults to critical", parisNight, nodeDown, "2026-06-01T22:00:00Z", alertDecision{Action: alertNotify}},
		{"without exceptCritical critical is muted",
			`{"default":{"quietHours":{"start":"23:00","end":"07:00","tz":"Europe/Paris"}}}`, critical, "2026-06-01T22:00:00Z",
			alertDecision{Action: alertMute, Reason: alertReasonQuiet, Until: rulesAt(t, "2026-06-02T05:00:00Z")}},
		{"same-day window", `{"default":{"quietHours":{"start":"12:00","end":"14:00","tz":"UTC"}}}`, warning, "2026-06-02T13:59:00Z",
			alertDecision{Action: alertMute, Reason: alertReasonQuiet, Until: rulesAt(t, "2026-06-02T14:00:00Z")}},
		{"start equal to end is no window", `{"default":{"quietHours":{"start":"07:00","end":"07:00","tz":"UTC"}}}`, warning,
			"2026-06-02T07:00:00Z", alertDecision{Action: alertNotify}},
		{"floor warning mutes info", `{"default":{"minSeverity":"warning"}}`, info, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertMute, Reason: alertReasonSeverity}},
		{"floor warning keeps warning", `{"default":{"minSeverity":"warning"}}`, warning, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertNotify}},
		{"floor critical mutes warning", `{"default":{"minSeverity":"critical"}}`, warning, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertMute, Reason: alertReasonSeverity}},
		{"floor all keeps info", `{"default":{"minSeverity":"all"}}`, info, "2026-06-01T12:00:00Z", alertDecision{Action: alertNotify}},
		{"unknown severity is a warning", `{"default":{"minSeverity":"critical"}}`,
			alertIn{Cluster: rulesProd, Key: "k", Kind: "node", Severity: "page", Problem: true}, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertMute, Reason: alertReasonSeverity}},
		{"upper-case severity", `{"default":{"minSeverity":"critical"}}`,
			alertIn{Cluster: rulesProd, Key: "k", Kind: "am", Severity: "CRITICAL", Problem: true}, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertNotify}},
		{"recovery passes the floor", `{"default":{"minSeverity":"critical"}}`,
			alertIn{Cluster: rulesProd, Key: "am:slow", Kind: "am", Severity: "warning"}, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertNotify}},
		{"recovery of a warning is quiet", parisNight, alertIn{Cluster: rulesProd, Key: "am:slow", Kind: "am", Severity: "warning"},
			"2026-06-01T22:00:00Z", alertDecision{Action: alertMute, Reason: alertReasonQuiet, Until: rulesAt(t, "2026-06-02T05:00:00Z")}},
		{"recovery of a critical passes exceptCritical", parisNight, alertIn{Cluster: rulesProd, Key: "node:10.0.0.1", Kind: "node"},
			"2026-06-01T22:00:00Z", alertDecision{Action: alertNotify}},
		{"digest", `{"default":{"digest":true}}`, warning, "2026-06-01T12:00:00Z", alertDecision{Action: alertDigest, Reason: alertReasonDigest}},
		{"quiet comes before digest", `{"default":{"digest":true,"quietHours":{"start":"23:00","end":"07:00","tz":"Europe/Paris"}}}`,
			warning, "2026-06-01T22:00:00Z",
			alertDecision{Action: alertMute, Reason: alertReasonQuiet, Until: rulesAt(t, "2026-06-02T05:00:00Z")}},
		{"recovery of a critical skips the digest", `{"default":{"digest":true}}`,
			alertIn{Cluster: rulesProd, Key: "node:10.0.0.1", Kind: "node"}, "2026-06-01T12:00:00Z", alertDecision{Action: alertNotify}},
		{"recovery of a warning goes to the digest", `{"default":{"digest":true}}`,
			alertIn{Cluster: rulesProd, Key: "am:slow", Kind: "am", Severity: "warning"}, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertDigest, Reason: alertReasonDigest}},
		{"floor comes before digest", `{"default":{"digest":true,"minSeverity":"critical"}}`, warning, "2026-06-01T12:00:00Z",
			alertDecision{Action: alertMute, Reason: alertReasonSeverity}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runAlertRules(t, c.rules, []alertIn{c.alert}, rulesAt(t, c.now))
			c.want.Cluster, c.want.Key = c.alert.Cluster, c.alert.Key

			if len(got.Decisions) != 1 || got.Decisions[0] != c.want {
				t.Errorf("decisions = %+v, want %+v", got.Decisions, c.want)
			}
		})
	}
}

const repeat15 = `{"default":{"repeatMinutes":15}}`

// TestAlertRulesRepeat: an ongoing critical alert is posted again once its repeat is due, never
// a warning, an acknowledged, snoozed or digest one, nor one the quiet hours keep quiet.
func TestAlertRulesRepeat(t *testing.T) {
	now := rulesAt(t, "2026-06-01T12:00:00Z")
	minutesAgo := func(m int64) int64 { return now - m*60_000 }
	ongoing := func(change func(*alertIn)) alertIn {
		a := alertIn{
			Cluster: rulesProd, Key: "node:10.0.0.1", Kind: "node", Problem: true, Ongoing: true,
			FirstSeenMillis: minutesAgo(40), LastNotifiedMillis: minutesAgo(16),
		}
		if change != nil {
			change(&a)
		}

		return a
	}
	muted := func(reason string) alertDecision { return alertDecision{Action: alertMute, Reason: reason} }
	repeated := alertDecision{Action: alertRepeat, Reason: alertReasonRepeat, Repeats: true}

	cases := []struct {
		name  string
		rules string
		alert alertIn
		want  alertDecision
	}{
		{"due", repeat15, ongoing(nil), repeated},
		{"a minute early is due", repeat15, ongoing(func(a *alertIn) { a.LastNotifiedMillis = minutesAgo(14) }), repeated},
		{"not due", repeat15, ongoing(func(a *alertIn) { a.LastNotifiedMillis = minutesAgo(10) }), muted(alertReasonNotDue)},
		{"first seen later than last posted", repeat15,
			ongoing(func(a *alertIn) { a.FirstSeenMillis, a.LastNotifiedMillis = minutesAgo(5), minutesAgo(30) }), muted(alertReasonNotDue)},
		{"acknowledged", repeat15, ongoing(func(a *alertIn) { a.Acknowledged = true }), muted(alertReasonAcknowledged)},
		{"snoozed", repeat15, ongoing(func(a *alertIn) { a.Snoozed = true }), muted(alertReasonSnoozed)},
		{"warning never repeats", repeat15, ongoing(func(a *alertIn) { a.Kind, a.Severity = "am", "warning" }), muted(alertReasonRepeatOff)},
		{"critical by label repeats", repeat15, ongoing(func(a *alertIn) { a.Kind, a.Severity = "am", "critical" }), repeated},
		{"off without a repeat", ``, ongoing(nil), muted(alertReasonRepeatOff)},
		{"off at zero", `{"default":{"repeatMinutes":0}}`, ongoing(nil), muted(alertReasonRepeatOff)},
		{"cluster turns it off", `{"default":{"repeatMinutes":15},"clusters":{"` + rulesProd + `":{"repeatMinutes":0}}}`,
			ongoing(nil), muted(alertReasonRepeatOff)},
		{"cluster turns it on", `{"clusters":{"` + rulesProd + `":{"repeatMinutes":5}}}`,
			ongoing(func(a *alertIn) { a.LastNotifiedMillis = minutesAgo(6) }), repeated},
		{"digest cluster does not repeat", `{"default":{"repeatMinutes":15,"digest":true}}`, ongoing(nil), muted(alertReasonRepeatOff)},
		{"a recovery is never ongoing", repeat15, ongoing(func(a *alertIn) { a.Problem = false }), muted(alertReasonRepeatOff)},
		{"quiet hours mute it", `{"default":{"repeatMinutes":15,"quietHours":{"start":"11:00","end":"13:00","tz":"UTC"}}}`,
			ongoing(nil), alertDecision{Action: alertMute, Reason: alertReasonQuiet, Until: rulesAt(t, "2026-06-01T13:00:00Z")}},
		{"except critical lets it through",
			`{"default":{"repeatMinutes":15,"quietHours":{"start":"11:00","end":"13:00","tz":"UTC","exceptCritical":true}}}`,
			ongoing(nil), repeated},
		{"minimum severity critical keeps it", `{"default":{"repeatMinutes":15,"minSeverity":"critical"}}`, ongoing(nil), repeated},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runAlertRules(t, c.rules, []alertIn{c.alert}, now)
			c.want.Cluster, c.want.Key = c.alert.Cluster, c.alert.Key

			if len(got.Decisions) != 1 || got.Decisions[0] != c.want {
				t.Errorf("decisions = %+v, want %+v", got.Decisions, c.want)
			}

			if len(got.Digests) != 0 {
				t.Errorf("digests = %+v, want none", got.Digests)
			}
		})
	}
}

// TestAlertRulesRepeatsFlag: a posted critical alert is marked "repeats" when its rule repeats
// it, so the app offers Acknowledge; a warning, a recovery or a digest cluster's are not.
func TestAlertRulesRepeatsFlag(t *testing.T) {
	now := rulesAt(t, "2026-06-01T12:00:00Z")
	node := alertIn{Cluster: rulesProd, Key: "node:10.0.0.1", Kind: "node", Problem: true}
	cases := []struct {
		name  string
		rules string
		alert alertIn
		want  bool
	}{
		{"critical with a repeat", repeat15, node, true},
		{"critical without a repeat", ``, node, false},
		{"warning", repeat15, alertIn{Cluster: rulesProd, Key: "am:slow", Kind: "am", Severity: "warning", Problem: true}, false},
		{"recovery", repeat15, alertIn{Cluster: rulesProd, Key: "node:10.0.0.1", Kind: "node"}, false},
		{"digest cluster", `{"default":{"repeatMinutes":15,"digest":true}}`, node, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runAlertRules(t, c.rules, []alertIn{c.alert}, now)
			if len(got.Decisions) != 1 || got.Decisions[0].Repeats != c.want {
				t.Errorf("decisions = %+v, want repeats %v", got.Decisions, c.want)
			}
		})
	}
}

// Europe/Paris goes from CET to CEST on 2026-03-29 at 02:00 and back on 2026-10-25 at 03:00:
// the window keeps its wall-clock times, so the night is an hour shorter or longer.
func TestAlertRulesDST(t *testing.T) {
	warning := alertIn{Cluster: rulesProd, Key: "am:slow", Kind: "am", Severity: "warning", Problem: true}

	cases := []struct {
		name, now, until string
	}{
		{"spring night starts in CET", "2026-03-28T22:30:00Z", "2026-03-29T05:00:00Z"},  // 23:30 CET → 07:00 CEST
		{"spring morning in CEST", "2026-03-29T04:30:00Z", "2026-03-29T05:00:00Z"},      // 06:30 CEST
		{"autumn night starts in CEST", "2026-10-24T21:30:00Z", "2026-10-25T06:00:00Z"}, // 23:30 CEST → 07:00 CET
		{"autumn repeated hour", "2026-10-25T01:30:00Z", "2026-10-25T06:00:00Z"},        // second 02:30, CET
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runAlertRules(t, parisNight, []alertIn{warning}, rulesAt(t, c.now))
			if d := got.Decisions[0]; d.Action != alertMute || d.Until != rulesAt(t, c.until) {
				t.Errorf("decision = %+v, want mute until %s", d, c.until)
			}
		})
	}

	got := runAlertRules(t, parisNight, []alertIn{warning}, rulesAt(t, "2026-03-29T05:00:00Z")) // 07:00 CEST
	if got.Decisions[0].Action != alertNotify {
		t.Errorf("07:00 CEST on the spring day: %+v, want notify", got.Decisions[0])
	}
}

func TestAlertRulesClusterOverride(t *testing.T) {
	rules := `{
		"default":{"quietHours":{"start":"23:00","end":"07:00","tz":"UTC","exceptCritical":true},"minSeverity":"warning"},
		"clusters":{
			"` + rulesStaging + `":{"digest":true,"minSeverity":"all"},
			"` + rulesLab + `":{"quietHours":{"start":"","end":""}}
		}}`
	alerts := []alertIn{
		{Cluster: rulesProd, Key: "p1", Kind: "am", Severity: "warning", Problem: true},
		{Cluster: rulesStaging, Key: "s1", Kind: "am", Severity: "info", Problem: true},
		{Cluster: rulesLab, Key: "l1", Kind: "am", Severity: "warning", Problem: true},
		{Cluster: rulesLab, Key: "l2", Kind: "am", Severity: "info", Problem: true},
	}

	night := runAlertRules(t, rules, alerts, rulesAt(t, "2026-06-01T23:30:00Z"))
	until := rulesAt(t, "2026-06-02T07:00:00Z")
	want := []alertDecision{
		{Cluster: rulesProd, Key: "p1", Action: alertMute, Reason: alertReasonQuiet, Until: until},
		{Cluster: rulesStaging, Key: "s1", Action: alertMute, Reason: alertReasonQuiet, Until: until}, // keeps the default's window
		{Cluster: rulesLab, Key: "l1", Action: alertNotify},                                           // its window is off
		{Cluster: rulesLab, Key: "l2", Action: alertMute, Reason: alertReasonSeverity},                // keeps the default's floor
	}

	if !reflect.DeepEqual(night.Decisions, want) {
		t.Errorf("night decisions = %+v\nwant %+v", night.Decisions, want)
	}

	day := runAlertRules(t, rules, alerts, rulesAt(t, "2026-06-01T12:00:00Z"))
	if d := day.Decisions[1]; d.Action != alertDigest {
		t.Errorf("staging by day = %+v, want digest (its floor is all)", d)
	}
}

func TestAlertRulesDigests(t *testing.T) {
	rules := `{"clusters":{"` + rulesStaging + `":{"digest":true},"` + rulesLab + `":{"digest":true}}}`
	alerts := []alertIn{{Cluster: rulesProd, Key: "p1", Kind: "am", Severity: "critical", Problem: true}}

	for i, sev := range []string{"critical", "warning", "info", "", "warning", "warning", "critical"} {
		alerts = append(alerts, alertIn{Cluster: rulesStaging, Key: "s" + string(rune('1'+i)), Kind: "am", Severity: sev, Problem: true})
	}

	alerts = append(alerts,
		alertIn{Cluster: rulesLab, Key: "l1", Kind: "etcd", Problem: true},
		alertIn{Cluster: rulesStaging, Key: "s-snoozed", Kind: "am", Severity: "critical", Problem: true, Snoozed: true},
	)

	got := runAlertRules(t, rules, alerts, rulesAt(t, "2026-06-01T12:00:00Z"))
	want := []alertDigestGroup{
		{Cluster: rulesStaging, Count: 7, Critical: 2, Warning: 4, FirstKeys: []string{"s1", "s2", "s3", "s4", "s5"}},
		{Cluster: rulesLab, Count: 1, Critical: 1, FirstKeys: []string{"l1"}},
	}

	if !reflect.DeepEqual(got.Digests, want) {
		t.Errorf("digests = %+v\nwant %+v", got.Digests, want)
	}

	if got.Decisions[0].Action != alertNotify || got.Decisions[len(got.Decisions)-1].Reason != alertReasonSnoozed {
		t.Errorf("decisions = %+v", got.Decisions)
	}
}

func TestAlertRulesEmptyAnswer(t *testing.T) {
	out, err := AlertRulesApply(``, `[]`, 0)
	if err != nil {
		t.Fatal(err)
	}

	if out != `{"decisions":[],"digests":[]}` {
		t.Errorf("out = %s", out)
	}
}

func TestAlertRulesErrors(t *testing.T) {
	cases := []struct {
		name, rules, alerts, want string
	}{
		{"unknown rule field", `{"default":{"quiet":true}}`, `[]`, `unknown field "quiet"`},
		{"unknown top field", `{"defaults":{}}`, `[]`, `unknown field "defaults"`},
		{"unknown quiet field", `{"default":{"quietHours":{"from":"23:00"}}}`, `[]`, `unknown field "from"`},
		{"bad start names the cluster", `{"clusters":{"` + rulesLab + `":{"quietHours":{"start":"25:00","end":"07:00","tz":"UTC"}}}}`, `[]`,
			"alert rule " + rulesLab + ": quiet hours: start \"25:00\": want HH:MM"},
		{"short time", `{"default":{"quietHours":{"start":"7:00","end":"08:00","tz":"UTC"}}}`, `[]`, `start "7:00": want HH:MM`},
		{"bad zone", `{"default":{"quietHours":{"start":"23:00","end":"07:00","tz":"Mars/Olympus"}}}`, `[]`,
			"alert rule default: quiet hours: time zone \"Mars/Olympus\" is not an IANA zone"},
		{"Local is not a zone", `{"default":{"quietHours":{"start":"23:00","end":"07:00","tz":"Local"}}}`, `[]`, `"Local" is not an IANA zone`},
		{"no zone", `{"default":{"quietHours":{"start":"23:00","end":"07:00"}}}`, `[]`, "no time zone"},
		{"bad floor", `{"clusters":{"` + rulesProd + `":{"minSeverity":"high"}}}`, `[]`, "alert rule " + rulesProd + `: minSeverity "high"`},
		{"repeat too long", `{"default":{"repeatMinutes":1441}}`, `[]`, "alert rule default: repeatMinutes 1441: want 0 to 1440"},
		{"negative repeat", `{"clusters":{"` + rulesLab + `":{"repeatMinutes":-5}}}`, `[]`, "alert rule " + rulesLab + ": repeatMinutes -5"},
		{"trailing data", `{} {}`, `[]`, "data after the rules object"},
		{"bad alerts", `{}`, `{}`, "invalid alerts"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := AlertRulesApply(c.rules, c.alerts, 0)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to contain %q", err, c.want)
			}
		})
	}
}

// TestAlertRulesProbeSamples keeps the probe's samples (go/cmd/probe/testdata/alert-rules*.json)
// valid: they decode and give one decision per alert.
func TestAlertRulesProbeSamples(t *testing.T) {
	rules, err := os.ReadFile("../cmd/probe/testdata/alert-rules.json")
	if err != nil {
		t.Fatal(err)
	}

	alerts, err := os.ReadFile("../cmd/probe/testdata/alert-rules-alerts.json")
	if err != nil {
		t.Fatal(err)
	}

	var in []alertIn
	if err := json.Unmarshal(alerts, &in); err != nil {
		t.Fatal(err)
	}

	got := runAlertRules(t, string(rules), in, rulesAt(t, "2026-06-01T21:30:00Z"))
	if len(got.Decisions) != len(in) || len(got.Digests) == 0 {
		t.Errorf("samples: %+v", got)
	}
}
