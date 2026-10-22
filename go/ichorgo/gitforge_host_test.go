package ichorgo

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGitForgeKindOf(t *testing.T) {
	for host, want := range map[string]string{
		"github.com": gitForgeGitHub, "gitlab.com": gitForgeGitLab, "gitlab.example.org": gitForgeGitLab,
		"gitea.home.example": gitForgeGitea, "forgejo.example.org": gitForgeGitea, "codeberg.org": gitForgeGitea,
	} {
		if got, err := gitForgeKindOf(host); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", host, got, err, want)
		}
	}

	if _, err := gitForgeKindOf("bitbucket.org"); err == nil || !strings.Contains(err.Error(), "unknown Git host bitbucket.org") {
		t.Errorf("bitbucket: %v", err)
	}
}

func TestGitForgeRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/example-org/platform.git":        "github.com|example-org|platform",
		"git@github.com:example-org/platform.git":            "github.com|example-org|platform",
		"ssh://git@gitlab.example.org/group/sub/infra.git":   "gitlab.example.org|group/sub|infra",
		"https://codeberg.org/example-org/platform/":         "codeberg.org|example-org|platform",
		"https://user:pass@gitea.home.example/ops/fleet.git": "gitea.home.example|ops|fleet",
	} {
		host, owner, name, err := gitForgeRepo(in)
		if got := host + "|" + owner + "|" + name; err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", in, got, err, want)
		}
	}

	for _, bad := range []string{"oci://registry/chart", "https://github.com/onlyowner", "not a url"} {
		if _, _, _, err := gitForgeRepo(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestParseGitForgeHostRefusesPlainHTTP(t *testing.T) {
	if _, err := parseGitForgeHost(gitHostJSON("github", "http://git.home.example")); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain http: %v", err)
	}

	if h, err := parseGitForgeHost(gitHostJSON("gitea", "http://127.0.0.1:3000/")); err != nil || h.BaseURL != "http://127.0.0.1:3000" {
		t.Errorf("loopback: %+v %v", h, err)
	}

	if _, err := parseGitForgeHost(gitHostJSON("auto", "https://bitbucket.org")); err == nil {
		t.Error("an unknown host was accepted")
	}
}

func TestParseGitForgeHostAPIAddresses(t *testing.T) {
	for in, want := range map[string]string{
		`{"kind":"auto","baseURL":"https://api.github.com"}`:               "github|github.com|https://github.com",
		`{"kind":"github","baseURL":"https://ghe.example.org/api/v3/"}`:    "github|ghe.example.org|https://ghe.example.org",
		`{"kind":"auto","baseURL":"https://gitlab.example.org/api/v4"}`:    "gitlab|gitlab.example.org|https://gitlab.example.org",
		`{"kind":"auto","baseURL":"https://gitea.example.org/git/api/v1"}`: "gitea|gitea.example.org|https://gitea.example.org/git",
	} {
		h, err := parseGitForgeHost(in)
		if got := h.Kind + "|" + h.host + "|" + h.BaseURL; err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", in, got, err, want)
		}
	}

	h, _ := parseGitForgeHost(`{"kind":"auto","baseURL":"https://api.github.com"}`) //nolint:errcheck
	if api := newGitForge(h).(gitHubForge).c.api; api != "https://api.github.com" {
		t.Errorf("github.com API %s", api)
	}
}

func TestRateLimitResetPrefersRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Reset", "1900000000")
	h.Set("Retry-After", "60")

	if until := time.Until(rateLimitReset(h)); until <= 0 || until > 2*time.Minute {
		t.Errorf("reset in %s, want about a minute", until)
	}
}

