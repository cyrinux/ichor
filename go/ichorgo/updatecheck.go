package ichorgo

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const updateCheckTTL = 6 * time.Hour

type talosUpdate struct {
	Latest     string `json:"latest"`
	LatestDate string `json:"latestDate"`
	Newer      bool   `json:"newer"`
	Outdated   int    `json:"outdated"` // nodes below latest
	Oldest     string `json:"oldest"`
	Notes      string `json:"notes"` // release notes URL
}

var releaseCache struct {
	sync.Mutex
	at       time.Time
	releases []talosRelease

	// The last failure, kept a while: offline or rate limited (60 requests an hour without
	// a token), every screen asking again would wait for the fetch timeout.
	failedAt time.Time
	err      error
}

const updateCheckRetry = 5 * time.Minute

// TalosUpdateCheck tells whether a newer stable Talos release than the nodes run exists.
// nodeVersions is comma-separated, one entry per node ("v1.14.1,v1.14.1,v1.13.4"); entries
// that are empty or not versions are skipped. Releases come from GitHub, cached for 6 h.
func TalosUpdateCheck(nodeVersions string) (out string, err error) {
	defer maskResult(&out, &err)

	releases, err := cachedReleases()
	if err != nil {
		return "", err
	}

	return toJSON(checkUpdate(releases, nodeVersions))
}

func cachedReleases() ([]talosRelease, error) {
	releaseCache.Lock()
	defer releaseCache.Unlock()

	if releaseCache.releases != nil && time.Since(releaseCache.at) < updateCheckTTL {
		return releaseCache.releases, nil
	}

	if releaseCache.err != nil && time.Since(releaseCache.failedAt) < updateCheckRetry {
		return nil, releaseCache.err
	}

	releases, err := fetchReleases()
	if err != nil {
		releaseCache.failedAt, releaseCache.err = time.Now(), err

		return nil, err
	}

	releaseCache.at, releaseCache.releases, releaseCache.err = time.Now(), releases, nil

	return releases, nil
}

// checkUpdate compares the node versions with the newest stable release.
func checkUpdate(releases []talosRelease, nodeVersions string) talosUpdate {
	var latest *talosRelease

	for i := range releases {
		r := &releases[i]
		if r.Prerelease || prereleaseOf(r.Version) != "" {
			continue
		}

		if latest == nil || compareVersions(r.Version, latest.Version) > 0 {
			latest = r
		}
	}

	out := talosUpdate{}
	if latest == nil {
		return out
	}

	out.Latest, out.LatestDate = latest.Version, latest.Date
	out.Notes = "https://github.com/siderolabs/talos/releases/tag/" + latest.Version

	for _, raw := range strings.Split(nodeVersions, ",") {
		v, ok := normalizeTalosVersion(raw)
		if !ok {
			continue
		}

		if out.Oldest == "" || compareVersions(v, out.Oldest) < 0 {
			out.Oldest = v
		}

		if compareVersions(v, latest.Version) < 0 {
			out.Outdated++
		}
	}

	out.Newer = out.Outdated > 0

	return out
}

// compareVersions orders "v1.14.10" after "v1.14.9", and a prerelease ("v1.15.0-alpha.1")
// before its release. Returns -1, 0 or 1.
func compareVersions(a, b string) int {
	na, nb := versionNumbers(a), versionNumbers(b)
	if c := slices.Compare(na[:], nb[:]); c != 0 {
		return c
	}

	pa, pb := prereleaseOf(a), prereleaseOf(b)

	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	case pa < pb:
		return -1
	default:
		return 1
	}
}

func versionNumbers(v string) [3]int {
	core, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(v), "v"), "-")

	var out [3]int

	for i, part := range strings.SplitN(core, ".", 3) {
		n, _ := strconv.Atoi(part)
		out[i] = n
	}

	return out
}

func prereleaseOf(v string) string {
	_, pre, _ := strings.Cut(strings.TrimSpace(v), "-")

	return pre
}
