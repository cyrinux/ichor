package ichorgo

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func runLogTemplates(t *testing.T, lines any, limit int) logTemplatesResult {
	t.Helper()

	in, err := json.Marshal(lines)
	if err != nil {
		t.Fatal(err)
	}

	out, err := LogTemplates(string(in), limit)
	if err != nil {
		t.Fatalf("templates: %v", err)
	}

	var res logTemplatesResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}

	return res
}

func TestLogTemplatesMask(t *testing.T) {
	cases := []struct{ in, want string }{
		{"connection to 10.0.0.7:5432 refused after 30s", "connection to <*> refused after <*>"},
		{"request 3f2c1a9e-8b7d-4c6e-a1f0-0123456789ab took 12.3ms", "request <*> took <*>"},
		{"pod shop/web-7d9f8c6b5-x2k9q restarted 4 times", "pod shop/web-<*> restarted <*> times"},
		{`user "alice" logged in from [fd00::7]:8443`, `user "<*>" logged in from <*>`},
		{"GET /api/v1/orders/18324 200 in 2h1m3s", "GET /api/v1/orders/<*> <*> in <*>"},
		{"commit 9fceb02d0ae598e95dc970b74767f19372d61af8 pushed at 2026-11-14T10:02:03.123Z", "commit <*> pushed at <*>"},
		// Words survive, digits inside a word too, and hex needs a digit to be an id.
		{"http2 upstream k8s facade decade ready", "http2 upstream k8s facade decade ready"},
	}
	for _, c := range cases {
		if got := maskLogMessage(c.in); got != c.want {
			t.Errorf("mask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLogTemplatesGroupsAPodTail(t *testing.T) {
	var lines []string

	for i := 0; i < 6; i++ {
		lines = append(lines, fmt.Sprintf(`{"level":"error","msg":"connection to 10.0.0.%d:5432 refused after %ds"}`, i, 30+i))
	}

	for i := 0; i < 3; i++ {
		lines = append(lines, fmt.Sprintf(`level=info msg="served request" path=/orders/%d status=200 duration=%dms`, i, 5+i))
	}

	lines = append(lines, "OOMKilled worker-1", "OOMKilled worker-2", "plain line with no number")

	res := runLogTemplates(t, lines, 0)
	if res.Lines != 12 || res.Distinct != 4 {
		t.Fatalf("lines=%d distinct=%d, want 12 and 4: %+v", res.Lines, res.Distinct, res.Groups)
	}

	first := res.Groups[0]
	if first.Template != "connection to <*> refused after <*>" || first.Level != "error" || first.Count != 6 {
		t.Errorf("first group = %+v", first)
	}

	if first.Sample != "connection to 10.0.0.5:5432 refused after 35s" {
		t.Errorf("sample = %q, want the newest message, unmasked", first.Sample)
	}

	if first.Share != 0.5 || first.Pods != 0 {
		t.Errorf("share=%v pods=%d, want 0.5 and 0 for a single pod", first.Share, first.Pods)
	}

	logfmt := res.Groups[1]
	if logfmt.Count != 3 || !strings.HasPrefix(logfmt.Template, "served request") || !strings.Contains(logfmt.Template, "path=<*>") {
		t.Errorf("logfmt group = %+v", logfmt)
	}

	if oom := res.Groups[2]; oom.Count != 2 || oom.Template != "OOMKilled worker-<*>" {
		t.Errorf("plain group = %+v", oom)
	}

	if limited := runLogTemplates(t, lines, 2); len(limited.Groups) != 2 || limited.Distinct != 4 {
		t.Errorf("limit 2 = %+v", limited)
	}
}

func TestLogTemplatesWorkloadLines(t *testing.T) {
	at := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

	var lines []workloadLogLine

	for i := 0; i < 4; i++ {
		lines = append(lines, workloadLogLine{
			Pod:   fmt.Sprintf("web-7d9f8c6b5-%s", []string{"aaaaa", "bbbbb", "aaaaa", "ccccc"}[i]),
			Time:  at.Add(time.Duration(i) * time.Minute).UnixMilli(),
			Text:  fmt.Sprintf("timeout after %dms talking to db", 100*(i+1)),
			Level: "warn",
		})
	}

	res := runLogTemplates(t, lines, 0)
	if len(res.Groups) != 1 {
		t.Fatalf("groups = %+v", res.Groups)
	}

	g := res.Groups[0]
	if g.Pods != 3 || g.Count != 4 || g.FirstTs != at.UnixMilli() || g.LastTs != at.Add(3*time.Minute).UnixMilli() {
		t.Errorf("workload group = %+v", g)
	}

	if g.Sample != "timeout after 400ms talking to db" {
		t.Errorf("sample = %q", g.Sample)
	}
}

func TestLogTemplatesEdges(t *testing.T) {
	if res := runLogTemplates(t, []string{}, 0); res.Groups == nil || res.Lines != 0 {
		t.Errorf("empty = %+v", res)
	}

	// A message that would mask to nothing but placeholders keeps its raw text.
	if res := runLogTemplates(t, []string{"12345", "12345"}, 0); res.Groups[0].Template != "12345" {
		t.Errorf("numbers only = %+v", res.Groups)
	}

	if _, err := LogTemplates("{", 0); err == nil {
		t.Error("bad JSON accepted")
	}

	// Only the newest 4 000 lines are read; a long template is cut.
	var many []string
	for i := 0; i < 4100; i++ {
		many = append(many, fmt.Sprintf("tick %d %s", i, strings.Repeat("word ", 60)))
	}

	res := runLogTemplates(t, many, 0)
	if res.Lines != logTemplatesMaxLines || len([]rune(res.Groups[0].Template)) > logTemplatesMaxRunes {
		t.Errorf("lines=%d template runes=%d", res.Lines, len([]rune(res.Groups[0].Template)))
	}
}

func TestLogTemplatesDemoPodLog(t *testing.T) {
	res := runLogTemplates(t, strings.Split(strings.TrimSpace(demoPodLog("web-0", false)), "\n"), 0)
	repeated := 0

	for _, g := range res.Groups {
		if g.Count > 1 {
			repeated++
		}
	}

	if repeated < 2 {
		t.Errorf("demo log: %d repeated groups, want at least 2: %+v", repeated, res.Groups)
	}
}

func BenchmarkLogTemplates(b *testing.B) {
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, fmt.Sprintf(`level=info msg="GET /orders/%d" took=%dms from=10.0.%d.%d:443`, i, i%90, i%255, i%7))
	}

	in, _ := json.Marshal(lines)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := LogTemplates(string(in), 20); err != nil {
			b.Fatal(err)
		}
	}
}
