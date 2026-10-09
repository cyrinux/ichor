package ichorgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withAuditClock fixes the time entries are recorded at, returned as a setter.
func withAuditClock(t *testing.T, at time.Time) func(time.Time) {
	t.Helper()

	now := at
	auditNow = func() time.Time { return now }
	t.Cleanup(func() { auditNow = time.Now })

	return func(next time.Time) { now = next }
}

func readAudit(t *testing.T, cluster, action string) []auditEntry {
	t.Helper()

	out, err := AuditLog(cluster, action)
	if err != nil {
		t.Fatal(err)
	}

	var entries []auditEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return entries
}

func TestAuditRecordsNewestFirstAndFilters(t *testing.T) {
	withDataDir(t)
	setNow := withAuditClock(t, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))

	recordOutcome("", "prod", auditAction{Action: "reboot", Node: "10.0.0.2", Params: "mode=default"}, nil)
	setNow(time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC))
	recordOutcome("", "lab", auditAction{Action: "scale", Namespace: "web", Object: "Deployment/api", Params: "replicas=3"}, errors.New("forbidden"))
	setNow(time.Date(2026, 10, 1, 12, 9, 0, 0, time.UTC))
	recordOutcome("", "prod", auditAction{Action: "scale", Namespace: "web", Object: "Deployment/api"}, nil)

	all := readAudit(t, "", "")
	if len(all) != 3 {
		t.Fatalf("got %d entries, want 3", len(all))
	}

	if all[0].Cluster != "prod" || all[0].Action != "scale" || all[2].Action != "reboot" {
		t.Errorf("not newest first: %+v", all)
	}

	failed := all[1]
	if failed.Outcome != auditFailed || failed.Error != "forbidden" || failed.Params != "replicas=3" {
		t.Errorf("failed entry = %+v", failed)
	}

	if all[2].Outcome != auditOK || all[2].Node != "10.0.0.2" || all[2].At != time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("ok entry = %+v", all[2])
	}

	if got := readAudit(t, "prod", ""); len(got) != 2 {
		t.Errorf("cluster filter: %d entries, want 2", len(got))
	}

	if got := readAudit(t, "prod", "scale"); len(got) != 1 || got[0].Cluster != "prod" {
		t.Errorf("cluster and action filter: %+v", got)
	}
}

func TestAuditEmptyLogIsAnEmptyList(t *testing.T) {
	withDataDir(t)

	if out, err := AuditLog("", ""); err != nil || out != "[]" {
		t.Errorf("AuditLog = %q, %v; want []", out, err)
	}
}

func TestAuditFileIsEncryptedAndSurvivesRestart(t *testing.T) {
	dir := withDataDir(t)
	withAuditClock(t, time.Now())

	recordOutcome("", "secret-cluster", auditAction{Action: "reboot", Node: "10.9.8.7"}, nil)

	data, err := os.ReadFile(filepath.Join(dir, auditFile))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(data, []byte("secret-cluster")) || bytes.Contains(data, []byte("10.9.8.7")) {
		t.Errorf("audit file holds cluster data in clear")
	}

	// A new launch: same directory and key.
	SetDataDir("", nil)
	SetDataDir(dir, testDataKey)

	if got := readAudit(t, "", ""); len(got) != 1 || got[0].Cluster != "secret-cluster" {
		t.Errorf("after restart: %+v", got)
	}
}

func TestAuditNothingRecordedWithoutDataDir(t *testing.T) {
	SetDataDir("", nil)

	recordOutcome("", "prod", auditAction{Action: "reboot"}, nil)

	if got := readAudit(t, "", ""); len(got) != 0 {
		t.Errorf("recorded without a data directory: %+v", got)
	}
}

func TestAuditRetention(t *testing.T) {
	withDataDir(t)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	setNow := withAuditClock(t, start)

	recordOutcome("", "prod", auditAction{Action: "old"}, nil)
	setNow(start.Add(auditRetention + time.Hour))
	recordOutcome("", "prod", auditAction{Action: "new"}, nil)

	got := readAudit(t, "", "")
	if len(got) != 1 || got[0].Action != "new" {
		t.Errorf("entries older than the retention kept: %+v", got)
	}
}

func TestAuditCapsEntries(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	entries := make([]auditEntry, auditMaxEntries)
	for i := range entries {
		entries[i] = auditEntry{At: auditNow().UnixMilli(), Cluster: "prod", Action: "first"}
	}

	if err := writeAuditLocked(entries); err != nil {
		t.Fatal(err)
	}

	recordOutcome("", "prod", auditAction{Action: "last"}, nil)

	got := readAudit(t, "", "")
	if len(got) != auditMaxEntries || got[0].Action != "last" {
		t.Errorf("got %d entries, newest %q; want %d, newest last", len(got), got[0].Action, auditMaxEntries)
	}
}

