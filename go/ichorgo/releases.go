package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const (
	talosReleasesURL = "https://api.github.com/repos/siderolabs/talos/releases?per_page=25"
	releasesTimeout  = 10 * time.Second
	maxReleases      = 20
)

type talosRelease struct {
	Version    string `json:"version"`
	Date       string `json:"date"` // RFC 3339
	Prerelease bool   `json:"prerelease"`
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

// TalosReleases lists the latest Talos releases from GitHub (no authentication, 10 s
// timeout), newest first, at most 20: [{"version","date","prerelease"}]. They are
// suggestions for UpgradeImage; any version can be typed.
func TalosReleases() (out string, err error) {
	defer maskResult(&out, &err)

	releases, err := fetchReleases()
	if err != nil {
		return "", err
	}

	return toJSON(releases)
}

// fetchReleases downloads and parses the release list from GitHub.
func fetchReleases() ([]talosRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), releasesTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, talosReleasesURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := newHTTPClient(httpClientOpts{followRedirects: true}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Talos releases: %s", friendlyError(err))
	}

	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch Talos releases: GitHub answered %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("fetch Talos releases: %w", err)
	}

	return parseReleases(body)
}

func parseReleases(body []byte) ([]talosRelease, error) {
	var in []githubRelease
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("parse Talos releases: %w", err)
	}

	sort.SliceStable(in, func(i, j int) bool { return in[i].PublishedAt.After(in[j].PublishedAt) })

	out := []talosRelease{}

	for _, r := range in {
		v, ok := normalizeTalosVersion(r.TagName)
		if r.Draft || !ok {
			continue
		}

		out = append(out, talosRelease{Version: v, Date: r.PublishedAt.UTC().Format(time.RFC3339), Prerelease: r.Prerelease})

	}

	// Newest version first, not newest release: patch releases of older lines are published
	// after newer minor versions.
	sort.SliceStable(out, func(i, j int) bool { return compareVersions(out[i].Version, out[j].Version) > 0 })

	if len(out) > maxReleases {
		out = out[:maxReleases]
	}

	return out, nil
}
