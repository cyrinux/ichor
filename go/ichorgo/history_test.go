package ichorgo

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	hMin  = int64(60_000)
	hHour = 60 * hMin
	hDay  = 24 * hHour
	hT0   = int64(1_780_000_000_000) // a fixed instant, well after 1970
)

// hRec builds a record JSON: nodes as "addr=health" (hostname derived), alerts as keys.
func hRec(at int64, nodes []string, alerts ...string) string {
	rec := map[string]any{"at": at}

	var ns []map[string]any

	for _, n := range nodes {
		addr, health, _ := strings.Cut(n, "=")
		ns = append(ns, map[string]any{"node": addr, "hostname": "host-" + addr, "health": health})
	}

	rec["nodes"] = ns

	var as []map[string]any
	for _, k := range alerts {
		as = append(as, map[string]any{"key": k, "track": "data", "severity": "warning", "title": "t " + k})
	}

	rec["alerts"] = as

	b, _ := json.Marshal(rec)

	return string(b)
}

func hAppend(t *testing.T, ring []byte, rec string, now int64) []byte {
	t.Helper()

	out, err := HistoryAppend(ring, rec, now)
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	return out
}

func hDecode(t *testing.T, ring []byte) historyRing {
	t.Helper()

	r, err := decodeHistoryRing(ring)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	return r
}

func hRaw(version byte, payload string) []byte {
	var buf bytes.Buffer

	buf.WriteString(historyMagic)
	buf.WriteByte(version)

	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(payload))
	_ = zw.Close()

	return buf.Bytes()
}

