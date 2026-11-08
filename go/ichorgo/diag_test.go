package ichorgo

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Invented names: none of them is a real cluster, nor one of the mask's own fakes.
const (
	diagTestContext = "otter@marsh-ridge"
	diagTestHost    = "marsh-ctl7"
	diagTestAddr    = "192.0.2.77"
	diagTestToken   = "eyJhbGciOiJSUzI1NiIsImtpZCI6IlFhYkxtNk9wUXJTdFV2V3hZejAxMjM0NTY3ODkifQ"
	diagTestConfig  = "context: " + diagTestContext + "\ncontexts:\n  " + diagTestContext +
		":\n    endpoints: [" + diagTestAddr + "]\n"
)

// withDiagRing empties the diagnostics ring for one test, persistence off, and again after.
func withDiagRing(t *testing.T) {
	t.Helper()

	reset := func() {
		diagLog.mu.Lock()
		diagLog.lines, diagLog.bytes, diagLog.persistState = nil, 0, persistState{}
		diagLog.mu.Unlock()
	}

	reset()
	t.Cleanup(reset)
}

func diagEntriesOf(t *testing.T, limit int) []diagEntry {
	t.Helper()

	out, err := DiagEntries(limit)
	if err != nil {
		t.Fatal(err)
	}

	var entries []diagEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("DiagEntries is not a JSON array of entries: %v\n%s", err, out)
	}

	return entries
}

func findDiag(entries []diagEntry, msg string) (diagEntry, bool) {
	for _, e := range entries {
		if e.Msg == msg {
			return e, true
		}
	}

	return diagEntry{}, false
}

func TestDiagRingBounds(t *testing.T) {
	cases := []struct {
		name      string
		n         int
		value     string
		wantCount func(int) bool
	}{
		{"entries", diagMaxEntries + 150, "", func(n int) bool { return n == diagMaxEntries }},
		{"bytes", 4000, strings.Repeat("x", diagMaxValue), func(n int) bool { return n < diagMaxEntries }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDiagRing(t)

			for i := range tc.n {
				diag(slog.LevelInfo, "api", "bound", nil, "i", i, "pad", tc.value)
			}

			entries := diagEntriesOf(t, 0)
			if !tc.wantCount(len(entries)) {
				t.Fatalf("%d entries kept", len(entries))
			}

			if got := entries[0].Attrs["i"]; got != float64(tc.n-1) {
				t.Errorf("newest entry i = %v, want %d (newest first)", got, tc.n-1)
			}

			if diagLog.bytes > diagMaxBytes {
				t.Errorf("ring holds %d bytes, over %d", diagLog.bytes, diagMaxBytes)
			}

			if got := len(diagEntriesOf(t, 5)); got != 5 {
				t.Errorf("DiagEntries(5) = %d entries", got)
			}
		})
	}
}

func TestDiagRedactsWithScreenshotModeOff(t *testing.T) {
	withDiagRing(t)
	SetPrivacyMask(false, "")

	// Learnt the way the core learns them, with screenshot mode off.
	privacy.learnConfig(diagTestConfig)
	privacy.learnHost(diagTestHost, "controlplane")

	slog.Error("dial failed", "err", errors.New("dial "+diagTestHost+" ("+diagTestAddr+"): Authorization: Bearer "+diagTestToken),
		"peer", diagTestHost, "status", 503, "took", 1500*time.Millisecond)
	diag(slog.LevelWarn, "kube", "context missing", errors.New("context "+diagTestContext+` not found in namespaces "pecan-team"`))
	slog.Debug("below info is not kept")

	out, err := DiagEntries(0)
	if err != nil {
		t.Fatal(err)
	}

	for _, leak := range []string{diagTestHost, diagTestAddr, diagTestToken, "marsh-ridge", "pecan-team", "below info"} {
		if strings.Contains(out, leak) {
			t.Errorf("entries hold %q:\n%s", leak, out)
		}
	}

	entries := diagEntriesOf(t, 0)

	e, ok := findDiag(entries, "dial failed")
	if !ok {
		t.Fatalf("no slog.Error entry in %s", out)
	}

	if e.Level != "error" || !strings.Contains(e.Err, "cp-") || !strings.Contains(e.Err, "<redacted>") {
		t.Errorf("entry = %+v, want level error and the mask's fake and <redacted> in err", e)
	}

	if e.Attrs["status"] != float64(503) || e.Attrs["took"] != float64(1500) || !strings.HasPrefix(e.Attrs["peer"].(string), "cp-") {
		t.Errorf("attrs = %v, want status 503, took 1500 (ms) and a fake peer", e.Attrs)
	}
}

