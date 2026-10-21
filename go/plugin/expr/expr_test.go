package expr

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixture is a kube-green SleepInfo with the shapes paths meet: nested maps, lists of maps,
// dotted label keys, conditions, numbers decoded as float64.
const fixture = `{
  "metadata": {"name": "nightly", "namespace": "demo-shop",
    "labels": {"app": "shop", "kube-green.com/managed": "true"}},
  "spec": {"weekdays": "1-5", "sleepAt": "20:00", "wakeUpAt": "08:00", "timeZone": "Europe/Paris",
    "suspendCronJobs": false, "replicas": 3, "ratio": 0.75, "memory": "2Gi", "cpu": "250m",
    "excludeRef": [{"kind": "Deployment", "name": "api"}, {"kind": "StatefulSet", "name": "db"}],
    "tags": ["a", "b"]},
  "status": {"operation": "SLEEP", "lastScheduleTime": "2026-10-15T06:00:00Z",
    "conditions": [
      {"type": "Ready", "status": "True", "reason": "Scheduled"},
      {"type": "Degraded", "status": "False"}]}
}`

// now is a Thursday, noon UTC.
var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func fixtureEnv(t testing.TB) Env {
	t.Helper()

	var obj any
	if err := json.Unmarshal([]byte(fixture), &obj); err != nil {
		t.Fatal(err)
	}

	return Env{
		Object:       obj,
		Fields:       map[string]Value{"asleep": true, "wakeUpAt": "08:00", "used": int64(90), "capacity": int64(100)},
		Health:       "warning",
		Left:         map[string]any{"metadata": map[string]any{"name": "weekend", "namespace": "demo-shop"}},
		DataServices: map[string]Value{"summary": map[string]any{"total": int64(2), "asleep": int64(1)}},
		Params:       map[string]Value{"replicas": int64(5)},
		Now:          now,
	}
}