func TestHistoryAppendRejectsBadRecords(t *testing.T) {
	tests := []struct {
		name, rec, want string
	}{
		{"not json", "{", "history record"},
		{"no time", `{"nodes":[]}`, "missing"},
		{"future", fmt.Sprintf(`{"at":%d}`, hT0+2*hDay), "future"},
		{"bad health", fmt.Sprintf(`{"at":%d,"nodes":[{"node":"a","health":"fine"}]}`, hT0), "health"},
		{"no node", fmt.Sprintf(`{"at":%d,"nodes":[{"health":"ready"}]}`, hT0), "no \"node\""},
		{"too many", fmt.Sprintf(`{"at":%d,"alerts":[%s{"key":"x"}]}`, hT0, strings.Repeat(`{"key":"x"},`, historyMaxAlerts)), "too many"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HistoryAppend(nil, tt.rec, hT0)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}

func TestHistoryAppendCleansRecord(t *testing.T) {
	rec := fmt.Sprintf(`{"at":%d,"reachable":true,
		"nodes":[{"node":"b","health":"ready","memUsedPercent":42.06},{"node":"a","health":"notReady"},{"node":"a","health":"ready"}],
		"volumes":[{"key":"a|STATE","name":"STATE","node":"a","usedPercent":120},{"key":""},{"key":"a|EPHEMERAL","node":"a","usedPercent":-3}],
		"alerts":[{"key":"z","title":"%s"},{"key":""},{"key":"y"}]}`, hT0, strings.Repeat("é", 200))

	r := hDecode(t, hAppend(t, nil, rec, hT0))
	got := r.records[0]

	if got.Reachable != nil {
		t.Errorf("reachable true should be left out")
	}

	if len(got.Nodes) != 2 || got.Nodes[0].Node != "a" || *got.Nodes[1].MemUsedPercent != 42.1 {
		t.Errorf("nodes = %+v", got.Nodes)
	}

	if len(got.Volumes) != 2 || got.Volumes[0].UsedPercent != 0 || got.Volumes[1].UsedPercent != 100 {
		t.Errorf("volumes = %+v", got.Volumes)
	}

	if len(got.Alerts) != 2 || got.Alerts[0].Key != "y" || len(got.Alerts[1].Title) > historyMaxTitle+len("…") {
		t.Errorf("alerts = %+v", got.Alerts)
	}
}

func TestHistoryAppendOrderPruneReplace(t *testing.T) {
	now := hT0 + 40*hDay

	var ring []byte

	for _, at := range []int64{hT0, now - 10*hMin, now - 20*hMin, now - 31*hDay} {
		ring = hAppend(t, ring, hRec(at, []string{"a=ready"}), now)
	}

	// Same time again: replaced, not added.
	ring = hAppend(t, ring, hRec(now-10*hMin, []string{"a=notReady"}), now)

	r := hDecode(t, ring)
	if len(r.records) != 2 {
		t.Fatalf("records = %d, want 2 (older than 30 days pruned)", len(r.records))
	}

	if r.records[0].At != now-20*hMin || r.records[1].Nodes[0].Health != historyNotReady {
		t.Fatalf("records = %+v", r.records)
	}
}

func TestHistoryThinning(t *testing.T) {
	now := hT0 + 20*hDay

	var recs []historyRecord

	// Every 15 minutes for 10 days: older than 7 days thinned to one per hour, except
	// where the state changes.
	for at := now - 10*hDay; at <= now; at += 15 * hMin {
		health := historyReady
		if at == now-9*hDay+30*hMin {
			health = historyNotReady
		}

		recs = append(recs, historyRecord{At: at, Nodes: []historyNode{{Node: "a", Health: health}}})
	}

	out := thinHistory(recs, now)

	old, recent := 0, 0

	for _, r := range out {
		if r.At < now-7*hDay {
			old++
		} else {
			recent++
		}
	}

	if old != 3*24+2 { // 72 hours + the change and the return to ready
		t.Errorf("old records = %d, want %d", old, 3*24+2)
	}

	if recent != 7*24*4+1 {
		t.Errorf("recent records = %d, want %d", recent, 7*24*4+1)
	}

	if again := thinHistory(out, now); len(again) != len(out) {
		t.Errorf("thinning twice changed the ring: %d then %d", len(out), len(again))
	}
}

func TestHistorySizeCap(t *testing.T) {
	var ring historyRing

	for i := range 400 {
		ring.records = append(ring.records, historyRecord{
			At:     hT0 + int64(i)*hMin,
			Alerts: []historyAlert{{Key: fmt.Sprintf("%x", i*7919*104729), Title: strings.Repeat(fmt.Sprintf("%d", i*31337), 10)}},
		})
	}

	b, err := encodeHistoryCapped(ring, 4096)
	if err != nil {
		t.Fatal(err)
	}

	r := hDecode(t, b)

	if len(b) > 4096 || len(r.records) >= 400 || r.records[len(r.records)-1].At != hT0+399*hMin {
		t.Fatalf("size %d with %d records, newest %d", len(b), len(r.records), r.records[len(r.records)-1].At)
	}

	// A single record larger than the cap stays.
	one, _ := encodeHistoryCapped(historyRing{records: ring.records[:1]}, 10)
	if len(hDecode(t, one).records) != 1 {
		t.Fatal("the newest record must stay")
	}
}

func TestHistoryVersionAndCorruption(t *testing.T) {
	rec := hRec(hT0, []string{"a=ready"})

	tests := []struct {
		name      string
		ring      []byte
		wantErr   error // from HistoryAppend
		wantReset string
	}{
		{"newer version", hRaw(historyVersion+1, "{}\n"), errHistoryNewer, ""},
		{"garbage", []byte("hello world"), nil, "not a history ring"},
		{"short", []byte("ICHH"), nil, "not a history ring"},
		{"version zero", hRaw(0, "{}\n"), nil, "unknown format"},
		{"not gzip", append([]byte(historyMagic+"\x01"), "plain"...), nil, "cannot be read"},
		{"bad meta", hRaw(historyVersion, "[\n"), nil, "meta"},
		{"bad record", hRaw(historyVersion, "{}\n{\"at\":\n"), nil, "record 1"},
		{"empty stream", hRaw(historyVersion, ""), nil, "empty"},
		{"long line", hRaw(historyVersion, "{}\n"+strings.Repeat("x", historyMaxLine+1)), nil, "too long"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := HistoryAppend(tt.ring, rec, hT0)
			if tt.wantErr != nil {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}

				if _, qerr := HistoryQuery(tt.ring, 0, hT0); qerr == nil {
					t.Fatal("query of a newer ring should fail")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			q := hQuery(t, out, hT0-hHour, hT0)
			if q.ResetAt != hT0 || !strings.Contains(q.ResetReason, tt.wantReset) || q.Records != 1 {
				t.Fatalf("reset = %d %q, records %d", q.ResetAt, q.ResetReason, q.Records)
			}

			if _, err := HistoryQuery(tt.ring, 0, hT0); err == nil {
				t.Fatal("query of a corrupt ring should fail")
			}
		})
	}
}

func TestHistoryInflateLimit(t *testing.T) {
	big := "{}\n" + strings.Repeat(fmt.Sprintf(`{"at":%d}`+"\n", hT0), historyMaxRaw/20+10)
	if _, err := decodeHistoryRing(hRaw(historyVersion, big)); !errors.Is(err, errHistoryCorrupt) {
		t.Fatalf("err = %v", err)
	}
}

func hQuery(t *testing.T, ring []byte, since, now int64) historyQueryResult {
	t.Helper()

	s, err := HistoryQuery(ring, since, now)
	if err != nil {
		t.Fatal(err)
	}

	var q historyQueryResult
	if err := json.Unmarshal([]byte(s), &q); err != nil {
		t.Fatalf("%v: %s", err, s)
	}

	return q
}

func TestHistoryQueryIntervalsAndGaps(t *testing.T) {
	var ring []byte

	add := func(at int64, rec string) { ring = hAppend(t, ring, rec, at) }

	// a: ready 0-30m, notReady 30-60m, ready 60m-1h30, then the phone is off 1h30-5h,
	// unreachable cluster at 5h-5h15, ready again from 5h15 until 6h.
	add(hT0, hRec(hT0, []string{"a=ready", "b=ready"}))
	add(hT0+15*hMin, hRec(hT0+15*hMin, []string{"a=ready", "b=ready"}))
	add(hT0+30*hMin, hRec(hT0+30*hMin, []string{"a=notReady", "b=ready"}))
	add(hT0+60*hMin, hRec(hT0+60*hMin, []string{"a=ready", "b=ready"}))
	add(hT0+90*hMin, hRec(hT0+90*hMin, []string{"a=ready"}))
	add(hT0+5*hHour, fmt.Sprintf(`{"at":%d,"reachable":false,"nodes":[{"node":"a","health":"unreachable"}]}`, hT0+5*hHour))
	add(hT0+5*hHour+15*hMin, hRec(hT0+5*hHour+15*hMin, []string{"a=ready"}))

	q := hQuery(t, ring, hT0-hHour, hT0+6*hHour)

	if q.Records != 7 || len(q.Nodes) != 2 {
		t.Fatalf("records %d nodes %d", q.Records, len(q.Nodes))
	}

	wantGaps := []historySpan{{hT0 - hHour, hT0}, {hT0 + 90*hMin, hT0 + 5*hHour + 15*hMin}}
	if fmt.Sprint(q.Gaps) != fmt.Sprint(wantGaps) {
		t.Errorf("gaps = %v, want %v", q.Gaps, wantGaps)
	}

	a := q.Nodes[0]
	wantA := []historyInterval{
		{historyReady, hT0, hT0 + 30*hMin},
		{historyNotReady, hT0 + 30*hMin, hT0 + 60*hMin},
		{historyReady, hT0 + 60*hMin, hT0 + 90*hMin},
		{historyReady, hT0 + 5*hHour + 15*hMin, hT0 + 6*hHour},
	}

	if a.Node != "a" || a.Hostname != "host-a" || fmt.Sprint(a.Intervals) != fmt.Sprint(wantA) {
		t.Errorf("a = %+v", a)
	}

	// 105 of 135 observed minutes ready.
	if a.UptimePercent == nil || *a.UptimePercent != 77.78 {
		t.Errorf("uptime = %v", a.UptimePercent)
	}

	// b vanished after 90m: observed 0-90m, always ready.
	if b := q.Nodes[1]; *b.UptimePercent != 100 || b.Intervals[0].To != hT0+90*hMin {
		t.Errorf("b = %+v", b)
	}

	// A window inside the first record's span, and one with no record at all.
	q = hQuery(t, ring, hT0+5*hMin, hT0+10*hMin)
	if q.Records != 0 || len(q.Gaps) != 0 || q.Nodes[0].Intervals[0].From != hT0+5*hMin {
		t.Errorf("inner window = %+v", q)
	}

	if q = hQuery(t, nil, hT0, hT0+hHour); len(q.Gaps) != 1 || len(q.Nodes) != 0 {
		t.Errorf("empty ring = %+v", q)
	}

	if _, err := HistoryQuery(ring, hT0, hT0-1); err == nil {
		t.Error("an inverted window should fail")
	}
}

func TestHistoryGapAfterFollowsSpacing(t *testing.T) {
	var recs []historyRecord
	for i := range 5 {
		recs = append(recs, historyRecord{At: hT0 + int64(i)*3*hHour})
	}

	if got := historyGapAfter(recs); got != 9*hHour {
		t.Fatalf("gapAfter = %d, want 9h", got)
	}

	if got := historyGapAfter(recs[:1]); got != 2*hHour {
		t.Fatalf("gapAfter = %d, want 2h", got)
	}
}

func TestHistoryAlertPairing(t *testing.T) {
	var ring []byte

	steps := []struct {
		at        int64
		reachable bool
		alerts    []string
	}{
		{0, true, []string{"x"}},
		{15, true, []string{"x", "y"}},
		{30, false, nil}, // says nothing: x and y stay open
		{45, true, []string{"y"}},
		{60, true, nil},
		{75, true, []string{"x"}}, // x again: a new span
	}

	for _, s := range steps {
		rec := hRec(hT0+s.at*hMin, []string{"a=ready"}, s.alerts...)
		if !s.reachable {
			rec = fmt.Sprintf(`{"at":%d,"reachable":false}`, hT0+s.at*hMin)
		}

		ring = hAppend(t, ring, rec, hT0+s.at*hMin)
	}

	q := hQuery(t, ring, hT0, hT0+90*hMin)

	got := make([]string, 0, len(q.Alerts))
	for _, a := range q.Alerts {
		closed := "open"
		if a.ClosedAt != nil {
			closed = fmt.Sprint((*a.ClosedAt - hT0) / hMin)
		}

		got = append(got, fmt.Sprintf("%s:%d-%s", a.Key, (a.OpenedAt-hT0)/hMin, closed))
	}

	if want := "[x:0-45 y:15-60 x:75-open]"; fmt.Sprint(got) != want {
		t.Fatalf("alerts = %v, want %s", got, want)
	}

	// A window after the first spans closed only keeps the one still open.
	if q = hQuery(t, ring, hT0+70*hMin, hT0+90*hMin); len(q.Alerts) != 1 || q.Alerts[0].Title != "t x" {
		t.Fatalf("late window alerts = %+v", q.Alerts)
	}
}

func TestHistorySeriesAndDownsampling(t *testing.T) {
	var ring []byte

	now := hT0 + 6*hDay

	for i := int64(0); i < 6*24*4; i++ {
		at := hT0 + i*15*hMin
		pct := float64(i%50) + 0.5
		rec := fmt.Sprintf(`{"at":%d,"nodes":[{"node":"a","health":"ready","memUsedPercent":%v}],
			"volumes":[{"key":"a|EPHEMERAL","name":"EPHEMERAL","node":"a","usedPercent":%v}]}`, at, pct, pct)
		ring = hAppend(t, ring, rec, now)
	}

	q := hQuery(t, ring, hT0, now)

	if len(q.Volumes) != 1 || len(q.Memory) != 1 {
		t.Fatalf("series: %d volumes, %d memory", len(q.Volumes), len(q.Memory))
	}

	v := q.Volumes[0]
	if v.Key != "a|EPHEMERAL" || v.Name != "EPHEMERAL" || v.Node != "a" || len(v.Series) > historyMaxPoints || len(v.Series) < historyMaxPoints/2 {
		t.Fatalf("volume series: %+v (%d points)", v.Key, len(v.Series))
	}

	// Peaks survive: the max of each bucket is kept.
	peak := 0.0
	for _, p := range q.Memory[0].Series {
		peak = max(peak, p.V)
	}

	if peak != 49.5 {
		t.Fatalf("peak = %v", peak)
	}

	// Small series are untouched and marshal as [t, pct].
	short := downsampleHistory([]historyPoint{{hT0, 1.5}}, hT0, now, 10)

	b, _ := json.Marshal(short)
	if string(b) != fmt.Sprintf("[[%d,1.5]]", hT0) {
		t.Fatalf("json = %s", b)
	}
}

func (p *historyPoint) UnmarshalJSON(b []byte) error {
	var pair [2]float64
	if err := json.Unmarshal(b, &pair); err != nil {
		return err
	}

	p.T, p.V = int64(pair[0]), pair[1]

	return nil
}

func hSince(t *testing.T, ring []byte, lastLooked int64) historySinceResult {
	t.Helper()

	s, err := HistorySince(ring, lastLooked)
	if err != nil {
		t.Fatal(err)
	}

	var r historySinceResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatal(err)
	}

	return r
}

func TestHistorySince(t *testing.T) {
	var ring []byte

	add := func(m int64, nodes string, version string, alerts ...string) {
		rec := hRec(hT0+m*hMin, strings.Split(nodes, ","), alerts...)
		if version != "" {
			rec = strings.ReplaceAll(rec, `"health":"ready"`, `"health":"ready","version":"`+version+`"`)
		}

		ring = hAppend(t, ring, rec, hT0+m*hMin)
	}

	add(0, "a=ready,b=ready,c=notReady", "v1.10.0", "old")
	add(15, "a=notReady,b=ready,c=ready", "v1.10.0", "old")
	add(30, "a=unreachable,b=ready,c=ready", "v1.10.0")
	add(45, "a=ready,b=notReady,c=ready", "v1.11.0", "new")
	add(60, "a=ready,b=notReady,c=ready", "v1.11.0", "new")

	r := hSince(t, ring, hT0+20*hMin)

	if r.Records != 3 || r.To != hT0+60*hMin {
		t.Fatalf("records %d to %d", r.Records, r.To)
	}

	// a went down before lastLooked and came back after it (worst state unreachable);
	// c came back before lastLooked: not listed.
	if len(r.NodesRecovered) != 1 || r.NodesRecovered[0].Node != "a" || r.NodesRecovered[0].State != historyUnreachable ||
		r.NodesRecovered[0].DownAt != hT0+15*hMin || *r.NodesRecovered[0].UpAt != hT0+45*hMin {
		t.Errorf("recovered = %+v", r.NodesRecovered)
	}

	if len(r.NodesDown) != 1 || r.NodesDown[0].Node != "b" || r.NodesDown[0].DownAt != hT0+45*hMin || r.NodesDown[0].UpAt != nil {
		t.Errorf("down = %+v", r.NodesDown)
	}

	if len(r.AlertsResolved) != 1 || r.AlertsResolved[0].Key != "old" || len(r.AlertsOpen) != 1 || r.AlertsOpen[0].Key != "new" {
		t.Errorf("alerts = %+v / %+v", r.AlertsResolved, r.AlertsOpen)
	}

	// The ready nodes a and c were upgraded at 45 (b's version is not sent while not ready).
	if len(r.Upgrades) != 2 || r.Upgrades[0].From != "v1.10.0" || r.Upgrades[0].To != "v1.11.0" || r.Upgrades[0].At != hT0+45*hMin {
		t.Errorf("upgrades = %+v", r.Upgrades)
	}

	// Nothing after the last record: empty lists, not null.
	s, err := HistorySince(ring, hT0+2*hHour)
	if err != nil || !strings.Contains(s, `"nodesRecovered":[]`) || !strings.Contains(s, `"upgrades":[]`) {
		t.Errorf("quiet summary = %s (%v)", s, err)
	}

	if _, err := HistorySince([]byte("junk"), 0); err == nil {
		t.Error("a corrupt ring should fail")
	}
}

func TestHistoryExportAnonymises(t *testing.T) {
	rec := func(at int64) string {
		return fmt.Sprintf(`{"at":%d,"nodes":[{"node":"192.0.2.10","hostname":"kitchen-nuc","health":"ready","version":"v1.11.0"},
			{"node":"192.0.2.11","hostname":"attic-pi","health":"notReady"}],
			"volumes":[{"key":"192.0.2.10|EPHEMERAL","name":"EPHEMERAL","node":"192.0.2.10","usedPercent":40},
			{"key":"192.0.2.11|family-photos","name":"family-photos","node":"192.0.2.11","usedPercent":91}],
			"alerts":[{"key":"192.0.2.11|family-photos","track":"storage","severity":"critical","title":"family-photos is 91%% full"}]}`, at)
	}

	var ring []byte
	ring = hAppend(t, ring, rec(hT0), hT0)
	ring = hAppend(t, ring, rec(hT0+hMin), hT0+hMin)

	s, err := HistoryExport(ring)
	if err != nil {
		t.Fatal(err)
	}

	for _, secret := range []string{"192.0.2", "kitchen", "attic", "family", "91% full"} {
		if strings.Contains(s, secret) {
			t.Errorf("export leaks %q: %s", secret, s)
		}
	}

	var out historyExportResult
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatal(err)
	}

	r0, r1 := out.Records[0], out.Records[1]
	if out.Format != historyVersion || r0.At != hT0 || r0.Nodes[0].Node != "node-1" || r0.Nodes[0].Hostname != "node-1" ||
		r1.Nodes[1].Node != "node-2" || r0.Nodes[0].Version != "v1.11.0" {
		t.Errorf("nodes = %+v / %+v", r0.Nodes, r1.Nodes)
	}

	if r0.Volumes[0].Key != "node-1|EPHEMERAL" || r1.Volumes[1].Key != "node-2|volume-1" || r1.Volumes[1].UsedPercent != 91 {
		t.Errorf("volumes = %+v", r1.Volumes)
	}

	if r1.Alerts[0].Key != "alert-1" || r1.Alerts[0].Track != "storage" || r1.Alerts[0].Title != "" {
		t.Errorf("alerts = %+v", r1.Alerts)
	}

	if _, err := HistoryExport([]byte("junk")); err == nil {
		t.Error("a corrupt ring should fail")
	}
}