func TestDiagHooks(t *testing.T) {
	withDiagRing(t)

	traceCounts("diag.test_step", time.Now(), 3, 1)
	_ = panicError("token=" + diagTestToken)
	_, exportErr := AuditExport("", "xml")

	if exportErr == nil {
		t.Fatal("AuditExport accepted an unknown format")
	}

	entries := diagEntriesOf(t, 0)

	cases := []struct {
		msg, level, component string
		check                 func(diagEntry) bool
	}{
		{"startup step", "info", "trace", func(e diagEntry) bool {
			return e.Attrs["step"] == "diag.test_step" && e.Attrs["count"] == float64(3) && e.Attrs["unreachable"] == float64(1)
		}},
		{"panic recovered", "error", "api", func(e diagEntry) bool {
			return strings.Contains(e.Err, "<redacted>") && !strings.Contains(e.Err, diagTestToken)
		}},
		{"call failed", "warn", "api", func(e diagEntry) bool {
			return e.Attrs["fn"] == "AuditExport" && strings.Contains(e.Err, "unknown export format")
		}},
	}

	for _, tc := range cases {
		e, ok := findDiag(entries, tc.msg)
		if !ok || e.Level != tc.level || e.Component != tc.component || !tc.check(e) {
			t.Errorf("%q entry = %+v (found %v)", tc.msg, e, ok)
		}
	}
}

func TestDiagLog(t *testing.T) {
	cases := []struct {
		name, level, component, attrs string
		wantErr                       bool
		want                          map[string]any
	}{
		{"flat attrs", "info", "android", `{"ms":42,"cold":true,"kind":"Pod"}`, false, map[string]any{"ms": float64(42), "cold": true, "kind": "Pod"}},
		{"no attrs", "warn", "ios", "", false, nil},
		{"empty object", "debug", "store", "{}", false, nil},
		{"not JSON", "error", "net", "{ms:", false, map[string]any{"bad_attrs": true}},
		{"nested", "error", "net", `{"a":{"b":1}}`, false, map[string]any{"bad_attrs": true}},
		{"unknown level", "fatal", "api", "", true, nil},
		{"unknown component", "info", "cluster", "", true, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDiagRing(t)

			err := DiagLog(tc.level, tc.component, "app event", tc.attrs)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}

			e, ok := findDiag(diagEntriesOf(t, 0), "app event")
			if tc.wantErr {
				if ok {
					t.Error("a refused entry was recorded")
				}

				return
			}

			if !ok || e.Level != tc.level || e.Component != tc.component {
				t.Fatalf("entry = %+v (found %v)", e, ok)
			}

			if b, w := mustJSON(t, e.Attrs), mustJSON(t, tc.want); b != w {
				t.Errorf("attrs = %s, want %s", b, w)
			}
		})
	}
}

func TestDiagClear(t *testing.T) {
	withDiagRing(t)
	dir := withDataDir(t)

	if err := SetDiagPersist(true); err != nil {
		t.Fatal(err)
	}

	diag(slog.LevelInfo, "api", "before clear", nil)

	if err := DiagClear(); err != nil {
		t.Fatal(err)
	}

	if n := len(diagEntriesOf(t, 0)); n != 0 {
		t.Errorf("%d entries after DiagClear", n)
	}

	if _, err := os.Stat(filepath.Join(dir, diagFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the persisted log survived DiagClear: %v", err)
	}
}

func TestDiagPersistRotatesAndReseeds(t *testing.T) {
	withDiagRing(t)
	dir := withDataDir(t)

	if err := SetDiagPersist(true); err != nil {
		t.Fatal(err)
	}

	pad := strings.Repeat("y", diagMaxValue)
	for i := range 3000 {
		diag(slog.LevelInfo, "store", "persisted", nil, "i", i, "pad", pad)
	}

	for _, name := range []string{diagFile, diagOldFile} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Size() > diagMaxBytes {
			t.Fatalf("%s: %v (size within %d expected)", name, err, diagMaxBytes)
		}
	}

	// A new process: the ring is empty until the data directory is set again.
	diagLog.mu.Lock()
	diagLog.lines, diagLog.bytes, diagLog.dir = nil, 0, ""
	diagLog.mu.Unlock()

	if err := SetDataDir(dir, testDataKey); err != nil {
		t.Fatal(err)
	}

	entries := diagEntriesOf(t, 0)
	if len(entries) == 0 || entries[0].Attrs["i"] != float64(2999) {
		t.Fatalf("reseeded ring: %d entries, newest %v; want the last line written", len(entries), entries)
	}

	if err := SetDiagPersist(false); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{diagFile, diagOldFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s kept after SetDiagPersist(false): %v", name, err)
		}
	}
}

func TestDiagPersistWithoutDataDir(t *testing.T) {
	withDiagRing(t)
	SetDataDir("", nil)

	if err := SetDiagPersist(true); err != nil {
		t.Fatalf("SetDiagPersist without a data dir: %v", err)
	}

	diag(slog.LevelInfo, "api", "memory only", nil)

	if _, ok := findDiag(diagEntriesOf(t, 0), "memory only"); !ok {
		t.Error("entry missing without a data dir")
	}
}

// TestDiagConcurrentWrites runs writers and readers together (run with -race).
func TestDiagConcurrentWrites(t *testing.T) {
	withDiagRing(t)
	withDataDir(t)

	if err := SetDiagPersist(true); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	for w := range 8 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			for i := range 200 {
				slog.Info("concurrent", "w", w, "i", i)
			}
		}()

		go func() {
			defer wg.Done()

			for range 20 {
				_, _ = DiagEntries(50)
			}
		}()
	}

	wg.Wait()

	n := 0
	for _, e := range diagEntriesOf(t, 0) {
		if e.Msg == "concurrent" {
			n++
		}
	}

	if n != 1600 {
		t.Errorf("%d entries, want 1600", n)
	}
}