func TestAuditMarksDemoEntries(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeDeletePod(demo, "Demo cluster", "", "web", "api-1"); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("KubeDeletePod in the demo = %v", err)
	}

	got := readAudit(t, "", "")
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}

	e := got[0]
	if !e.Demo || e.Action != "delete-pod" || e.Namespace != "web" || e.Object != "Pod/api-1" || e.Outcome != auditFailed {
		t.Errorf("demo entry = %+v", e)
	}
}

func TestRecordActionRecordsPanicAsFailure(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	panicking := func() (err error) {
		defer maskErr(&err)
		defer recordAction(&err, "", "prod", auditAction{Action: "reboot"})

		panic("boom")
	}

	if err := panicking(); err == nil {
		t.Fatal("panic not turned into an error")
	}

	if got := readAudit(t, "", ""); len(got) != 1 || got[0].Outcome != auditFailed || got[0].Error == "" {
		t.Errorf("panic entry = %+v", got)
	}
}

func TestAuditRedactsSecrets(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	token := strings.Repeat("aB3", 20)
	recordOutcome("", "prod", auditAction{Action: "edit", Params: "token=" + token}, errors.New("bad password: hunter2 and bearer "+token))

	got := readAudit(t, "", "")[0]
	for _, field := range []string{got.Params, got.Error} {
		if strings.Contains(field, token) || strings.Contains(field, "hunter2") {
			t.Errorf("secret kept: %q", field)
		}
	}
}

func TestAuditKeepsDigestsAndLongNames(t *testing.T) {
	schematic := strings.Repeat("376567988ad370138ad8b2698212367b", 2)
	name := "very-long-deployment-name-that-goes-past-forty-characters"

	for _, s := range []string{"image=factory.talos.dev/installer/" + schematic + ":v1.11.2", "Deployment/" + name} {
		if got := redactSecrets(s); got != s {
			t.Errorf("redactSecrets(%q) = %q", s, got)
		}
	}
}

func TestAuditExport(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))

	recordOutcome("", "prod", auditAction{Action: "scale", Namespace: "web", Object: "Deployment/api", Params: "replicas=3"}, nil)
	recordOutcome("", "lab", auditAction{Action: "reboot", Node: "10.0.0.2"}, errors.New("unreachable"))

	js, err := AuditExport("prod", "json")
	if err != nil {
		t.Fatal(err)
	}

	var entries []auditEntry
	if err := json.Unmarshal([]byte(js), &entries); err != nil || len(entries) != 1 || entries[0].Cluster != "prod" {
		t.Errorf("json export = %s (%v)", js, err)
	}

	md, err := AuditExport("", "markdown")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"| Time", "2026-10-01 12:00:00Z", "web/Deployment/api", "replicas=3", "10.0.0.2", "failed: unreachable"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown export lacks %q:\n%s", want, md)
		}
	}

	if _, err := AuditExport("", "csv"); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestAuditMarkdownEscapesPipes(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	recordOutcome("", "prod", auditAction{Action: "edit"}, errors.New("a | b\nc"))

	md, err := AuditExport("", "markdown")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(md, `a \| b c`) {
		t.Errorf("cell not escaped:\n%s", md)
	}
}

func TestAuditClear(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	recordOutcome("", "prod", auditAction{Action: "reboot"}, nil)
	recordOutcome("", "lab", auditAction{Action: "reboot"}, nil)

	if err := AuditClear("prod"); err != nil {
		t.Fatal(err)
	}

	if got := readAudit(t, "", ""); len(got) != 1 || got[0].Cluster != "lab" {
		t.Errorf("after clearing prod: %+v", got)
	}

	if err := AuditClear(""); err != nil {
		t.Fatal(err)
	}

	if got := readAudit(t, "", ""); len(got) != 0 {
		t.Errorf("after clearing all: %+v", got)
	}
}

// A background run that panics is recorded as a failure, then still reported to its listener.
func TestRecordedRunRecordsAPanicThenRaisesIt(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	var reported string

	func() {
		defer onPanic(func(msg string) { reported = msg })

		_ = recordedRun("", "prod", func() auditAction { return auditAction{Action: "drain", Node: "w1"} }, func() error {
			panic("boom")
		})
	}()

	if reported != "internal error: boom" {
		t.Errorf("listener got %q", reported)
	}

	got := readAudit(t, "prod", "drain")
	if len(got) != 1 || got[0].Outcome != auditFailed || got[0].Error != "internal error: boom" || got[0].Node != "w1" {
		t.Errorf("entries = %+v", got)
	}
}

// The action is built once the work ended, so it can say how it ended.
func TestRecordedRunRecordsTheOutcome(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	outcome := ""
	err := recordedRun("", "prod", func() auditAction { return auditAction{Action: "config-try", Params: "outcome=" + outcome} }, func() error {
		outcome = "rolled-back"

		return errors.New("not healthy")
	})

	if err == nil || err.Error() != "not healthy" {
		t.Fatalf("err = %v", err)
	}

	got := readAudit(t, "prod", "config-try")
	if len(got) != 1 || got[0].Outcome != auditFailed || got[0].Params != "outcome=rolled-back" {
		t.Errorf("entries = %+v", got)
	}
}
