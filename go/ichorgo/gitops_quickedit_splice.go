package ichorgo

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

// textEdit replaces content[start:end] with text; line is the 1-based line it lands on.
type textEdit struct {
	start, end int
	text       string
	line       int
}

// spliceTargets writes each target into content and returns the new content and the first
// changed line (the first target's line when nothing changed).
func spliceTargets(content string, targets []quickEditTarget) (string, int, error) {
	starts := lineStarts(content)

	var (
		edits []textEdit
		first int
	)

	for _, t := range targets {
		ed, changed, err := targetEdit(content, starts, t)
		if err != nil {
			return "", 0, err
		}

		if first == 0 {
			first = ed.line
		}

		if changed {
			edits = append(edits, ed)
		}
	}

	if len(edits) == 0 {
		return content, first, nil
	}

	// Apply from the end so earlier offsets stay valid; at one offset, the later target
	// first, so the targets keep their order in the file.
	order := make([]int, len(edits))
	for i := range order {
		order[i] = i
	}

	slices.SortStableFunc(order, func(a, b int) int {
		if edits[a].start != edits[b].start {
			return edits[b].start - edits[a].start
		}

		return b - a
	})

	out := content
	for _, i := range order {
		out = out[:edits[i].start] + edits[i].text + out[edits[i].end:]
	}

	return out, minEditLine(edits), nil
}

func minEditLine(edits []textEdit) int {
	best := edits[0]
	for _, ed := range edits[1:] {
		if ed.start < best.start {
			best = ed
		}
	}

	return best.line
}

func targetEdit(content string, starts []int, t quickEditTarget) (textEdit, bool, error) {
	if t.node != nil {
		start, end, err := scalarSpan(content, starts, t.node)
		if err != nil {
			return textEdit{}, false, err
		}

		text, err := renderScalar(t.node.Style, t.value, t.tag)
		if err != nil {
			return textEdit{}, false, err
		}

		ed := textEdit{start: start, end: end, text: text, line: t.node.Line}
		changed := t.node.Value != t.value || (t.node.ShortTag() != t.tag && t.node.Style&quotedStyles == 0)

		return ed, changed, nil
	}

	if t.after.Line < 1 || t.after.Line > len(starts) {
		return textEdit{}, false, errors.New("the value's position is unknown: edit it in the editor")
	}

	text, err := renderScalar(0, t.value, t.tag)
	if err != nil {
		return textEdit{}, false, err
	}

	eol, at := lineEnd(content, starts, t.after.Line)
	newLine := strings.Repeat(" ", t.indent) + t.key + ": " + text + eol

	if at == len(content) && !strings.HasSuffix(content, "\n") {
		newLine = eol + strings.Repeat(" ", t.indent) + t.key + ": " + text
	}

	return textEdit{start: at, end: at, text: newLine, line: t.after.Line + 1}, true, nil
}

const quotedStyles = yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle

func lineStarts(s string) []int {
	starts := []int{0}

	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && i+1 < len(s) {
			starts = append(starts, i+1)
		}
	}

	return starts
}

// lineEnd is the line ending of a line ("\n" or "\r\n") and the offset just past it.
func lineEnd(content string, starts []int, line int) (string, int) {
	i := strings.IndexByte(content[starts[line-1]:], '\n')
	if i < 0 {
		return "\n", len(content)
	}

	at := starts[line-1] + i + 1
	if i > 0 && content[at-2] == '\r' {
		return "\r\n", at
	}

	return "\n", at
}

// scalarSpan is the byte range of a one-line scalar's text in content (quotes included,
// anchor and tag excluded), found from its node's line and column.
func scalarSpan(content string, starts []int, n *yaml.Node) (int, int, error) {
	if n.Kind != yaml.ScalarNode || n.Line < 1 || n.Line > len(starts) {
		return 0, 0, errors.New("the value's position is unknown: edit it in the editor")
	}

	ls := starts[n.Line-1]

	le := len(content)
	if i := strings.IndexByte(content[ls:], '\n'); i >= 0 {
		le = ls + i
	}

	i := ls + runeOffset(content[ls:le], n.Column-1)

	// An anchor (&a) or a tag (!!str) before the value.
	for i < le && (content[i] == '&' || content[i] == '!') {
		for i < le && content[i] != ' ' && content[i] != '\t' {
			i++
		}

		for i < le && (content[i] == ' ' || content[i] == '\t') {
			i++
		}
	}

	switch {
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return 0, 0, errors.New("the value is a block scalar (| or >): edit it in the editor")
	case n.Style&yaml.DoubleQuotedStyle != 0:
		if end, ok := quotedEnd(content[:le], i, '"'); ok {
			return i, end, nil
		}
	case n.Style&yaml.SingleQuotedStyle != 0:
		if end, ok := quotedEnd(content[:le], i, '\''); ok {
			return i, end, nil
		}
	default:
		if end := i + len(n.Value); end <= le && content[i:end] == n.Value {
			return i, end, nil
		}
	}

	return 0, 0, fmt.Errorf("the value on line %d spans several lines: edit it in the editor", n.Line)
}

func runeOffset(line string, runes int) int {
	off := 0
	for k := 0; k < runes && off < len(line); k++ {
		_, w := utf8.DecodeRuneInString(line[off:])
		off += w
	}

	return off
}

// quotedEnd is the offset just past the closing quote of the scalar opening at i, on the
// same line (a doubled single quote escapes one, a backslash escapes in double quotes).
func quotedEnd(s string, i int, q byte) (int, bool) {
	if i >= len(s) || s[i] != q {
		return 0, false
	}

	for j := i + 1; j < len(s); j++ {
		switch {
		case q == '"' && s[j] == '\\':
			j++
		case s[j] == q && q == '\'' && j+1 < len(s) && s[j+1] == '\'':
			j++
		case s[j] == q:
			return j + 1, true
		}
	}

	return 0, false
}

// renderScalar writes value in the scalar's current style: quoted stays quoted (same quote),
// plain stays plain when YAML reads it back with the same tag, and is double-quoted when a
// string would otherwise read as a number or a boolean ("1.10", "true").
func renderScalar(style yaml.Style, value, tag string) (string, error) {
	switch {
	case style&yaml.DoubleQuotedStyle != 0:
		return strconv.Quote(value), nil
	case style&yaml.SingleQuotedStyle != 0:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'", nil
	case plainReadsAs(value, tag):
		return value, nil
	case tag == "!!str":
		return strconv.Quote(value), nil
	}

	return "", fmt.Errorf("%q cannot be written as a %s", value, strings.TrimPrefix(tag, "!!"))
}

func plainReadsAs(value, tag string) bool {
	var doc yaml.Node
	if yaml.Unmarshal([]byte("v: "+value+"\n"), &doc) != nil || len(doc.Content) == 0 {
		return false
	}

	m := doc.Content[0]
	if m.Kind != yaml.MappingNode || len(m.Content) != 2 {
		return false
	}

	v := m.Content[1]

	return v.Kind == yaml.ScalarNode && v.Style == 0 && v.Value == value && v.ShortTag() == tag
}
