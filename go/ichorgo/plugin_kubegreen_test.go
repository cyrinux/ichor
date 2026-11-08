package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
	"github.com/cyrinux/ichor/go/plugin/expr"
)

// The kube-green built-in plugin against the Go mapping the Data services tab used before it
// (mapKubeGreenSleepInfo, still behind the workload rows): the same SleepInfos, served by the
// same kind of httptest API, must give the same values, health and reasons.

// kubeGreenParitySleepInfos are the SleepInfos of kube_kubegreen_schedule_test.go, plus the
// cases where the mapping gives two reasons, as the API serves them.
var kubeGreenParitySleepInfos = []string{
	// A weekday schedule that ran this morning: ok.
	`{"metadata":{"name":"nights","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00","timeZone":"Europe/Rome"},
	  "status":{"lastScheduleTime":"2026-10-14T07:00:00+02:00","operation":"WAKE_UP"}}`,
	// No wake-up time.
	`{"metadata":{"name":"until-changed","namespace":"sandbox"},"spec":{"weekdays":"1-5","sleepAt":"19:00"},
	  "status":{"lastScheduleTime":"2026-10-13T19:00:00Z","operation":"SLEEP"}}`,
	// One part that does not parse.
	`{"metadata":{"name":"bad-weekdays","namespace":"staging"},"spec":{"weekdays":"9","sleepAt":"20:00","wakeUpAt":"07:00"}}`,
	`{"metadata":{"name":"bad-hour","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"25:00","wakeUpAt":"07:00"}}`,
	`{"metadata":{"name":"bad-format","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"2000","wakeUpAt":"07:00"}}`,
	`{"metadata":{"name":"bad-zone","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00","timeZone":"Mars/Phobos"}}`,
	`{"metadata":{"name":"bad-wake-up","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"7"}}`,
	// The controller missed its last expected run.
	`{"metadata":{"name":"overdue","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00","timeZone":"Europe/Rome"},
	  "status":{"lastScheduleTime":"2026-10-12T07:00:00+02:00","operation":"WAKE_UP"}}`,
	// Never ran.
	`{"metadata":{"name":"fresh","namespace":"staging"},"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"07:00"}}`,
	// Explicit flags and scope counts.
	`{"metadata":{"name":"flags","namespace":"preview"},"spec":{"weekdays":"*","sleepAt":"22:30","wakeUpAt":"06:00",
	  "suspendDeployments":false,"suspendStatefulSets":false,"suspendCronJobs":true,
	  "excludeRef":[{"kind":"Deployment","name":"api"},{"kind":"Deployment","name":"web"}],
	  "includeRef":[{"kind":"StatefulSet","name":"db"}],"patches":[{"target":{"group":"apps","kind":"Deployment"}}]},
	  "status":{"lastScheduleTime":"2026-10-14T06:00:00Z","operation":"WAKE_UP"}}`,
	// Two reasons: the mapping lists both, the plugin keeps the first rule's.
	`{"metadata":{"name":"never-woken","namespace":"sandbox"},"spec":{"weekdays":"1-5","sleepAt":"19:00"}}`,
	`{"metadata":{"name":"broken-no-wake","namespace":"sandbox"},"spec":{"weekdays":"1-5","sleepAt":"nope"}}`,
	`{"metadata":{"name":"late-no-wake","namespace":"sandbox"},"spec":{"weekdays":"1-5","sleepAt":"19:00"},
	  "status":{"lastScheduleTime":"2026-10-12T19:00:00Z","operation":"SLEEP"}}`,
}

// kubeGreenParityClocks: Wednesday morning (awake), Wednesday night (asleep), Saturday (the
// next runs are Monday's), and the data-services fixture's clock.
var kubeGreenParityClocks = []time.Time{
	time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC),
	time.Date(2026, 10, 14, 21, 0, 0, 0, time.UTC),
	time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC),
	fixtureNow,
	// Just after a sleep was due (20:00 Rome), then past its grace.
	time.Date(2026, 10, 14, 18, 0, 30, 0, time.UTC),
	time.Date(2026, 10, 14, 18, 11, 0, 0, time.UTC),
}

