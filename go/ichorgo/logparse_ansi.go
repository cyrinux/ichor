package ichorgo

import (
	"fmt"
	"strings"
	"time"
)

// Containers that log with colours on (logrus ForceColors, zerolog's ConsoleWriter) write ANSI
// escape sequences into their log. stripANSI removes every one of them and keeps the SGR
// (colour, bold…) ones as styled spans, so no escape byte reaches the apps and they draw the
// colours instead. One pass over the bytes, no regular expression: a 256 KiB line full of
// escapes stays linear, and a malformed sequence never eats the rest of the line.

const ansiESC = 0x1b

// logSpan is a run of text with one style. Fg and Bg are a 16-colour index (0-7 normal, 8-15
// bright) or -1 for the default; FgRGB and BgRGB ("#rrggbb") hold a 256 or true colour, and
// then Fg/Bg are -1.
type logSpan struct {
	T         string `json:"t"`
	Fg        int    `json:"fg"`
	Bg        int    `json:"bg"`
	FgRGB     string `json:"fgRgb,omitempty"`
	BgRGB     string `json:"bgRgb,omitempty"`
	Bold      bool   `json:"b,omitempty"`
	Dim       bool   `json:"d,omitempty"`
	Italic    bool   `json:"i,omitempty"`
	Underline bool   `json:"u,omitempty"`
}

// ansiStyle is the SGR state; inverse is applied when a span is written.
type ansiStyle struct {
	fg, bg                       int
	fgRGB, bgRGB                 string
	bold, dim, italic, underline bool
	inverse                      bool
}

var ansiDefault = ansiStyle{fg: -1, bg: -1}

func (s ansiStyle) span(text string) logSpan {
	fg, bg, fgRGB, bgRGB := s.fg, s.bg, s.fgRGB, s.bgRGB
	if s.inverse {
		fg, bg, fgRGB, bgRGB = bg, fg, bgRGB, fgRGB
	}

	return logSpan{
		T: text, Fg: fg, Bg: bg, FgRGB: fgRGB, BgRGB: bgRGB,
		Bold: s.bold, Dim: s.dim, Italic: s.italic, Underline: s.underline,
	}
}

// stripANSI returns s without escape sequences and, when it had any SGR styling, its text as
// spans (nil otherwise: the line is drawn plainly).
func stripANSI(s string) (string, []logSpan) {
	if strings.IndexByte(s, ansiESC) < 0 {
		return s, nil
	}

	var (
		b      strings.Builder
		spans  = make([]logSpan, 0, 8)
		styled bool
		style  = ansiDefault
		spanAt int
	)

	b.Grow(len(s))

	flush := func() {
		text := b.String()[spanAt:]
		if text == "" {
			return
		}

		spanAt = b.Len()

		next := style.span(text)
		if n := len(spans); n > 0 && sameStyle(spans[n-1], next) {
			spans[n-1].T += text

			return
		}

		spans = append(spans, next)
	}

	for i := 0; i < len(s); {
		if s[i] != ansiESC {
			j := i + 1
			for j < len(s) && s[j] != ansiESC {
				j++
			}

			b.WriteString(s[i:j])
			i = j

			continue
		}

		next, params, sgr := ansiSequence(s, i)
		if sgr {
			flush()

			style = applySGR(style, params)
			styled = true
		}

		i = next
	}

	flush()

	if !styled {
		return b.String(), nil
	}

	return b.String(), spans
}

func sameStyle(a, b logSpan) bool {
	a.T, b.T = "", ""

	return a == b
}