func TestEval(t *testing.T) {
	tests := []struct {
		name, src string
		want      Value
		err       string // a substring of the error, "" for none
	}{
		// Paths.
		{"member", `$.metadata.name`, "nightly", ""},
		{"nested", `$.status.operation`, "SLEEP", ""},
		{"integral number is int", `$.spec.replicas`, int64(3), ""},
		{"float stays float", `$.spec.ratio`, 0.75, ""},
		{"bool", `$.spec.suspendCronJobs`, false, ""},
		{"index", `$.spec.excludeRef[0].name`, "api", ""},
		{"negative index", `$.spec.excludeRef[-1].name`, "db", ""},
		{"index out of range", `$.spec.excludeRef[5].name`, nil, ""},
		{"wildcard", `$.spec.excludeRef[*].name`, []any{"api", "db"}, ""},
		{"wildcard over map is sorted", `$.metadata.labels[*]`, []any{"shop", "true"}, ""},
		{"filter", `$.status.conditions[?(@.type == "Ready")].status`, []any{"True"}, ""},
		{"filter single quotes", `$.status.conditions[?(@.type == 'Degraded')].status | first()`, "False", ""},
		{"filter no match", `$.status.conditions[?(@.type == "Gone")].status`, []any{}, ""},
		{"index applies to each match", `$.spec.excludeRef[?(@.kind != "Deployment")][0].name`, []any{}, ""},
		{"filter then first", `$.spec.excludeRef[?(@.kind != "Deployment")] | first()`, map[string]any{"kind": "StatefulSet", "name": "db"}, ""},
		{"quoted key", `$.metadata.labels["kube-green.com/managed"]`, "true", ""},
		{"quoted key at root", `$["metadata"]["name"]`, "nightly", ""},
		{"computed key", `$.metadata["na" + "me"]`, "nightly", ""},
		{"missing path is null", `$.spec.nothing.deeper`, nil, ""},
		{"path through a string is null", `$.metadata.name.first`, nil, ""},
		{"index a map with an int is null", `$.metadata[0]`, nil, ""},
		{"index with a bool fails", `$.metadata[true]`, nil, "index must be"},
		{"null index is null", `$.metadata[null]`, nil, ""},
		{"list literal index", `["x", "y"][1]`, "y", ""},
		{"parenthesised path", `($.spec).weekdays`, "1-5", ""},

		// Roots.
		{"fields", `fields.asleep`, true, ""},
		{"missing field", `fields.nope`, nil, ""},
		{"health", `health`, "warning", ""},
		{"left", `left.metadata.name`, "weekend", ""},
		{"dataServices", `dataServices.summary.total`, int64(2), ""},
		{"params", `params.replicas`, int64(5), ""},
		{"now", `now`, now.UnixMilli(), ""},
		{"conditions sugar true", `conditions.Ready`, true, ""},
		{"conditions sugar false", `conditions.Degraded`, false, ""},
		{"conditions sugar missing", `conditions.Synced`, nil, ""},
		{"not a missing condition", `!conditions.Synced`, true, ""},
		{"unknown name", `nope`, nil, `unknown name "nope"`},

		// Literals and operators.
		{"int", `42`, int64(42), ""},
		{"float", `1.5e2`, 150.0, ""},
		{"string escapes", `"a\"b\n"`, "a\"b\n", ""},
		{"unicode escape", `"é"`, "é", ""},
		{"null literal", `null`, nil, ""},
		{"list literal", `[1, "a", true]`, []any{int64(1), "a", true}, ""},
		{"empty list", `[]`, []any{}, ""},
		{"precedence", `1 + 2 * 3`, int64(7), ""},
		{"parentheses", `(1 + 2) * 3`, int64(9), ""},
		{"subtraction left assoc", `10 - 3 - 2`, int64(5), ""},
		{"division is float", `7 / 2`, 3.5, ""},
		{"integral division is int", `6 / 3`, int64(2), ""},
		{"division by zero is null", `1 / 0`, nil, ""},
		{"int and float", `$.spec.replicas * 0.5`, 1.5, ""},
		{"ratio", `fields.used / fields.capacity > 0.8`, true, ""},
		{"negation", `-$.spec.replicas`, int64(-3), ""},
		{"negate a string fails", `-"a"`, nil, "cannot negate"},
		{"string concat", `"sleepinfo-" + $.metadata.name`, "sleepinfo-nightly", ""},
		{"list concat", `[1] + [2]`, []any{int64(1), int64(2)}, ""},
		{"string plus int fails", `"a" + 1`, nil, "cannot add string and int"},
		{"bool arithmetic fails", `true * 2`, nil, "cannot apply *"},
		{"null arithmetic is null", `$.nope + 1`, nil, ""},
		{"equal numbers across types", `$.spec.replicas == 3.0`, true, ""},
		{"not equal", `$.status.operation != "SLEEP"`, false, ""},
		{"null equals null", `$.nope == null`, true, ""},
		{"null is not empty", `$.nope == ""`, false, ""},
		{"null ordering is false", `$.nope < 1`, false, ""},
		{"null ordering is false both ways", `$.nope >= 1`, false, ""},
		{"string ordering", `"a" < "b"`, true, ""},
		{"mixed ordering is false", `"a" < 1`, false, ""},
		{"le ge", `3 <= 3 && 3 >= 3`, true, ""},
		{"and or", `true && false || true`, true, ""},
		{"short circuit and", `false && nope`, false, ""},
		{"short circuit or", `true || nope`, true, ""},
		{"not", `!fields.asleep`, false, ""},
		{"truthy empty string", `!""`, true, ""},
		{"in list", `"b" in $.spec.tags`, true, ""},
		{"in map", `"app" in $.metadata.labels`, true, ""},
		{"in string", `"night" in $.metadata.name`, true, ""},
		{"not in", `!("z" in $.spec.tags)`, true, ""},
		{"ternary", `fields.asleep ? "Asleep" : "Awake"`, "Asleep", ""},
		{"nested ternary right assoc", `false ? 1 : true ? 2 : 3`, int64(2), ""},
		{"deep equal lists", `$.spec.tags == ["a", "b"]`, true, ""},
		{"large ints compare exactly", `9007199254740993 == 9007199254740992`, false, ""},
		{"large ints order exactly", `1700000000000000001 > 1700000000000000000`, true, ""},
		{"int overflow goes float", `9223372036854775807 + 1`, 9223372036854775808.0, ""},
		{"int multiply overflow goes float", `4611686018427387904 * 4`, 18446744073709551616.0, ""},
		{"int of an out-of-range float is null", `int(1e19)`, nil, ""},
		{"deep equal maps", `$.spec.excludeRef[0] == $.spec.excludeRef[0]`, true, ""},

		// Pipes: the left side of | is everything before it, and the expression goes on.
		{"pipe", `$.spec.wakeUpAt | default("")`, "08:00", ""},
		{"pipe takes the whole left side", `1 + 2 | string()`, "3", ""},
		{"pipe then comparison", `$.spec.memory | quantity() > 1000`, true, ""},
		{"pipe then ternary", `$.nope | default(0) > 1 ? "big" : "small"`, "small", ""},
		{"pipe chain", `$.metadata.name | upper() | lower()`, "nightly", ""},
		{"pipe then path", `$.spec.excludeRef | first().kind`, "Deployment", ""},
		{"pipe inside arguments", `len($.spec.nope | default([]))`, int64(0), ""},
		{"pipe after ternary takes it all", `fields.asleep ? "a" : "b" | upper()`, "A", ""},
		{"parenthesised ternary branch", `fields.asleep ? "a" : ("b" | upper())`, "a", ""},

		// Functions.
		{"default keeps false", `$.spec.suspendCronJobs | default(true)`, false, ""},
		{"default replaces null", `$.spec.suspendDeployments | default(true)`, true, ""},
		{"has", `has($.spec.wakeUpAt)`, true, ""},
		{"has missing", `has($.spec.nope)`, false, ""},
		{"len string runes", `len("héllo")`, int64(5), ""},
		{"len list", `len($.spec.excludeRef)`, int64(2), ""},
		{"len map", `len($.metadata.labels)`, int64(2), ""},
		{"len null", `len($.nope)`, int64(0), ""},
		{"len number fails", `len(3)`, nil, "no length"},
		{"lower", `lower("ABC")`, "abc", ""},
		{"upper null", `upper($.nope)`, "", ""},
		{"join", `$.spec.tags | join("+")`, "a+b", ""},
		{"join a projection", `$.spec.excludeRef[*].kind | join(", ")`, "Deployment, StatefulSet", ""},
		{"split", `"a,b,,c" | split(",")`, []any{"a", "b", "", "c"}, ""},
		{"split empty", `"" | split(",")`, []any{}, ""},
		{"int of string", `int("42")`, int64(42), ""},
		{"int truncates", `int(3.9)`, int64(3), ""},
		{"int of junk is null", `int("x")`, nil, ""},
		{"float of string", `float("2.5")`, 2.5, ""},
		{"float of bool", `float(true)`, 1.0, ""},
		{"string of number", `string(3.25)`, "3.25", ""},
		{"string of list", `string($.spec.tags)`, "a, b", ""},
		{"string of null", `string(null)`, "", ""},
		{"quantity binary", `quantity($.spec.memory)`, int64(2 << 30), ""},
		{"quantity milli", `quantity($.spec.cpu)`, 0.25, ""},
		{"quantity junk", `quantity("lots")`, nil, ""},
		{"quantity number", `quantity(5)`, int64(5), ""},
		{"bytes", `$.spec.memory | bytes()`, "2 GiB", ""},
		{"bytes fraction", `bytes(1610612736)`, "1.5 GiB", ""},
		{"bytes small", `bytes(512)`, "512 B", ""},
		{"bytes null", `bytes($.nope)`, "", ""},
		{"time", `$.status.lastScheduleTime | time()`, time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"time missing is 0", `time($.nope)`, int64(0), ""},
		{"time junk is 0", `time("yesterday")`, int64(0), ""},
		{"age", `age($.status.lastScheduleTime)`, int64(6 * time.Hour / time.Millisecond), ""},
		{"age missing", `age($.nope)`, nil, ""},
		{"relativeTime past", `$.status.lastScheduleTime | relativeTime()`, "6h ago", ""},
		{"relativeTime future", `relativeTime(now + 3 * 86400000)`, "in 3d", ""},
		{"relativeTime minutes", `relativeTime(now - 300000)`, "5m ago", ""},
		{"relativeTime now", `relativeTime(now)`, "now", ""},
		{"relativeTime centuries ago", `relativeTime(-10000000000000)`, "136482d ago", ""},
		{"duration", `duration("1h0m0s")`, int64(3600000), ""},
		{"duration minutes", `duration("30m")`, int64(1800000), ""},
		{"duration mixed", `duration("1h30m15.5s")`, int64(5415500), ""},
		{"duration zero", `duration("0s")`, int64(0), ""},
		{"duration spaces", `duration(" 5m ")`, int64(300000), ""},
		{"duration junk", `duration("an hour")`, nil, ""},
		{"duration no unit", `duration("60")`, nil, ""},
		{"duration missing", `duration($.nope)`, nil, ""},
		{"duration number", `duration(1500)`, nil, ""},
		{"cronNext huge step", `cronNext("5/9223372036854775807 * * * *")`, time.Date(2026, 10, 15, 12, 5, 0, 0, time.UTC).UnixMilli(), ""},
		{"relativeTime never", `relativeTime(0)`, "never", ""},
		// Thursday noon UTC, Paris is UTC+2: 20:00 Paris is 18:00 UTC the same day.
		{"cronNext", `cronNext($.spec.sleepAt, $.spec.weekdays, $.spec.timeZone)`, time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronNext defaults", `cronNext("13:30")`, time.Date(2026, 10, 15, 13, 30, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronNext weekend", `cronNext("08:00", "6,0", "UTC")`, time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronNext full cron", `cronNext("0 0 1 * *")`, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronNext bad", `cronNext("25:99")`, int64(0), ""},
		{"cronNext bad zone", `cronNext("08:00", "*", "Mars/Olympus")`, int64(0), ""},
		{"cronNext never", `cronNext("0 0 31 2 *")`, int64(0), ""},
		// Thursday noon UTC: the last weekday 20:00 Paris was Wednesday 18:00 UTC.
		{"cronPrev", `cronPrev($.spec.sleepAt, $.spec.weekdays, $.spec.timeZone)`, time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev today", `cronPrev("08:00")`, time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev strictly before now", `cronPrev("12:00")`, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev weekend", `cronPrev("08:00", "6,0", "UTC")`, time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev every minute", `cronPrev("*:*")`, time.Date(2026, 10, 15, 11, 59, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev full cron", `cronPrev("0 0 1 * *")`, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev last year", `cronPrev("0 0 1 12 *")`, time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev before a time", `cronPrev("11:55", "*", "UTC", now - 600000)`, time.Date(2026, 10, 14, 11, 55, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev before a timestamp", `cronPrev("08:00", "*", "UTC", "2026-10-15T07:00:00Z")`, time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC).UnixMilli(), ""},
		{"cronPrev bad", `cronPrev("2000")`, int64(0), ""},
		{"cronPrev bad zone", `cronPrev("08:00", "*", "Mars/Olympus")`, int64(0), ""},
		{"cronPrev never", `cronPrev("0 0 31 2 *")`, int64(0), ""},
		{"first list", `first($.spec.tags)`, "a", ""},
		{"first empty", `first([])`, nil, ""},
		{"first scalar", `first("x")`, "x", ""},
		{"contains string", `contains($.metadata.name, "night")`, true, ""},
		{"contains list", `$.spec.tags | contains("c")`, false, ""},
		{"matches", `$.metadata.name | matches("^night")`, true, ""},
		{"matches no", `matches("weekend", "^night")`, false, ""},
		{"matches dynamic pattern", `matches("abc", "a" + "b")`, true, ""},
		{"matches dynamic bad pattern", `matches("abc", "(" + "")`, nil, "matches:"},
		{"coalesce", `coalesce($.nope, $.spec.nope, "x", "y")`, "x", ""},
		{"coalesce all null", `coalesce($.nope)`, nil, ""},
		{"condition", `condition("Ready").reason`, "Scheduled", ""},
		{"condition piped", `($ | condition("Degraded")).status`, "False", ""},
		{"condition missing", `condition("Synced")`, nil, ""},
		{"condition type must be a string", `condition(1)`, nil, "must be a string"},
		{"unknown function", `nope(1)`, nil, `unknown function "nope"`},
		{"wrong arity", `lower("a", "b")`, nil, "lower takes 1 argument, got 2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse(tc.src)
			if err == nil {
				var got Value

				got, err = n.Eval(fixtureEnv(t))
				if err == nil && tc.err == "" && !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("%s = %#v, want %#v", tc.src, got, tc.want)
				}
			}

			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("%s: %v", tc.src, err)
			case tc.err != "" && err == nil:
				t.Fatalf("%s: no error, want %q", tc.src, tc.err)
			case tc.err != "" && !strings.Contains(err.Error(), tc.err):
				t.Fatalf("%s: error %q, want %q", tc.src, err, tc.err)
			}
		})
	}
}