// kubeGreenReasonOrder is the order of the plugin's health rules: the reason the plugin
// keeps when the mapping gives several.
var kubeGreenReasonOrder = []string{kubeGreenReasonInvalidSchedule, kubeGreenReasonNeverRan, kubeGreenReasonOverdue, kubeGreenReasonNoWakeUp}

func kubeGreenPrimaryReason(reasons []string) string {
	for _, r := range kubeGreenReasonOrder {
		if slices.Contains(reasons, r) {
			return r
		}
	}

	return ""
}

// kubeGreenBuiltin is the built-in plugin, made the only one the cluster calls know, at now.
func kubeGreenBuiltin(t *testing.T, now time.Time) *plugin.Compiled {
	t.Helper()

	return useBuiltinPlugin(t, "kube-green", now)
}

// kubeGreenNative maps the parity SleepInfos as the former Data services reader did.
func kubeGreenNative(t *testing.T, now time.Time) map[string]kubeGreenSleepInfo {
	t.Helper()

	out := map[string]kubeGreenSleepInfo{}

	for _, raw := range kubeGreenParitySleepInfos {
		var obj sleepInfoObject
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			t.Fatal(err)
		}

		s := mapKubeGreenSleepInfo(obj, now)
		out[s.Namespace+"/"+s.Name] = s
	}

	return out
}

// Every field of the plugin, evaluated on its own, equals the mapping's value.
func TestKubeGreenPluginFieldsMatchTheMapping(t *testing.T) {
	for _, now := range kubeGreenParityClocks {
		c := kubeGreenBuiltin(t, now)
		native := kubeGreenNative(t, now)

		for _, raw := range kubeGreenParitySleepInfos {
			fields, key := kubeGreenPluginFields(t, c, raw, now)
			s := native[key]
			want := map[string]any{
				"weekdays": s.Weekdays, "sleepAt": s.SleepAt, "wakeUpAt": s.WakeUpAt, "timeZone": orDefault(s.TimeZone, "UTC"),
				"suspendDeployments": s.SuspendDeployments, "suspendStatefulSets": s.SuspendStatefulSets, "suspendCronJobs": s.SuspendCronJobs,
				"excludeCount": int64(s.ExcludeCount), "includeCount": int64(s.IncludeCount), "patchCount": int64(s.PatchCount),
				"operation": s.Operation, "asleep": s.Asleep, "lastScheduleAt": s.LastScheduleAt,
				"nextSleepAt": s.NextSleepAt, "nextWakeUpAt": s.NextWakeUpAt,
				"overdue":       slices.Contains(s.Reasons, kubeGreenReasonOverdue),
				"validSchedule": !slices.Contains(s.Reasons, kubeGreenReasonInvalidSchedule),
			}

			for name, w := range want {
				if got := fields[name]; got != w {
					t.Errorf("%s %s/%s: %s = %v (%T), the mapping says %v", now.Format(time.RFC3339), s.Namespace, s.Name, name, got, got, w)
				}
			}
		}
	}
}

// kubeGreenPluginFields evaluates the plugin's fields over one SleepInfo, as the render does,
// with its namespace/name.
func kubeGreenPluginFields(t *testing.T, c *plugin.Compiled, raw string, now time.Time) (map[string]expr.Value, string) {
	t.Helper()

	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatal(err)
	}

	fields := map[string]expr.Value{}
	env := expr.Env{Object: obj, Fields: fields, Now: now}

	for _, f := range c.Fields["sleepinfo"] {
		if f.Name == "state" { // a join: the render's own function, checked through the tree
			continue
		}

		v, err := f.Expr.Node().Eval(env)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}

		fields[f.Name] = expr.Normalize(v)
	}

	meta := obj["metadata"].(map[string]any)

	return fields, meta["namespace"].(string) + "/" + meta["name"].(string)
}

