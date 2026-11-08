package ichorgo

import (
	"strconv"
	"strings"
)

// satisfiesRange tells whether version is inside a semver constraint as Flux and Argo CD
// write them: "*", ">=1.2 <2", "~1.2", "^1.2.3", "1.x", "1.2.*", "=1.2.3", alternatives
// joined by "||". A constraint it cannot read accepts nothing, so the candidate stays shown.
func satisfiesRange(constraint, version string) bool {
	v, ok := versionCore(version)
	if !ok {
		return false
	}

	for _, alt := range strings.Split(constraint, "||") {
		fields := strings.FieldsFunc(alt, func(r rune) bool { return r == ',' || r == ' ' })
		if len(fields) == 0 {
			continue
		}

		all := true
		for _, f := range fields {
			if !satisfiesOne(f, v) {
				all = false

				break
			}
		}

		if all {
			return true
		}
	}

	return false
}

func satisfiesOne(c string, v [3]int) bool {
	if c == "*" || c == "x" || c == "X" {
		return true
	}

	for _, op := range []string{">=", "<=", "!=", ">", "<", "=", "~", "^"} {
		rest, ok := strings.CutPrefix(c, op)
		if !ok {
			continue
		}

		bound, parts, ok := partialCore(rest)
		if !ok {
			return false
		}

		cmp := compareCore(v, bound)

		switch op {
		case ">=":
			return cmp >= 0
		case "<=":
			return cmp <= 0
		case ">":
			return cmp > 0
		case "<":
			return cmp < 0
		case "!=":
			return cmp != 0
		case "=":
			return prefixMatch(v, bound, parts)
		case "~":
			// ~1.2.3 and ~1.2: the same minor; ~1: the same major.
			return cmp >= 0 && prefixMatch(v, bound, min(parts, 2))
		default: // ^
			return cmp >= 0 && caretMatch(v, bound, parts)
		}
	}

	bound, parts, ok := partialCore(c)

	return ok && prefixMatch(v, bound, parts)
}

func caretMatch(v, bound [3]int, parts int) bool {
	switch {
	case bound[0] > 0 || parts == 1:
		return v[0] == bound[0]
	case bound[1] > 0 || parts == 2:
		return v[0] == 0 && v[1] == bound[1]
	}

	return v == bound
}

func prefixMatch(v, bound [3]int, parts int) bool {
	for i := range parts {
		if v[i] != bound[i] {
			return false
		}
	}

	return true
}

func compareCore(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}

			return 1
		}
	}

	return 0
}

// partialCore reads "1", "1.2", "1.2.3" or "1.x" (a leading v allowed): the numbers given,
// zero-filled, and how many were given before any wildcard.
func partialCore(s string) ([3]int, int, bool) {
	var out [3]int

	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "-")
	s, _, _ = strings.Cut(s, "+")

	parts := strings.Split(s, ".")
	if len(parts) > 3 || s == "" {
		return out, 0, false
	}

	for i, p := range parts {
		if p == "x" || p == "X" || p == "*" {
			return out, i, true
		}

		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, 0, false
		}

		out[i] = n
	}

	return out, len(parts), true
}

func versionCore(s string) ([3]int, bool) {
	v, parts, ok := partialCore(s)

	return v, ok && parts > 0
}
