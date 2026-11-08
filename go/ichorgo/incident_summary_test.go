package ichorgo

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// An invented cluster: no real name, address or account.
const (
	incContext = "ops@orchard-lab"
	incCP      = "192.0.2.11"
	incWorker  = "192.0.2.12"
	incCPHost  = "pear-cp1.orchard.example"
	incWHost   = "plum-w1.orchard.example"
)

// incRecord is one monitor run: health per node, a Talos version for the control plane.
func incRecord(at int64, cpHealth, workerHealth, cpVersion string, alerts ...map[string]any) string {
	rec := map[string]any{
		"at": at,
		"nodes": []map[string]any{
			{"node": incCP, "hostname": incCPHost, "health": cpHealth, "version": cpVersion},
			{"node": incWorker, "hostname": incWHost, "health": workerHealth, "version": "v1.9.0"},
		},
		"volumes": []map[string]any{
			{"key": incCP + "|family-photos", "name": "family-photos", "node": incCP, "usedPercent": 91},
			{"key": incCP + "|EPHEMERAL", "name": "EPHEMERAL", "node": incCP, "usedPercent": 40},
		},
		"alerts": alerts,
	}

	b, _ := json.Marshal(rec)

	return string(b)
}

var (
	incWorkerDown = map[string]any{"key": incWorker + "|node", "track": "node", "severity": "critical", "title": "plum-w1 is unreachable"}
	incVolumeFull = map[string]any{"key": incCP + "|family-photos", "track": "storage", "severity": "warning", "title": "family-photos is 91% full"}
)

// incidentFixture is a ring of an outage (the worker goes down, the control plane is
// upgraded, the phone stops polling for a while) and two actions taken during it.
func incidentFixture(t *testing.T) []byte {
	t.Helper()

	withDataDir(t)
	setNow := withAuditClock(t, time.UnixMilli(hT0+90*hMin))

	recordOutcome("", incContext, auditAction{Action: "reboot", Node: incWorker, Params: "mode=default token=s3cr3t-value"}, nil)
	setNow(time.UnixMilli(hT0 + 100*hMin))
	recordOutcome("", incContext, auditAction{Action: "reboot", Node: incWorker}, errors.New("dial tcp "+incWorker+":50000: "+incWHost+" refused"))
	recordOutcome("", "other-cluster", auditAction{Action: "scale", Object: "Deployment/api"}, nil)
	setNow(time.UnixMilli(hT0 + 12*hHour))

	var ring []byte

	for _, r := range []string{
		incRecord(hT0, historyReady, historyReady, "v1.9.0"),
		incRecord(hT0+hHour, historyReady, historyUnreachable, "v1.9.0", incWorkerDown, incVolumeFull),
		incRecord(hT0+2*hHour, historyReady, historyUnreachable, "v1.9.1", incWorkerDown, incVolumeFull),
		incRecord(hT0+8*hHour, historyReady, historyUnreachable, "v1.9.1", incWorkerDown),
	} {
		ring = hAppend(t, ring, r, hT0+8*hHour)
	}

	return ring
}

func TestIncidentSummary(t *testing.T) {
	sections := []string{"# Incident summary: ", "## What is wrong now", "## Timeline", "## Actions taken from the phone", "## Gaps", "Window: "}
	secrets := []string{"s3cr3t-value"}
	real := []string{incCP, incWorker, "192.0.2.", "pear-cp1", "plum-w1", "orchard", incContext, "ops@", "family-photos", "other-cluster"}

	cases := []struct {
		name      string
		anonymize bool
		want      []string
		absent    []string
	}{
		{
			name:      "anonymised",
			anonymize: true,
			want: []string{
				"# Incident summary: cluster", "node-2 is unreachable", "node-1: Talos v1.9.0 → v1.9.1",
				"**critical**: a node alert", "volume-1 is 91% full", "**failed**", "from this phone only",
				"| reboot |", "token=<redacted>",
			},
			absent: append(real, secrets...),
		},
		{
			name:      "real names",
			anonymize: false,
			want: []string{
				"# Incident summary: " + incContext, incWHost + " (" + incWorker + ") is unreachable",
				"plum-w1 is unreachable", "family-photos is 91% full", incWHost + " refused",
			},
			absent: append([]string{"other-cluster", "node-1"}, secrets...),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ring := incidentFixture(t)

			out, err := IncidentSummary(incContext, ring, hT0-hHour, hT0+12*hHour, tc.anonymize)
			if err != nil {
				t.Fatal(err)
			}

			for _, s := range append(sections, tc.want...) {
				if !strings.Contains(out, s) {
					t.Errorf("missing %q in:\n%s", s, out)
				}
			}

			lower := strings.ToLower(out)
			for _, s := range tc.absent {
				if strings.Contains(lower, strings.ToLower(s)) {
					t.Errorf("%q must not appear in:\n%s", s, out)
				}
			}
		})
	}
}

