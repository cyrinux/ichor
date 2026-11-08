package upstream

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// version is a tag split by the policy: prefix, core numbers, prerelease, suffix.
// "v1.20.0-rc.1" is {v, [1 20 0], rc.1, ""}; "1.27.0-debian-12-r5" is {"", [1 27 0], "",
// "-debian-12", revision 5}.
type version struct {
	tag      string
	prefix   string
	core     []int
	pre      string
	suffix   string // build metadata and the revision left out
	revision int    // -r5; -1 when the tag has none
}

var (
	// Letters only in the prefix ("v", "release-"), two to four dotted numbers in the core.
	tagPattern = regexp.MustCompile(`^([A-Za-z]+[-_]?)?(\d+(?:\.\d+){1,3})(.*)$`)
	// A prerelease right after the core: -rc.1, -beta2, .alpha, -dev.
	prePattern = regexp.MustCompile(`(?i)^[-.]?((?:alpha|beta|rc|pre|preview|dev|snapshot|nightly)(?:[.-]?\d+)*)`)
	// A rebuild counter at the end of the suffix (Bitnami's -r5, a plain -1).
	revisionPattern = regexp.MustCompile(`^(.*)-r(\d+)$|^-(\d+)$`)
)

// parseVersion splits tag by the policy; ok is false for a tag that is not a version
// (latest, main, sha-abc123, 20241001).
func parseVersion(tag string) (version, bool) {
	m := tagPattern.FindStringSubmatch(tag)
	if m == nil {
		return version{}, false
	}

	v := version{tag: tag, prefix: m[1], revision: -1}

	for _, part := range strings.Split(m[2], ".") {
		n, err := strconv.Atoi(part)
		if err != nil || len(part) > 9 {
			return version{}, false
		}

		v.core = append(v.core, n)
	}

	rest := m[3]
	if pm := prePattern.FindStringSubmatch(rest); pm != nil {
		if after := rest[len(pm[0]):]; after == "" || strings.ContainsAny(after[:1], "-_+") {
			v.pre, rest = strings.ToLower(pm[1]), after
		}
	}

	rest, _, _ = strings.Cut(rest, "+") // build metadata does not change the shape

	if rm := revisionPattern.FindStringSubmatch(rest); rm != nil {
		digits := rm[2]
		if digits == "" {
			digits, rm[1] = rm[3], ""
		}

		n, err := strconv.Atoi(digits)
		if err == nil {
			rest, v.revision = rm[1], n
		}
	}

	v.suffix = rest

	return v, true
}

// shape is the template other tags must follow to be compared: "v{core}-alpine".
func (v version) shape() string {
	s := v.prefix + "{core}" + v.suffix
	if v.revision >= 0 {
		s += "-r{n}"
	}

	return s
}

// sameShape: same prefix, suffix, revision presence and number of core parts.
func (v version) sameShape(o version) bool {
	return v.prefix == o.prefix && v.suffix == o.suffix && len(v.core) == len(o.core) && (v.revision >= 0) == (o.revision >= 0)
}

// compare orders core numbers, then a release after its prereleases, then the revision.
func (v version) compare(o version) int {
	if c := slices.Compare(v.core, o.core); c != 0 {
		return c
	}

	switch {
	case v.pre == o.pre:
	case v.pre == "":
		return 1
	case o.pre == "":
		return -1
	default:
		if c := comparePre(v.pre, o.pre); c != 0 {
			return c
		}
	}

	switch {
	case v.revision < o.revision:
		return -1
	case v.revision > o.revision:
		return 1
	}

	return 0
}

// comparePre compares prereleases by their letter and number runs: rc.2 > rc.1, rc10 > rc9,
// beta > alpha.
func comparePre(a, b string) int {
	ta, tb := preTokens(a), preTokens(b)
	for i := 0; i < len(ta) && i < len(tb); i++ {
		na, ea := strconv.Atoi(ta[i])
		nb, eb := strconv.Atoi(tb[i])

		switch {
		case ea == nil && eb == nil && na != nb:
			if na < nb {
				return -1
			}

			return 1
		case ea != nil || eb != nil:
			if c := strings.Compare(ta[i], tb[i]); c != 0 {
				return c
			}
		}
	}

	return len(ta) - len(tb)
}

func preTokens(s string) []string {
	var out []string

	start := 0
	for i := 1; i <= len(s); i++ {
		if i == len(s) || isDigit(s[i]) != isDigit(s[i-1]) || s[i] == '.' || s[i] == '-' {
			if tok := strings.Trim(s[start:i], ".-"); tok != "" {
				out = append(out, tok)
			}

			start = i
		}
	}

	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// Policy reasons.
const (
	ReasonNotVersion  = "not a version"
	ReasonDigest      = "pinned by digest"
	ReasonCredentials = "registry needs credentials the app does not have"
)

// classify fills the patch, minor and major candidates of current among tags.
func classify(current string, tags []string) Candidate {
	out := Candidate{Current: current}

	cur, ok := parseVersion(current)
	if !ok {
		out.Reason = ReasonNotVersion

		return out
	}

	out.Shape, out.Prerelease = cur.shape(), cur.pre != ""

	var patch, minor, major *version

	for _, tag := range tags {
		// Cheap filter first: thousands of tags, most of another shape.
		if !strings.HasPrefix(tag, cur.prefix) || !strings.Contains(tag, cur.suffix) {
			continue
		}

		v, ok := parseVersion(tag)
		if !ok || !cur.sameShape(v) || (v.pre != "" && cur.pre == "") || v.compare(cur) <= 0 {
			continue
		}

		major = newer(major, v)
		if v.core[0] == cur.core[0] {
			minor = newer(minor, v)
			if v.core[1] == cur.core[1] {
				patch = newer(patch, v)
			}
		}
	}

	out.Patch = tagOf(patch)
	if out.Minor = tagOf(minor); out.Minor == out.Patch {
		out.Minor = ""
	}

	if out.Major = tagOf(major); out.Major == tagOf(minor) || out.Major == out.Patch {
		out.Major = ""
	}

	return out
}

func newer(best *version, v version) *version {
	if best == nil || v.compare(*best) > 0 {
		return &v
	}

	return best
}

func tagOf(v *version) string {
	if v == nil {
		return ""
	}

	return v.tag
}
