package upstream

import "testing"

func TestUpstreamPolicy(t *testing.T) {
	cases := []struct {
		name, current string
		tags          []string
		want          Candidate
	}{
		{
			name: "patch, minor and major", current: "v1.19.0",
			tags: []string{"v1.18.9", "v1.19.1", "v1.19.2", "v1.20.0", "v1.20.1", "v2.0.0", "v2.0.0-rc.1", "1.21.0", "latest"},
			want: Candidate{Current: "v1.19.0", Patch: "v1.19.2", Minor: "v1.20.1", Major: "v2.0.0", Shape: "v{core}"},
		},
		{
			name: "suffix must match", current: "1.19.0-alpine",
			tags: []string{"1.19.2", "1.19.1-alpine", "1.20.0-alpine-slim", "1.21.0-alpine3.20", "1.20.0-alpine"},
			want: Candidate{Current: "1.19.0-alpine", Patch: "1.19.1-alpine", Minor: "1.20.0-alpine", Shape: "{core}-alpine"},
		},
		{
			name: "two parts stay two parts", current: "1.19",
			tags: []string{"1.19.5", "1.20", "1.21", "2.0.1"},
			want: Candidate{Current: "1.19", Minor: "1.21", Shape: "{core}"},
		},
		{
			name: "not a version", current: "latest",
			tags: []string{"1.0.0"},
			want: Candidate{Current: "latest", Reason: ReasonNotVersion},
		},
		{name: "branch", current: "main", want: Candidate{Current: "main", Reason: ReasonNotVersion}},
		{name: "commit", current: "sha-abc123", want: Candidate{Current: "sha-abc123", Reason: ReasonNotVersion}},
		{name: "dashed date", current: "2024-10-01", want: Candidate{Current: "2024-10-01", Reason: ReasonNotVersion}},
		{name: "no tag", current: "", want: Candidate{Reason: ReasonNotVersion}},
		{
			name: "dotted date is a version", current: "2024.10.1",
			tags: []string{"2024.10.3", "2024.11.0", "2025.1.0"},
			want: Candidate{Current: "2024.10.1", Patch: "2024.10.3", Minor: "2024.11.0", Major: "2025.1.0", Shape: "{core}"},
		},
		{
			name: "prerelease current", current: "v1.20.0-rc.1",
			tags: []string{"v1.20.0-rc.2", "v1.20.0-beta.1", "v1.19.9"},
			want: Candidate{Current: "v1.20.0-rc.1", Patch: "v1.20.0-rc.2", Prerelease: true, Shape: "v{core}"},
		},
		{
			name: "prerelease current takes the release", current: "v1.20.0-rc.1",
			tags: []string{"v1.20.0-rc.2", "v1.20.0", "v1.20.0-rc.10"},
			want: Candidate{Current: "v1.20.0-rc.1", Patch: "v1.20.0", Prerelease: true, Shape: "v{core}"},
		},
		{
			name: "release current skips prereleases", current: "6.7.0",
			tags: []string{"6.7.1-rc.1", "6.8.0-beta", "6.7.1"},
			want: Candidate{Current: "6.7.0", Patch: "6.7.1", Shape: "{core}"},
		},
		{
			name: "prerelease keeps its suffix", current: "1.2.0-rc.1-alpine",
			tags: []string{"1.2.0-alpine", "1.2.0-rc.2", "1.2.0-rc.2-alpine"},
			want: Candidate{Current: "1.2.0-rc.1-alpine", Patch: "1.2.0-alpine", Prerelease: true, Shape: "{core}-alpine"},
		},
		{
			name: "release prefix", current: "release-1.4.2",
			tags: []string{"release-1.4.3", "v1.5.0", "1.5.0"},
			want: Candidate{Current: "release-1.4.2", Patch: "release-1.4.3", Shape: "release-{core}"},
		},
		{
			name: "revision counter", current: "1.27.0-debian-12-r5",
			tags: []string{"1.27.0-debian-12-r7", "1.27.1-debian-12-r0", "1.28.0-debian-11-r2", "1.27.1"},
			want: Candidate{Current: "1.27.0-debian-12-r5", Patch: "1.27.1-debian-12-r0", Shape: "{core}-debian-12-r{n}"},
		},
		{
			name: "nothing newer", current: "v2.0.0",
			tags: []string{"v1.0.0", "v2.0.0"},
			want: Candidate{Current: "v2.0.0", Shape: "v{core}"},
		},
		{
			name: "major only", current: "1.2.3",
			tags: []string{"2.0.0", "3.1.0"},
			want: Candidate{Current: "1.2.3", Major: "3.1.0", Shape: "{core}"},
		},
		{
			name: "minor repeats no patch", current: "1.2.3",
			tags: []string{"1.3.0"},
			want: Candidate{Current: "1.2.3", Minor: "1.3.0", Shape: "{core}"},
		},
		{
			name: "build metadata", current: "1.2.3+1",
			tags: []string{"1.2.4+7"},
			want: Candidate{Current: "1.2.3+1", Patch: "1.2.4+7", Shape: "{core}"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.current, tc.tags); got != tc.want {
				t.Errorf("classify(%q)\n got %+v\nwant %+v", tc.current, got, tc.want)
			}
		})
	}
}

func TestUpstreamParseVersion(t *testing.T) {
	cases := []struct {
		tag, prefix, pre, suffix string
		parts, revision          int
	}{
		{tag: "v1.20.0-rc.1", prefix: "v", pre: "rc.1", parts: 3, revision: -1},
		{tag: "1.27.0-alpine3.20", suffix: "-alpine3.20", parts: 3, revision: -1},
		{tag: "16.4-bookworm", suffix: "-bookworm", parts: 2, revision: -1},
		{tag: "3.12.1-rootless", suffix: "-rootless", parts: 3, revision: -1},
		{tag: "1.2.3-beta2", pre: "beta2", parts: 3, revision: -1},
		{tag: "1.2.3-prerelease", suffix: "-prerelease", parts: 3, revision: -1},
		{tag: "1.2.3.4", parts: 4, revision: -1},
		{tag: "1.2.3-2", parts: 3, revision: 2},
	}

	for _, tc := range cases {
		v, ok := parseVersion(tc.tag)
		if !ok || v.prefix != tc.prefix || v.pre != tc.pre || v.suffix != tc.suffix || len(v.core) != tc.parts || v.revision != tc.revision {
			t.Errorf("parseVersion(%q) = %+v, %v", tc.tag, v, ok)
		}
	}
}