// ansiSequence reads the escape sequence starting at s[i] (an ESC) and returns where the text
// resumes, and the parameters when it is an SGR (CSI … m). A lone ESC is dropped alone; an
// unterminated CSI or OSC at the end of the line is dropped with the bytes it covers.
func ansiSequence(s string, i int) (next int, params string, sgr bool) {
	if i+1 >= len(s) {
		return i + 1, "", false
	}

	switch s[i+1] {
	case '[': // CSI: parameter and intermediate bytes, then a final byte 0x40-0x7E.
		for j := i + 2; j < len(s); j++ {
			c := s[j]
			if c >= 0x40 && c <= 0x7e {
				return j + 1, s[i+2 : j], c == 'm'
			}

			if c < 0x20 || c > 0x3f {
				// Not a CSI byte: the sequence is malformed, keep what follows.
				return j, "", false
			}
		}

		return len(s), "", false
	case ']': // OSC: up to BEL or ST (ESC \). OSC 8's link text sits between two OSCs.
		for j := i + 2; j < len(s); j++ {
			switch {
			case s[j] == 0x07:
				return j + 1, "", false
			case s[j] == ansiESC && j+1 < len(s) && s[j+1] == '\\':
				return j + 2, "", false
			}
		}

		return len(s), "", false
	default: // A two-byte escape (ESC c, ESC =…): dropped.
		return i + 2, "", false
	}
}

// applySGR applies the parameters of one SGR sequence. Parameters are separated by ';'; a
// parameter may carry ':'-separated sub-parameters (ITU T.416: 38:2::r:g:b). An empty
// parameter is 0, unknown ones are ignored and do not reset the style.
func applySGR(st ansiStyle, params string) ansiStyle {
	var ps sgrParams

	ps.parse(params)

	heads := ps.heads[:ps.groups]

	for k := 0; k < len(heads); k++ {
		p := heads[k]
		if p != 38 && p != 48 {
			st = applySGRCode(st, p)

			continue
		}

		var (
			idx int
			rgb string
		)

		if g := ps.group(k); len(g) > 1 {
			idx, rgb = colonColor(g[1:])
		} else {
			var used int

			idx, rgb, used = semicolonColor(heads[k+1:])
			k += used
		}

		switch {
		case idx < 0 && rgb == "":
		case p == 38:
			st.fg, st.fgRGB = idx, rgb
		default:
			st.bg, st.bgRGB = idx, rgb
		}
	}

	return st
}

func applySGRCode(st ansiStyle, p int) ansiStyle {
	switch {
	case p == 0:
		return ansiDefault
	case p == 1:
		st.bold = true
	case p == 2:
		st.dim = true
	case p == 3:
		st.italic = true
	case p == 4:
		st.underline = true
	case p == 7:
		st.inverse = true
	case p == 22:
		st.bold, st.dim = false, false
	case p == 23:
		st.italic = false
	case p == 24:
		st.underline = false
	case p == 27:
		st.inverse = false
	case p >= 30 && p <= 37:
		st.fg, st.fgRGB = p-30, ""
	case p >= 90 && p <= 97:
		st.fg, st.fgRGB = p-90+8, ""
	case p == 39:
		st.fg, st.fgRGB = -1, ""
	case p >= 40 && p <= 47:
		st.bg, st.bgRGB = p-40, ""
	case p >= 100 && p <= 107:
		st.bg, st.bgRGB = p-100+8, ""
	case p == 49:
		st.bg, st.bgRGB = -1, ""
	}

	return st
}

// sgrMaxParams bounds the parameters read from one SGR sequence; the rest are ignored.
const sgrMaxParams = 16

// sgrParams is one SGR sequence's parameters, in fixed arrays so parsing does not allocate:
// "1;38:2::255:0:0" is the groups [1] and [38 2 0 255 0 0], "" is [0].
type sgrParams struct {
	vals   [sgrMaxParams]int
	start  [sgrMaxParams]int // where group i starts in vals
	heads  [sgrMaxParams]int // group i's first value
	n      int               // values
	groups int
}

func (p *sgrParams) parse(params string) {
	v, open := 0, false

	for i := 0; i <= len(params) && p.n < sgrMaxParams; i++ {
		end := i == len(params)
		if !end && params[i] != ';' && params[i] != ':' {
			if c := params[i]; c >= '0' && c <= '9' && v < 1<<16 {
				v = v*10 + int(c-'0')
			}

			continue
		}

		if !open {
			p.start[p.groups], p.heads[p.groups] = p.n, v
			p.groups++
			open = true
		}

		p.vals[p.n] = v
		p.n++
		v = 0

		if end || params[i] == ';' {
			open = false
		}
	}
}

