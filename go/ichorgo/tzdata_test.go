package ichorgo

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// TestAlertRulesZonesWithoutZoneinfo: the quiet hours' zone loads with ZONEINFO pointed at an
// empty directory, as on iOS where Go finds no zoneinfo. Run in a child process, since the
// time package reads ZONEINFO once. Linux still has its system zoneinfo behind it, so the
// embedded database is also checked where it is imported.
func TestAlertRulesZonesWithoutZoneinfo(t *testing.T) {
	if os.Getenv("ICHOR_TZDATA_CHILD") == "1" {
		if _, err := time.LoadLocation("Europe/Paris"); err != nil {
			t.Fatal(err)
		}

		rules := `{"default":{"quietHours":{"start":"23:00","end":"07:00","tz":"America/New_York"}}}`
		if _, err := AlertRulesApply(rules, `[{"cluster":"fp","key":"am:x","kind":"am","problem":true}]`, 0); err != nil {
			t.Fatal(err)
		}

		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestAlertRulesZonesWithoutZoneinfo$")
	cmd.Env = append(os.Environ(), "ICHOR_TZDATA_CHILD=1", "ZONEINFO="+t.TempDir())

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("without zoneinfo: %v\n%s", err, out)
	}

	file, err := parser.ParseFile(token.NewFileSet(), "tzdata.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}

	for _, spec := range file.Imports {
		if path, _ := strconv.Unquote(spec.Path.Value); path == "time/tzdata" {
			return
		}
	}

	t.Fatal("tzdata.go does not import time/tzdata: iOS would load no time zone")
}