// A run due 30 seconds ago is not late yet; 11 minutes later it is, in the plugin and in the
// mapping alike.
func TestKubeGreenOverdueGrace(t *testing.T) {
	// Weekdays 20:00 to 07:00 Rome, last run this morning's wake-up; the sleep is due at 18:00 UTC.
	raw := kubeGreenParitySleepInfos[0]

	for _, tc := range []struct {
		now     time.Time
		overdue bool
	}{
		{time.Date(2026, 10, 14, 18, 0, 30, 0, time.UTC), false},
		{time.Date(2026, 10, 14, 18, 9, 0, 0, time.UTC), false},
		{time.Date(2026, 10, 14, 18, 11, 0, 0, time.UTC), true},
	} {
		c := kubeGreenBuiltin(t, tc.now)
		fields, key := kubeGreenPluginFields(t, c, raw, tc.now)
		s := kubeGreenNative(t, tc.now)[key]

		if fields["overdue"] != tc.overdue || slices.Contains(s.Reasons, kubeGreenReasonOverdue) != tc.overdue {
			t.Errorf("%s: plugin overdue %v, mapping reasons %v, want overdue %v", tc.now.Format(time.RFC3339), fields["overdue"], s.Reasons, tc.overdue)
		}
	}
}

// The tree KubePluginData renders over the httptest API says what the mapping says, row by
// row, and so do its summary and findings.
func TestKubeGreenPluginTreeMatchesTheMapping(t *testing.T) {
	for _, now := range kubeGreenParityClocks {
		for _, secretsReadable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s secrets %v", now.Format(time.RFC3339), secretsReadable), func(t *testing.T) {
				kubeGreenBuiltin(t, now)
				f := kubeGreenParityAPI(t, map[bool]int{true: http.StatusOK, false: http.StatusForbidden}[secretsReadable])

				tree := pluginData(t, "cfg", "ctx", "", "kube-green", "", "")
				assertKubeGreenTree(t, tree, kubeGreenNative(t, now), now, secretsReadable)

				for _, r := range f.recorded() {
					if r.path == "/api/v1/secrets" && !strings.Contains(r.query, "labelSelector=app.kubernetes.io%2Fmanaged-by%3Dkube-green") {
						t.Errorf("secrets listed without the manager's label: %s", r.query)
					}
				}
			})
		}
	}
}

// kubeGreenParityAPI serves kube-green with the parity SleepInfos; the state Secrets answer
// secretsStatus: 200 holds sleepinfo-nights, 403 is a kubeconfig that may not list Secrets.
func kubeGreenParityAPI(t *testing.T, secretsStatus int) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                                    kubeGreenGroups,
		"GET /apis/kube-green.com/v1alpha1":            kubeGreenResources,
		"GET /apis/kube-green.com/v1alpha1/sleepinfos": `{"metadata":{},"items":[` + strings.Join(kubeGreenParitySleepInfos, ",") + `]}`,
	})

	switch secretsStatus {
	case http.StatusOK:
		f.answerWith("GET /api/v1/secrets", http.StatusOK, `{"metadata":{},"items":[{"metadata":{"name":"sleepinfo-nights","namespace":"staging",`+
			`"labels":{"app.kubernetes.io/managed-by":"kube-green"}},"type":"Opaque","data":{"last-schedule":"c2VjcmV0"}}]}`)
	case http.StatusForbidden:
		f.answerWith("GET /api/v1/secrets", http.StatusForbidden, forbiddenStatus)
	default:
		f.answerWith("GET /api/v1/secrets", secretsStatus, `{"kind":"Status","status":"Failure","message":"etcd is down","code":`+strconv.Itoa(secretsStatus)+`}`)
	}

	useFakeKube(t, f)

	return f
}