// group is group i's values.
func (p *sgrParams) group(i int) []int {
	end := p.n
	if i+1 < p.groups {
		end = p.start[i+1]
	}

	return p.vals[p.start[i]:end]
}

// semicolonColor reads the arguments of 38/48 in the common form "5;n" or "2;r;g;b" from the
// heads of the groups after it, and how many groups it used.
func semicolonColor(heads []int) (idx int, rgb string, used int) {
	if len(heads) == 0 {
		return -1, "", 0
	}

	switch heads[0] {
	case 5:
		if len(heads) < 2 {
			return -1, "", 1
		}

		return xterm256(heads[1]), xtermRGB(heads[1]), 2
	case 2:
		if len(heads) < 4 {
			return -1, "", len(heads)
		}

		return -1, hexRGB(heads[1], heads[2], heads[3]), 4
	default:
		return -1, "", 1
	}
}

// colonColor reads the sub-parameters of 38/48: "5:n", "2:r:g:b" or "2:id:r:g:b".
func colonColor(sub []int) (int, string) {
	switch {
	case sub[0] == 5 && len(sub) >= 2:
		return xterm256(sub[1]), xtermRGB(sub[1])
	case sub[0] == 2 && len(sub) >= 5:
		return -1, hexRGB(sub[2], sub[3], sub[4])
	case sub[0] == 2 && len(sub) == 4:
		return -1, hexRGB(sub[1], sub[2], sub[3])
	default:
		return -1, ""
	}
}

// xterm256 is the 16-colour index of an xterm colour 0-15, else -1.
func xterm256(n int) int {
	if n >= 0 && n < 16 {
		return n
	}

	return -1
}

// xtermRGB is "#rrggbb" for the colour cube (16-231) and the greys (232-255), else "".
func xtermRGB(n int) string {
	switch {
	case n >= 16 && n <= 231:
		n -= 16
		level := func(v int) int {
			if v == 0 {
				return 0
			}

			return 55 + v*40
		}

		return hexRGB(level(n/36), level(n/6%6), level(n%6))
	case n >= 232 && n <= 255:
		g := 8 + (n-232)*10

		return hexRGB(g, g, g)
	default:
		return ""
	}
}

func hexRGB(r, g, b int) string {
	return fmt.Sprintf("#%02x%02x%02x", clampByte(r), clampByte(g), clampByte(b))
}

func clampByte(v int) int { return min(max(v, 0), 255) }

// StripLogEscapes returns text without ANSI escape sequences, line by line: what the apps copy
// and share from a log.
func StripLogEscapes(text string) (out string) {
	var err error

	defer maskResult(&out, &err)

	// The text was masked for display: map it back so masking it again reuses its fakes.
	text = privacy.unmaskText(text)

	if strings.IndexByte(text, ansiESC) < 0 {
		return text
	}

	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i], _ = stripANSI(l)
	}

	return strings.Join(lines, "\n")
}

// ParseLogText parses a whole log snapshot (a pod's or a workload's) in one call into the
// logTail of ServiceLogs: {"lines":[...],"entries":[...],"truncated"}. The empty line after a
// final newline is dropped; the newest maxLogLines lines are kept, and truncated is set when
// older ones were dropped. lines hold the text without escapes, entries their spans.
func ParseLogText(text string) (out string, err error) {
	defer maskResult(&out, &err)

	text = privacy.unmaskText(text)

	out, err = toJSON(parseLogText(text, time.Now()))

	return out, err
}

func parseLogText(text string, now time.Time) logTail {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = []string{}
	}

	truncated := len(lines) > maxLogLines
	if truncated {
		lines = lines[len(lines)-maxLogLines:]
	}

	entries := parseLogLines(lines, now)

	plain := make([]string, len(entries))
	for i, e := range entries {
		plain[i] = e.Raw
	}

	return logTail{Lines: plain, Entries: entries, Truncated: truncated}
}
