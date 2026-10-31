package ichorgo

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Omni's machine class selectors (client/pkg/cosi/labels): each entry of match_labels is one
// alternative, its comma-separated terms must all hold. The terms as the runtime's Labels.Matches
// reads them: `key`, `!key`, `key = v`, `key == v`, `key != v`, `key in (a, b)`, `key notin (a)`,
// and the numeric `<`, `<=`, `>`, `>=`, which never hold for a missing label or a non-number.

var (
	omniTermCompare = regexp.MustCompile(`^(\S+?)\s*(==|!=|>=|<=|=|>|<)\s*(.*)$`)
	omniTermSet     = regexp.MustCompile(`^(\S+)\s+(in|notin)\s*\((.*)\)$`)
)

// omniSelectorMissing says what labels lack for any of selectors to hold: the failing terms of
// the alternative closest to holding; "" when one holds, or there is none to check. A term the
// app cannot read is left to Omni, not refused.
func omniSelectorMissing(selectors []string, labels map[string]string) string {
	var closest []string

	for _, selector := range selectors {
		failing := omniFailingTerms(selector, labels)
		if len(failing) == 0 {
			return ""
		}

		if closest == nil || len(failing) < len(closest) {
			closest = failing
		}
	}

	return strings.Join(closest, ", ")
}

func omniFailingTerms(selector string, labels map[string]string) []string {
	var failing []string

	for _, term := range omniSplitTerms(selector) {
		if !omniTermHolds(term, labels) {
			failing = append(failing, term)
		}
	}

	return failing
}

// omniSplitTerms splits at the commas outside parentheses.
func omniSplitTerms(selector string) []string {
	var (
		terms []string
		depth int
		start int
	)

	for i, c := range selector {
		switch {
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			terms = append(terms, strings.TrimSpace(selector[start:i]))
			start = i + 1
		}
	}

	terms = append(terms, strings.TrimSpace(selector[start:]))

	return slices.DeleteFunc(terms, func(t string) bool { return t == "" })
}

func omniTermHolds(term string, labels map[string]string) bool {
	if m := omniTermSet.FindStringSubmatch(term); m != nil {
		value, ok := labels[m[1]]
		in := ok && slices.ContainsFunc(strings.Split(m[3], ","), func(v string) bool { return strings.TrimSpace(v) == value })

		return in == (m[2] == "in")
	}

	if m := omniTermCompare.FindStringSubmatch(term); m != nil {
		return omniCompareHolds(labels, m[1], m[2], strings.TrimSpace(m[3]))
	}

	if key, inverted := strings.CutPrefix(term, "!"); inverted {
		_, ok := labels[strings.TrimSpace(key)]

		return !ok
	}

	if strings.ContainsAny(term, " \t") {
		return true // not a form the app reads
	}

	_, ok := labels[term]

	return ok
}

func omniCompareHolds(labels map[string]string, key, op, want string) bool {
	value, ok := labels[key]

	switch op {
	case "=", "==":
		return ok && value == want
	case "!=":
		return !ok || value != want
	}

	if !ok {
		return false
	}

	left, errL := strconv.ParseFloat(value, 64)
	right, errR := strconv.ParseFloat(want, 64)

	if errL != nil || errR != nil {
		return false
	}

	switch op {
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	}

	return left >= right
}