func TestParseErrorsCarryPosition(t *testing.T) {
	tests := []struct {
		src  string
		pos  Pos
		want string
	}{
		{`$.a ==`, Pos{1, 7}, "unexpected end of expression"},
		{`$.a = 1`, Pos{1, 5}, "compare with =="},
		{`$.a & 1`, Pos{1, 5}, "and is &&"},
		{"$.a &&\n  $.b )", Pos{2, 7}, `unexpected ")"`},
		{`"open`, Pos{1, 1}, "unterminated string"},
		{`"\q"`, Pos{1, 1}, "unknown escape"},
		{`@.x`, Pos{1, 1}, "@ is only defined inside a filter"},
		{`$.`, Pos{1, 3}, "expected a field name"},
		{`$.a[1`, Pos{1, 6}, `expected "]"`},
		{`f(1,`, Pos{1, 5}, "unexpected end"},
		{`$.a | 3`, Pos{1, 7}, "expected a function after |"},
		{`true ? 1`, Pos{1, 9}, `expected ":"`},
		{`matches($.a, "(")`, Pos{1, 14}, "bad regular expression"},
		{`matches($.a, 1)`, Pos{1, 14}, "string pattern"},
		{`#`, Pos{1, 1}, "unexpected character"},
		{`1 2`, Pos{1, 3}, "unexpected"},
		{`in`, Pos{1, 1}, "unexpected"},
		{strings.Repeat("(", 40) + "1" + strings.Repeat(")", 40), Pos{1, 33}, "nested deeper"},
		{strings.Repeat("!", 40) + "true", Pos{1, 32}, "nested deeper"},
	}

	for _, tc := range tests {
		_, err := Parse(tc.src)

		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("%q: error %v, want an *Error", tc.src, err)
		}

		if e.Pos != tc.pos || !strings.Contains(e.Msg, tc.want) {
			t.Errorf("%q: %v at %v, want %q at %v", tc.src, e.Msg, e.Pos, tc.want, tc.pos)
		}
	}
}