func TestHistoryRealisticSize(t *testing.T) {
	now := hT0 + 30*hDay

	// 8 nodes, 3 volumes each, every 15 minutes for 30 days.
	record := func(at int64) string {
		var nodes, vols []string

		for n := range 8 {
			nodes = append(nodes, fmt.Sprintf(`{"node":"10.0.0.%d","hostname":"worker-%d","health":"ready","version":"v1.11.0","memUsedPercent":%d}`, n, n, (at/hMin+int64(n))%90))

			for _, v := range []string{"EPHEMERAL", "STATE", "data"} {
				vols = append(vols, fmt.Sprintf(`{"key":"10.0.0.%d|%s","name":"%s","node":"10.0.0.%d","usedPercent":%d}`, n, v, v, n, (at/hHour)%100))
			}
		}

		return fmt.Sprintf(`{"at":%d,"nodes":[%s],"volumes":[%s]}`, at, strings.Join(nodes, ","), strings.Join(vols, ","))
	}

	var ring historyRing

	for at := hT0; at < now; at += 15 * hMin {
		rec, err := parseHistoryRecord(record(at), at)
		if err != nil {
			t.Fatal(err)
		}

		ring.records = append(ring.records, rec)
	}

	ring.records = thinHistory(ring.records, now)

	b, err := encodeHistoryRing(ring)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	b = hAppend(t, b, record(now), now)
	elapsed := time.Since(start)

	if len(b) > historyMaxBytes {
		t.Fatalf("ring is %d bytes", len(b))
	}

	r := hDecode(t, b)
	if r.records[0].At > hT0+hDay || len(r.records) < 7*24*4 {
		t.Fatalf("%d records from %v: the month should fit", len(r.records), time.UnixMilli(r.records[0].At))
	}

	t.Logf("%d records in %d bytes, append took %v", len(r.records), len(b), elapsed)
}