func assertKubeGreenTree(t *testing.T, tree plugin.Tree, native map[string]kubeGreenSleepInfo, now time.Time, secretsReadable bool) {
	t.Helper()

	if !tree.Active || tree.DetectedVia != "api" || len(tree.Cards) != 1 {
		t.Fatalf("tree %+v", tree)
	}

	// The state Secrets are optional: forbidden, they leave no error.
	if len(tree.Errors) != 0 {
		t.Fatalf("errors %+v", tree.Errors)
	}

	rows := tree.Cards[0].Rows
	if len(rows) != len(native) {
		t.Fatalf("%d rows, the mapping has %d SleepInfos", len(rows), len(native))
	}

	var asleep, warnings int

	worst := "ok"

	for _, row := range rows {
		s, ok := native[row.Ref.Namespace+"/"+row.Ref.Name]
		if !ok {
			t.Fatalf("row %s/%s is not in the mapping", row.Ref.Namespace, row.Ref.Name)
		}

		assertKubeGreenRow(t, row, s, now, secretsReadable)

		if s.Asleep {
			asleep++
		}

		if s.Health == healthWarning {
			warnings++
		}

		if healthRank(s.Health) < healthRank(worst) {
			worst = s.Health
		}
	}

	if want := fmt.Sprintf("%d sleep schedules", len(native)); tree.Summary.Title != want {
		t.Errorf("summary title %q, want %q", tree.Summary.Title, want)
	}

	badges := map[string]string{}
	for _, b := range tree.Summary.Badges {
		badges[b.Label] = b.Value
	}

	if badges["asleep"] != countText(asleep) || badges["warnings"] != countText(warnings) {
		t.Errorf("summary badges %+v, want %d asleep and %d warnings", tree.Summary.Badges, asleep, warnings)
	}

	if tree.Summary.Health != worst {
		t.Errorf("summary health %s, want %s", tree.Summary.Health, worst)
	}

	if len(tree.Findings) != warnings {
		t.Errorf("%d findings, want one per warning (%d)", len(tree.Findings), warnings)
	}
}

// countText is a hideWhenZero badge's value: absent ("") at 0.
func countText(n int) string {
	if n == 0 {
		return ""
	}

	return strconv.Itoa(n)
}

func assertKubeGreenRow(t *testing.T, row plugin.TreeRow, s kubeGreenSleepInfo, now time.Time, secretsReadable bool) {
	t.Helper()

	at := s.Namespace + "/" + s.Name
	relative := func(ms int64) string {
		fn, _ := expr.Library("relativeTime")
		v, _ := fn.Call(expr.Ctx{Now: now}, []expr.Value{ms})

		return v.(string)
	}

	if row.Title != s.Name || row.Subtitle != s.Namespace {
		t.Errorf("%s: title %q subtitle %q", at, row.Title, row.Subtitle)
	}

	if row.Health == nil || row.Health.Tone != s.Health || row.Health.Reason != kubeGreenPrimaryReason(s.Reasons) {
		t.Errorf("%s: health %+v, the mapping says %s %v", at, row.Health, s.Health, s.Reasons)
	}

	state := map[bool]string{true: "Asleep", false: "Awake"}[s.Asleep]
	if row.Status == nil || row.Status.Text != state || row.Status.Tone != s.Health {
		t.Errorf("%s: status %+v, want %s %s", at, row.Status, state, s.Health)
	}

	next := s.NextSleepAt
	if s.Asleep {
		next = s.NextWakeUpAt
	}

	if row.Trailing == nil || row.Trailing.Text != relative(next) {
		t.Errorf("%s: trailing %+v, want %q", at, row.Trailing, relative(next))
	}

	values := rowItems(row, "keyValues")
	wake := s.WakeUpAt
	if wake == "" {
		wake = "–"
	}

	for label, want := range map[string]string{
		"Weekdays": s.Weekdays, "Sleeps at": s.SleepAt, "Wakes up at": wake, "Time zone": orDefault(s.TimeZone, "UTC"),
		"Last run": relative(s.LastScheduleAt),
	} {
		if got := values[label]["value"]; got != want {
			t.Errorf("%s: %s = %v, want %q", at, label, got, want)
		}
	}

	badges := rowItems(row, "badges")
	for label, on := range map[string]bool{"Deployments": s.SuspendDeployments, "StatefulSets": s.SuspendStatefulSets, "CronJobs": s.SuspendCronJobs} {
		if want := map[bool]string{true: "ok", false: "idle"}[on]; badges[label]["tone"] != want {
			t.Errorf("%s: %s badge %v, want %s", at, label, badges[label], want)
		}
	}

	for label, n := range map[string]int{"excluded": s.ExcludeCount, "included": s.IncludeCount, "patches": s.PatchCount} {
		if got, _ := badges[label]["value"].(string); got != countText(n) {
			t.Errorf("%s: %s badge %v, want %d", at, label, badges[label], n)
		}
	}

	rel := rowItems(row, "relations")["State secret"]
	if wantRel := secretsReadable && at == "staging/nights"; (rel != nil) != wantRel {
		t.Errorf("%s: state secret relation %v, want one: %v", at, rel, wantRel)
	}
}