func TestIncidentSummaryWindowAndGaps(t *testing.T) {
	ring := incidentFixture(t)

	out, err := IncidentSummary(incContext, ring, hT0-hHour, hT0+12*hHour, true)
	if err != nil {
		t.Fatal(err)
	}

	// The worker going down is in the window, the phone's silence from 2h on a gap.
	for _, s := range []string{
		incidentTime(hT0+hHour) + " node-2: ready → unreachable",
		"- " + incidentTime(hT0+2*hHour) + " to ",
		"Window: " + incidentTime(hT0-hHour) + " to " + incidentTime(hT0+12*hHour),
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}

	// A window before the actions and the outage shows neither.
	early, err := IncidentSummary(incContext, ring, hT0-2*hHour, hT0+30*hMin, true)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(early, "| reboot |") || strings.Contains(early, "ready → unreachable") {
		t.Errorf("events outside the window:\n%s", early)
	}

	if _, err := IncidentSummary(incContext, ring, hT0, hT0-1, true); err == nil {
		t.Error("an end before the start must fail")
	}

	if _, err := IncidentSummary(" ", ring, hT0, hT0+1, true); err == nil {
		t.Error("no cluster must fail")
	}
}

func TestIncidentSummaryEmptyRing(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.UnixMilli(hT0))

	out, err := IncidentSummary(incContext, nil, hT0-hDay, hT0, true)
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range []string{"No recorded history", "## Timeline", "## Gaps", "No action was taken"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}

	if strings.Contains(out, "orchard") {
		t.Errorf("the context name shows:\n%s", out)
	}
}

func TestIncidentSummaryBounded(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.UnixMilli(hT0+hDay))

	// 600 runs a minute apart where 10 nodes flap: thousands of timeline rows.
	var ring historyRing

	for i := range 600 {
		rec := historyRecord{At: hT0 + int64(i)*hMin}

		for n := range 10 {
			health := historyReady
			if (i+n)%2 == 0 {
				health = historyNotReady
			}

			addr := "198.51.100." + string(rune('0'+n))
			rec.Nodes = append(rec.Nodes, historyNode{Node: addr, Hostname: "flap-" + string(rune('a'+n)), Health: health})
		}

		ring.records = append(ring.records, rec)
	}

	b, err := encodeHistoryCapped(ring, historyMaxBytes)
	if err != nil {
		t.Fatal(err)
	}

	for _, anonymize := range []bool{true, false} {
		out, err := IncidentSummary(incContext, b, hT0, hT0+hDay, anonymize)
		if err != nil {
			t.Fatal(err)
		}

		if len(out) > incidentMaxBytes {
			t.Errorf("anonymize=%v: %d bytes, want at most %d", anonymize, len(out), incidentMaxBytes)
		}

		if !strings.Contains(out, " earlier events") || !strings.Contains(out, "## Gaps") {
			t.Errorf("anonymize=%v: no dropped-rows line or a section cut", anonymize)
		}

		// The newest rows are kept: the last run's changes.
		if !strings.Contains(out, incidentTime(hT0+599*hMin)) {
			t.Errorf("anonymize=%v: the newest rows were dropped", anonymize)
		}
	}
}

func TestIncidentSummaryBoundCutsActionsLast(t *testing.T) {
	v := incidentView{cluster: "cluster", hasHistory: true}
	for i := range 3000 {
		v.actions = append(v.actions, auditEntry{At: int64(i) * hMin, Action: "scale", Outcome: auditOK, Params: strings.Repeat("x", 30)})
	}

	out := renderIncidentBounded(v, 4<<10)
	if len(out) > 4<<10 || !strings.Contains(out, " earlier actions") || !strings.Contains(out, "## Gaps") {
		t.Errorf("actions not bounded (%d bytes):\n%s", len(out), out)
	}
}

func TestIncidentDuration(t *testing.T) {
	for ms, want := range map[int64]string{30_000: "1m", 45 * hMin: "45m", 3*hHour + 20*hMin: "3h 20m", 2*hDay + 3*hHour: "2d 3h"} {
		if got := incidentDuration(ms); got != want {
			t.Errorf("incidentDuration(%d) = %q, want %q", ms, got, want)
		}
	}
}
