package ichorgo

import (
	"fmt"
	"strings"
)

// diffOp is one line of a line diff: ' ' kept, '-' only in a, '+' only in b.
type diffOp struct {
	kind byte
	line string
}

// unifiedDiff is `diff -u` of two texts (fromName, toName in the header), "" when they are
// equal. The common head and tail are set aside first; what is left is compared line by
// line, or shown as removed then added when it is too large to compare.
func unifiedDiff(a, b, fromName, toName string) string {
	if a == b {
		return ""
	}

	ops := diffLines(diffSplitLines(a), diffSplitLines(b))

	var out strings.Builder

	fmt.Fprintf(&out, "--- %s\n+++ %s\n", fromName, toName)

	for _, h := range diffHunks(ops, diffContext) {
		out.WriteString(h)
	}

	return out.String()
}

// diffSplitLines splits s into its lines, without their "\n".
func diffSplitLines(s string) []string {
	if s == "" {
		return nil
	}

	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func diffLines(a, b []string) []diffOp {
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}

	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}

	ops := make([]diffOp, 0, len(a)+len(b))
	for _, l := range a[:head] {
		ops = append(ops, diffOp{' ', l})
	}

	ops = append(ops, diffMiddle(a[head:len(a)-tail], b[head:len(b)-tail])...)

	for _, l := range a[len(a)-tail:] {
		ops = append(ops, diffOp{' ', l})
	}

	return ops
}

// diffMiddle compares a and b by their longest common subsequence.
func diffMiddle(a, b []string) []diffOp {
	n, m := len(a), len(b)

	if (n+1)*(m+1) > diffMaxCells {
		ops := make([]diffOp, 0, n+m)
		for _, l := range a {
			ops = append(ops, diffOp{'-', l})
		}

		for _, l := range b {
			ops = append(ops, diffOp{'+', l})
		}

		return ops
	}

	// lcs[i][j] is the LCS length of a[i:] and b[j:], one flat table.
	w := m + 1
	lcs := make([]int32, (n+1)*w)

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}

	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0

	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[(i+1)*w+j] >= lcs[i*w+j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}

	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}

	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}

	return ops
}

// diffHunks groups the changes with ctx kept lines around each, as "@@ -l,s +l,s @@" hunks.
func diffHunks(ops []diffOp, ctx int) []string {
	var hunks []string

	for start := 0; start < len(ops); {
		first := start
		for first < len(ops) && ops[first].kind == ' ' {
			first++
		}

		if first == len(ops) {
			break
		}

		// Extend over changes closer than 2*ctx kept lines apart.
		last, kept := first, 0
		for i := first; i < len(ops) && kept <= 2*ctx; i++ {
			if ops[i].kind == ' ' {
				kept++
			} else {
				last, kept = i, 0
			}
		}

		from, to := max(first-ctx, 0), min(last+ctx+1, len(ops))
		hunks = append(hunks, renderHunk(ops, from, to))
		start = to
	}

	return hunks
}

func renderHunk(ops []diffOp, from, to int) string {
	aLine, bLine := 1, 1

	for _, op := range ops[:from] {
		if op.kind != '+' {
			aLine++
		}

		if op.kind != '-' {
			bLine++
		}
	}

	var body strings.Builder

	aCount, bCount := 0, 0

	for _, op := range ops[from:to] {
		if op.kind != '+' {
			aCount++
		}

		if op.kind != '-' {
			bCount++
		}

		body.WriteByte(op.kind)
		body.WriteString(op.line)
		body.WriteByte('\n')
	}

	// An empty side starts at the line before, as diff -u writes it.
	if aCount == 0 {
		aLine--
	}

	if bCount == 0 {
		bLine--
	}

	return fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", aLine, aCount, bLine, bCount) + body.String()
}