// rowItems are the items of the row's sections of one type, by label.
func rowItems(row plugin.TreeRow, typ string) map[string]map[string]any {
	out := map[string]map[string]any{}

	for _, s := range row.Detail {
		items, _ := s.Items.([]any)
		if s.Type != typ {
			continue
		}

		for _, it := range items {
			m, _ := it.(map[string]any)
			label, _ := m["label"].(string)
			out[label] = m
		}
	}

	return out
}

// An optional resource is silent when forbidden (above), not when the server fails.
func TestKubeGreenOptionalSecretsStillReportAFailure(t *testing.T) {
	kubeGreenBuiltin(t, kubeGreenParityClocks[0])
	kubeGreenParityAPI(t, http.StatusInternalServerError)

	tree := pluginData(t, "cfg", "ctx", "", "kube-green", "", "")
	if len(tree.Errors) != 1 || tree.Errors[0].Resource != "secrets" || len(tree.Cards[0].Rows) != len(kubeGreenParitySleepInfos) {
		t.Fatalf("errors %+v, %d rows", tree.Errors, len(tree.Cards[0].Rows))
	}
}

// The French tree takes the apps' former strings.
func TestKubeGreenPluginFrench(t *testing.T) {
	kubeGreenBuiltin(t, kubeGreenParityClocks[0])
	kubeGreenParityAPI(t, http.StatusForbidden)

	tree := pluginData(t, "cfg", "ctx", "", "kube-green", "", "fr")
	if tree.Summary.Title != fmt.Sprintf("%d plannings de veille", len(kubeGreenParitySleepInfos)) || tree.Cards[0].Empty != "Aucun planning de veille." {
		t.Fatalf("summary %+v, empty %q", tree.Summary, tree.Cards[0].Empty)
	}

	for _, row := range tree.Cards[0].Rows {
		if row.Ref.Name == "until-changed" && (row.Health.Message != "pas d'heure de réveil" || row.Status.Text != "Endormi") {
			t.Errorf("until-changed in French: %+v %+v", row.Health, row.Status)
		}
	}
}

// The demo renders the plugin's samples: one per health the plugin gives.
func TestKubeGreenPluginDemo(t *testing.T) {
	kubeGreenBuiltin(t, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))
	noKubeDial(t)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "kube-green", "", "")

	health := map[string]string{}
	for _, row := range tree.Cards[0].Rows {
		health[row.Title] = row.Health.Tone + " " + row.Health.Reason
	}

	want := map[string]string{"nightly": "ok ", "weekend": "warning noWakeUp", "previews": "idle neverRan"}
	if fmt.Sprint(health) != fmt.Sprint(want) || len(tree.Errors) != 0 {
		t.Fatalf("demo rows %v, errors %+v", health, tree.Errors)
	}

	// The samples' last runs follow the clock: no hour of the week finds them overdue.
	for h := range 7 * 24 {
		kubeGreenBuiltin(t, time.Date(2026, 10, 19, h, 30, 0, 0, time.UTC))

		for _, row := range pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "kube-green", "", "").Cards[0].Rows {
			if row.Health.Reason == kubeGreenReasonOverdue {
				t.Fatalf("hour %d: %s is overdue", h, row.Title)
			}
		}
	}
}

// Data services no longer carry kube-green: the plugin does.
func TestDataServicesHaveNoKubeGreen(t *testing.T) {
	raw, err := json.Marshal(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(jsonKeys(raw), "kubeGreen") {
		t.Error("the Data services JSON still has a kubeGreen key")
	}
}
