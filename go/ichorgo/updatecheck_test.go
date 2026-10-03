package ichorgo

import (
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.14.10", "v1.14.9", 1},
		{"v1.14.1", "v1.14.1", 0},
		{"v1.13.4", "v1.14.0", -1},
		{"v1.15.0-alpha.1", "v1.15.0", -1},
		{"v1.15.0-beta.0", "v1.15.0-alpha.2", 1},
		{"1.14.2", "v1.14.2", 0},
	}

	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCheckUpdate(t *testing.T) {
	releases := []talosRelease{
		{Version: "v1.15.0-alpha.1", Prerelease: true},
		{Version: "v1.14.10", Date: "2026-09-20T00:00:00Z"},
		{Version: "v1.14.9"},
		{Version: "v1.13.6"},
	}

	got := checkUpdate(releases, "v1.14.9, v1.14.9,,junk,v1.13.4,v1.14.10")
	if got.Latest != "v1.14.10" || !got.Newer || got.Outdated != 3 || got.Oldest != "v1.13.4" {
		t.Fatalf("unexpected result: %+v", got)
	}

	if got.Notes != "https://github.com/siderolabs/talos/releases/tag/v1.14.10" || got.LatestDate == "" {
		t.Fatalf("missing notes or date: %+v", got)
	}

	if upToDate := checkUpdate(releases, "v1.14.10,v1.15.0-alpha.1"); upToDate.Newer || upToDate.Outdated != 0 {
		t.Fatalf("up-to-date nodes reported outdated: %+v", upToDate)
	}

	if none := checkUpdate(nil, "v1.14.1"); none.Newer || none.Latest != "" {
		t.Fatalf("no releases should report nothing: %+v", none)
	}
}

func TestParseReleasesSortsByVersion(t *testing.T) {
	body := []byte(`[
		{"tag_name":"v1.13.11","published_at":"2026-09-30T00:00:00Z"},
		{"tag_name":"v1.14.2","published_at":"2026-09-29T00:00:00Z"},
		{"tag_name":"v1.12.12","published_at":"2026-09-28T00:00:00Z"},
		{"tag_name":"v1.15.0-alpha.1","prerelease":true,"published_at":"2026-09-27T00:00:00Z"},
		{"tag_name":"v1.14.10","published_at":"2026-09-26T00:00:00Z","draft":true}
	]`)

	got, err := parseReleases(body)
	if err != nil {
		t.Fatal(err)
	}

	var versions []string
	for _, r := range got {
		versions = append(versions, r.Version)
	}

	if want := "v1.15.0-alpha.1,v1.14.2,v1.13.11,v1.12.12"; strings.Join(versions, ",") != want {
		t.Fatalf("got %v, want %s", versions, want)
	}
}