func TestLimits(t *testing.T) {
	big := make([]any, MaxSteps+1)
	for i := range big {
		big[i] = map[string]any{"n": float64(i)}
	}

	long := strings.Repeat("x", MaxStringLen/2+1)

	tests := []struct {
		name, src string
		obj       any
		want      string
	}{
		{"wildcard over too many elements", `$.items[*].n`, map[string]any{"items": big}, "exceeds 10000 steps"},
		{"filter over too many elements", `$.items[?(@.n > 0)]`, map[string]any{"items": big}, "exceeds 10000 steps"},
		{"string concatenation", `$.s + $.s`, map[string]any{"s": long}, "longer than 64 KiB"},
		{"function result", `$.s | join("")`, map[string]any{"s": []any{long, long}}, "longer than 64 KiB"},
		// split and join multiply sizes in few steps: every list built is charged by its length.
		{"split bomb", `split(join(split($.s, ""), "yy"), "")`, map[string]any{"s": strings.Repeat("y", 3000)}, "exceeds 10000 steps"},
		{"list concat bomb", `$.l + $.l + $.l`, map[string]any{"l": make([]any, 4000)}, "exceeds 10000 steps"},
		{"string of a huge list", `string($.l)`, map[string]any{"l": []any{long, long, long}}, "longer than 64 KiB"},
		{"matches on a huge list", `$.l | matches("y")`, map[string]any{"l": []any{long, long, long}}, "longer than 64 KiB"},
	}

	for _, tc := range tests {
		n, err := Parse(tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		_, err = n.Eval(Env{Object: tc.obj, Now: now})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.name, err, tc.want)
		}

		if err != nil && !strings.Contains(err.Error(), "(in ") {
			t.Errorf("%s: error %q does not name the expression", tc.name, err)
		}
	}

	if _, err := Parse(strings.Repeat(" ", MaxStringLen+1)); err == nil {
		t.Error("a source over 64 KiB parses")
	}
}

