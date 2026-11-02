package ichorgo

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// ansiSampleLines are the shared vectors: the Kotlin and Swift tests decode the same lines'
// entries (written by TestANSISampleEntriesJSON) and expect the same spans.
var ansiSampleLines = []string{
	"\x1b[36mINFO\x1b[0m[0042] request done    \x1b[36mclient_ip\x1b[0m=10.0.0.7 \x1b[36mstatus\x1b[0m=200",
	"\x1b[1;31mERRO\x1b[0m[0043] upstream timeout \x1b[31merror\x1b[0m=\"dial tcp: i/o timeout\"",
	"\x1b[38;5;208mWARN\x1b[39m queue depth \x1b[38;2;255;136;0m850\x1b[0m",
	"\x1b]8;;https://example.invalid/run/1\x1b\\run 1\x1b]8;;\x1b\\ finished",
}

// sp is a span in the default colours; with sets the rest.
func sp(t string) logSpan { return logSpan{T: t, Fg: -1, Bg: -1} }

func fg(t string, c int) logSpan {
	s := sp(t)
	s.Fg = c

	return s
}

func TestStripANSI(t *testing.T) {
	bold := func(s logSpan) logSpan { s.Bold = true; return s }
	rgb := func(t, hex string) logSpan { s := sp(t); s.FgRGB = hex; return s }

	for _, tc := range []struct {
		name, line, plain string
		spans             []logSpan
	}{
		{"no escape", "plain text", "plain text", nil},
		{
			"logrus info", ansiSampleLines[0], "INFO[0042] request done    client_ip=10.0.0.7 status=200",
			[]logSpan{fg("INFO", 6), sp("[0042] request done    "), fg("client_ip", 6), sp("=10.0.0.7 "), fg("status", 6), sp("=200")},
		},
		{
			"logrus error", ansiSampleLines[1], `ERRO[0043] upstream timeout error="dial tcp: i/o timeout"`,
			[]logSpan{bold(fg("ERRO", 1)), sp("[0043] upstream timeout "), fg("error", 1), sp(`="dial tcp: i/o timeout"`)},
		},
		{
			"256 and true colour", ansiSampleLines[2], "WARN queue depth 850",
			[]logSpan{rgb("WARN", "#ff8700"), sp(" queue depth "), rgb("850", "#ff8800")},
		},
		{"osc 8 link keeps its text", ansiSampleLines[3], "run 1 finished", nil},
		{"osc ended by BEL", "\x1b]0;title\x07after", "after", nil},
		{
			"nested bold and colour", "\x1b[1ma\x1b[32mb\x1b[22mc\x1b[0md", "abcd",
			[]logSpan{bold(sp("a")), bold(fg("b", 2)), fg("c", 2), sp("d")},
		},
		{"reset with nothing open", "\x1b[0mtext", "text", []logSpan{sp("text")}},
		{"empty sgr is a reset", "\x1b[31mr\x1b[mx", "rx", []logSpan{fg("r", 1), sp("x")}},
		{"bright", "\x1b[91mx\x1b[0m", "x", []logSpan{fg("x", 9)}},
		{"256 low index stays an index", "\x1b[38;5;3mx", "x", []logSpan{fg("x", 3)}},
		{"256 grey", "\x1b[38;5;232mx", "x", []logSpan{rgb("x", "#080808")}},
		{"itu true colour", "\x1b[38:2::255:136:0mx", "x", []logSpan{rgb("x", "#ff8800")}},
		{"colon true colour without id", "\x1b[38:2:1:2:3mx", "x", []logSpan{rgb("x", "#010203")}},
		{"true colour then bold", "\x1b[38;2;1;2;3;1mx", "x", []logSpan{bold(rgb("x", "#010203"))}},
		{
			"background", "\x1b[41;97mx", "x",
			[]logSpan{{T: "x", Fg: 15, Bg: 1}},
		},
		{"inverse swaps", "\x1b[31;7mx", "x", []logSpan{{T: "x", Fg: -1, Bg: 1}}},
		{"unknown codes keep the style", "\x1b[31m\x1b[5;9mx", "x", []logSpan{fg("x", 1)}},
		{
			"styles", "\x1b[2;3;4mx\x1b[23;24my", "xy",
			[]logSpan{{T: "x", Fg: -1, Bg: -1, Dim: true, Italic: true, Underline: true}, {T: "y", Fg: -1, Bg: -1, Dim: true}},
		},
		{"same style merges", "\x1b[31ma\x1b[31mb", "ab", []logSpan{fg("ab", 1)}},
		{"cursor move dropped", "\x1b[2Kline\x1b[1A", "line", nil},
		{"lone trailing escape", "text\x1b", "text", nil},
		{"lone escape keeps what follows", "a\x1bcb", "ab", nil},
		{"unterminated csi", "text\x1b[31", "text", nil},
		{"malformed csi keeps the rest", "a\x1b[3\x01b", "a\x01b", nil},
		{"crlf", "\x1b[32mok\x1b[0m\r\n", "ok\r\n", []logSpan{fg("ok", 2), sp("\r\n")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain, spans := stripANSI(tc.line)
			if plain != tc.plain {
				t.Errorf("plain = %q, want %q", plain, tc.plain)
			}

			if !reflect.DeepEqual(spans, tc.spans) {
				t.Errorf("spans = %+v\n want %+v", spans, tc.spans)
			}
		})
	}
}

