package ichorgo

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type runbookList struct {
	Items []runbookItem `json:"items"`
}

func listRunbooks(t *testing.T, scope string) []runbookItem {
	t.Helper()

	out, err := RunbookStoreList(scope)
	if err != nil {
		t.Fatal(err)
	}

	var l runbookList
	if err := json.Unmarshal([]byte(out), &l); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return l.Items
}

func TestRunbookStoreKeepsEachScopeApartAndSealed(t *testing.T) {
	dir := withDataDir(t)
	runbookStoreNow = func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { runbookStoreNow = time.Now })

	src := readRunbookFixture(t, "scale-and-restart.yaml")

	out, err := RunbookStoreImport("fp-a", src)
	if err != nil {
		t.Fatal(err)
	}

	var item runbookItem
	if err := json.Unmarshal([]byte(out), &item); err != nil || item.ID != "restart-ingestion" || item.Steps != 2 || item.Destructive != 2 {
		t.Fatalf("import = %s (%v)", out, err)
	}

	if _, err := RunbookStoreImport("fp-a", readRunbookFixture(t, "three-steps.yaml")); err != nil {
		t.Fatal(err)
	}

	got := listRunbooks(t, "fp-a")
	if len(got) != 2 || got[0].Name != "Restart ingestion" || got[1].Name != "Three steps" {
		t.Errorf("list = %+v", got)
	}

	if other := listRunbooks(t, "fp-a.masked"); len(other) != 0 {
		t.Errorf("another scope sees %+v", other)
	}

	// The same name replaces; the file never holds the YAML in clear.
	if _, err := RunbookStoreImport("fp-a", strings.Replace(src, "replica, then", "replica and then", 1)); err != nil {
		t.Fatal(err)
	}

	if got := listRunbooks(t, "fp-a"); len(got) != 2 {
		t.Errorf("after re-import: %+v", got)
	}

	files, _ := filepath.Glob(filepath.Join(dir, runbookStoreDir, "*"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if bytes.Contains(data, []byte("ingest-api")) {
			t.Errorf("%s holds the runbook in clear", f)
		}
	}

	yaml, err := RunbookStoreGet("fp-a", "restart-ingestion")
	if err != nil || !strings.Contains(yaml, "replica and then") {
		t.Errorf("get = %q, %v", yaml, err)
	}

	if err := RunbookStoreRemove("fp-a", "restart-ingestion"); err != nil {
		t.Fatal(err)
	}

	if got := listRunbooks(t, "fp-a"); len(got) != 1 || got[0].ID != "three-steps" {
		t.Errorf("after remove: %+v", got)
	}

	if _, err := RunbookStoreGet("fp-a", "restart-ingestion"); err == nil {
		t.Error("a removed runbook was read")
	}
}

func TestRunbookStoreRefusals(t *testing.T) {
	if _, err := RunbookStoreList("fp"); err == nil || !strings.Contains(err.Error(), "no storage") {
		t.Errorf("without a data directory: %v", err)
	}

	withDataDir(t)

	if _, err := RunbookStoreImport("fp", "schemaVersion: nope"); err == nil {
		t.Error("an invalid runbook was stored")
	}

	if err := RunbookStoreRemove("fp", "nothing"); err != nil {
		t.Errorf("removing nothing: %v", err)
	}
}

func TestRunbookID(t *testing.T) {
	cases := map[string]string{
		"Restart ingestion":   "restart-ingestion",
		"  Reboot worker #1 ": "reboot-worker-1",
		"Été — nœud":          "t-n-ud",
	}

	for name, want := range cases {
		if got := runbookID(name); got != want {
			t.Errorf("runbookID(%q) = %q, want %q", name, got, want)
		}
	}

	if got := runbookID("———"); !strings.HasPrefix(got, "runbook-") {
		t.Errorf("no letters: %q", got)
	}
}