// Chains are not nesting: a long && rule, a deep path or a pipe chain evaluate, and Check
// stays linear on the longest chain a source can hold.
func TestLongChains(t *testing.T) {
	for _, src := range []string{
		strings.Repeat(`$.spec.replicas == 3 && `, 40) + "true",
		"$" + strings.Repeat(".a", 100),
		`$.metadata.name` + strings.Repeat(` | upper()`, 100),
	} {
		n, err := Parse(src)
		if err != nil {
			t.Fatalf("%.40s…: %v", src, err)
		}

		if _, err := n.Eval(fixtureEnv(t)); err != nil {
			t.Fatalf("%.40s…: %v", src, err)
		}
	}

	n, err := Parse(strings.Repeat("1+", 32_000) + "1")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if p := n.Check(Scope{Bool: true}); len(p) != 1 || time.Since(start) > time.Second {
		t.Fatalf("Check took %v: %v", time.Since(start), p)
	}
}

func TestRegister(t *testing.T) {
	Register("double", Func{Min: 1, Max: 1, Returns: KindNumber, Call: func(_ Ctx, a []Value) (Value, error) {
		n, _ := number(a[0])

		return n * 2, nil
	}})
	t.Cleanup(func() { delete(registry, "double") })

	n, err := Parse(`$.spec.replicas | double()`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := n.Eval(fixtureEnv(t))
	if err != nil || got != 6.0 {
		t.Fatalf("double = %v, %v", got, err)
	}
}

func TestNowDefaultsToWallClock(t *testing.T) {
	n, err := Parse(`now`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := n.Eval(Env{})
	if ms, ok := got.(int64); err != nil || !ok || time.Since(time.UnixMilli(ms)) > time.Minute {
		t.Fatalf("now = %v, %v", got, err)
	}
}