func TestParseLogLineStripsColours(t *testing.T) {
	for _, l := range ansiSampleLines {
		e := parseLogLine(l, logNow)

		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}

		if strings.Contains(string(b), `\u001b`) {
			t.Errorf("escape left in %s", b)
		}
	}

	// A coloured logfmt line: clean keys, its level read.
	e := parseLogLine("\x1b[36mlevel\x1b[0m=error \x1b[36mmsg\x1b[0m=\"upstream timeout\" \x1b[36mclient_ip\x1b[0m=10.0.0.7 \x1b[36mstatus\x1b[0m=504", logNow)
	if e.Level != "error" || e.Msg != "upstream timeout" || !equalJSON(t, e.Fields, kv("client_ip", "10.0.0.7", "status", "504")) {
		t.Errorf("got %+v", e)
	}

	// Searching the stripped text finds what the person sees.
	if e := parseLogLine(ansiSampleLines[0], logNow); !strings.Contains(e.Raw, "INFO[0042]") {
		t.Errorf("raw %q", e.Raw)
	}
}

func TestParseLogText(t *testing.T) {
	tail := parseLogText(ansiSampleLines[0]+"\nplain\n", logNow)
	if len(tail.Lines) != 2 || len(tail.Entries) != 2 || tail.Truncated {
		t.Fatalf("got %+v", tail)
	}

	if tail.Lines[0] != "INFO[0042] request done    client_ip=10.0.0.7 status=200" || len(tail.Entries[0].Spans) != 6 {
		t.Errorf("got %+v", tail.Entries[0])
	}

	if empty := parseLogText("", logNow); len(empty.Lines) != 0 || empty.Entries == nil {
		t.Errorf("empty: %+v", empty)
	}

	long := strings.Repeat("x\n", maxLogLines+3) + "last"

	got := parseLogText(long, logNow)
	if !got.Truncated || len(got.Lines) != maxLogLines || got.Lines[maxLogLines-1] != "last" {
		t.Errorf("long: %d lines, truncated %v", len(got.Lines), got.Truncated)
	}
}

func TestParseLogTextExportedJSON(t *testing.T) {
	SetPrivacyMask(false, "")

	out, err := ParseLogText(ansiSampleLines[1] + "\n")
	if err != nil {
		t.Fatal(err)
	}

	var tail logTail
	if err := json.Unmarshal([]byte(out), &tail); err != nil {
		t.Fatal(err)
	}

	if len(tail.Entries) != 1 || tail.Entries[0].Spans[0] != (logSpan{T: "ERRO", Fg: 1, Bg: -1, Bold: true}) {
		t.Errorf("got %s", out)
	}
}

func TestStripLogEscapes(t *testing.T) {
	SetPrivacyMask(false, "")

	got := StripLogEscapes(ansiSampleLines[0] + "\n" + ansiSampleLines[3] + "\n")
	if want := "INFO[0042] request done    client_ip=10.0.0.7 status=200\nrun 1 finished\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A line of escapes as long as a kubelet line parses in linear time.
func TestStripANSILongLine(t *testing.T) {
	line := strings.Repeat("\x1b[31ma\x1b[0m\x1b[", kubeLogLineMax/12)

	plain, _ := stripANSI(line)
	if len(plain) != kubeLogLineMax/12 {
		t.Errorf("plain is %d bytes", len(plain))
	}
}

// The coloured logrus line against the same line without its colours: the spec's budget is 2×.
func BenchmarkParseLogLineColoured(b *testing.B) {
	plain, _ := stripANSI(ansiSampleLines[0])

	for name, line := range map[string]string{"plain": plain, "coloured": ansiSampleLines[0]} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				parseLogLine(line, logNow)
			}
		})
	}
}