func TestNormalizeGitHostAlias(t *testing.T) {
	for in, want := range map[string]string{
		"git.example.org":                            "git.example.org",
		"  SSH.Git.Example.org.  ":                   "ssh.git.example.org",
		"https://user:pass@Git.Example.org/org/repo": "git.example.org",
		"ssh://git@ssh.git.example.org:2222/org/x":   "ssh.git.example.org:2222",
		"git@ssh.git.example.org:org/repo.git":       "ssh.git.example.org",
		"gitea-http.gitea.svc.cluster.local:3000":    "gitea-http.gitea.svc.cluster.local:3000",
		"[::1]:3000":      "[::1]:3000",
		"":                "",
		"   ":             "",
		"https:///nohost": "",
	} {
		if got := normalizeGitHostAlias(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeGitHostAliasesCapsAndDedupes(t *testing.T) {
	raw := []string{"", "A.example.org", "a.example.org.", " "}
	for i := range 40 {
		raw = append(raw, fmt.Sprintf("h%d.example.org", i))
	}

	got := normalizeGitHostAliases(raw)
	if len(got) != gitForgeMaxAliases || got[0] != "a.example.org" || slices.Contains(got, "") {
		t.Errorf("aliases %v", got)
	}

	h, err := parseGitForgeHost(gitHostJSONAliases("gitea", "https://git.example.org", "SSH.git.example.org", ""))
	if err != nil || !slices.Equal(h.aliases, []string{"ssh.git.example.org"}) {
		t.Errorf("parsed %+v %v", h.aliases, err)
	}
}

// A repository on the base host, a well-known name of it or an alias is read; any other is
// refused before a request.
func TestGitForgeArgsAliases(t *testing.T) {
	host := gitHostJSONAliases("gitea", "https://git.example.org",
		"ssh.git.example.org", "gitea-http.gitea.svc.cluster.local:3000", "git.lan.example:2222")

	for name, tc := range map[string]struct {
		repoURL string
		ok      bool
	}{
		"base host":              {"https://git.example.org/example-org/platform.git", true},
		"base host scp":          {"git@git.example.org:example-org/platform.git", true},
		"alias https":            {"https://ssh.git.example.org/example-org/platform", true},
		"alias ssh scp-like":     {"git@ssh.git.example.org:example-org/platform.git", true},
		"alias ssh url":          {"ssh://git@ssh.git.example.org/example-org/platform.git", true},
		"alias with port":        {"http://gitea-http.gitea.svc.cluster.local:3000/example-org/platform.git", true},
		"alias case and dot":     {"https://SSH.git.example.org./example-org/platform", true},
		"alias port, scp-like":   {"git@git.lan.example:example-org/platform.git", true},
		"alias other port":       {"http://gitea-http.gitea.svc.cluster.local:8080/example-org/platform.git", false},
		"alias port missing":     {"http://gitea-http.gitea.svc.cluster.local/example-org/platform.git", false},
		"port added to alias":    {"ssh://git@ssh.git.example.org:2222/example-org/platform.git", false},
		"other host":             {"https://github.com/example-org/platform", false},
		"alias as subdomain":     {"https://evil.ssh.git.example.org/example-org/platform", false},
		"alias suffix lookalike": {"https://ssh.git.example.org.evil.example/example-org/platform", false},
	} {
		t.Run(name, func(t *testing.T) {
			h, repo, _, _, err := gitForgeArgs(host, tc.repoURL, "main", "")
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "not on the Git host git.example.org") || !strings.Contains(err.Error(), "other names in Settings → Git hosts") {
					t.Errorf("accepted or wrong error: %v", err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if repo.host != "git.example.org" || repo.full() != "example-org/platform" || h.BaseURL != "https://git.example.org" {
				t.Errorf("repo %+v base %s", repo, h.BaseURL)
			}
		})
	}
}

// A refusal is "the file changed" by the forge's message, not by words in the host name.
func TestStaleFile(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"github stale":         {gitForgeStatusError{code: 409, message: "is at abc but expected def", err: errors.New("x")}, true},
		"empty repository":     {gitForgeStatusError{code: 409, message: "Git Repository is empty.", err: errors.New("x")}, false},
		"gitea sha":            {gitForgeStatusError{code: 422, message: "sha does not match", err: errors.New("x")}, true},
		"validation, sha host": {gitForgeStatusError{code: 422, message: "Validation Failed", err: errors.New("git.sharecode.example answered 422: Validation Failed")}, false},
		"gitlab changed":       {gitForgeStatusError{code: 400, message: "You are attempting to update a file that has changed since you started editing it.", err: errors.New("x")}, true},
		"gitlab other":         {gitForgeStatusError{code: 400, message: "Invalid reference name", err: errors.New("changed.example answered 400")}, false},
		"not a status":         {errors.New("sha changed already exists"), false},
	} {
		if got := staleFile(tc.err); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestGitSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Bump podinfo to 6.7.1":   "bump-podinfo-to-6-7-1",
		"  --Fix: HPA max!!  ":    "fix-hpa-max",
		"":                        "change",
		strings.Repeat("ab ", 30): strings.Repeat("ab-", 13) + "a",
	} {
		if got := gitSlug(in); got != want {
			t.Errorf("gitSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
