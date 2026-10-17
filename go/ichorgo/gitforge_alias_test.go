package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func gitHostJSONAliases(kind, baseURL string, aliases ...string) string {
	out, _ := json.Marshal(map[string]any{"kind": kind, "baseURL": baseURL, "token": gitTestToken, "aliases": aliases}) //nolint:errcheck

	return string(out)
}

func TestNormalizeGitHostAlias(t *testing.T) {
	for in, want := range map[string]string{
		"git.example.org":                            "git.example.org",
		"  SSH.Git.Example.org.  ":                   "ssh.git.example.org",
		"https://user:pass@Git.Example.org/org/repo": "git.example.org",
		"ssh://git@ssh.git.example.org:2222/org/x":   "ssh.git.example.org:2222",
		"git@ssh.git.example.org:org/repo.git":       "ssh.git.example.org",
		"gitea-http.gitea.svc.cluster.local:3000":    "gitea-http.gitea.svc.cluster.local:3000",
		"[::1]:3000":                                 "[::1]:3000",
		"":                                           "",
		"   ":                                        "",
		"https:///nohost":                            "",
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

func TestGitForgeAliasErrorNamesTheAddress(t *testing.T) {
	host := gitHostJSONAliases("gitea", "https://git.example.org")

	_, err := GitForgeTree(host, "http://gitea-http.gitea.svc:3000/example-org/platform.git", "main", "")
	want := "example-org/platform is on gitea-http.gitea.svc:3000, not on the Git host git.example.org: if it is the same server, add gitea-http.gitea.svc:3000 to the host's other names in Settings → Git hosts"

	if err == nil || err.Error() != want {
		t.Errorf("error %v, want %q", err, want)
	}
}

// A repository on an alias is read from the base server only: the alias server, which
// would get the token otherwise, sees no request.
func TestGitForgeAliasRequestsGoToBaseOnly(t *testing.T) {
	var aliasHits atomic.Int32

	alias := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aliasHits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(alias.Close)

	var baseHits atomic.Int32

	base := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v3/user": func(w http.ResponseWriter, _ *http.Request) {
			baseHits.Add(1)
			w.Header().Set("X-OAuth-Scopes", "repo")
			fmt.Fprint(w, `{"login":"octo"}`)
		},
		"/api/v3/repos/example-org/platform/contents/apps": func(w http.ResponseWriter, r *http.Request) {
			baseHits.Add(1)
			jsonAnswer(`[{"type":"file","path":"apps/README.md","size":12}]`)(w, r)
		},
		"/api/v3/repos/example-org/platform/contents/apps/README.md": func(w http.ResponseWriter, r *http.Request) {
			baseHits.Add(1)
			jsonAnswer(`{"type":"file","path":"apps/README.md","sha":"abc","size":5,"encoding":"base64","content":"` + b64Content("hello") + `"}`)(w, r)
		},
	})

	// Base on localhost, alias on 127.0.0.1: both loopback, different names and ports.
	baseURL := strings.Replace(base.URL, "127.0.0.1", "localhost", 1)
	aliasAddr := strings.TrimPrefix(alias.URL, "http://")
	host := gitHostJSONAliases("github", baseURL, aliasAddr)
	repo := alias.URL + "/example-org/platform.git"

	if _, err := GitForgeCheck(host); err != nil {
		t.Fatal(err)
	}

	if _, err := GitForgeTree(host, repo, "main", "apps"); err != nil {
		t.Fatal(err)
	}

	if _, err := GitForgeFile(host, "git@"+strings.Split(aliasAddr, ":")[0]+":example-org/platform.git", "main", "apps/README.md"); err != nil {
		t.Fatal(err)
	}

	if aliasHits.Load() != 0 || baseHits.Load() != 3 {
		t.Errorf("alias got %d requests, base %d", aliasHits.Load(), baseHits.Load())
	}
}
