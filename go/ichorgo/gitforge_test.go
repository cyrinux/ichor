package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const gitTestToken = "ghp_test_not_a_real_token"

func gitHostJSON(kind, baseURL string) string {
	out, _ := json.Marshal(map[string]string{"kind": kind, "baseURL": baseURL, "token": gitTestToken}) //nolint:errcheck

	return string(out)
}

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

// gitServer answers the paths of a fake forge and checks every request carries the token.
func gitServer(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gitTestToken && r.Header.Get("PRIVATE-TOKEN") != gitTestToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		h, ok := routes[r.URL.EscapedPath()]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)

			return
		}

		h(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func jsonAnswer(body string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}
}

func b64Content(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestGitHubTreeAndFile(t *testing.T) {
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v3/user": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-OAuth-Scopes", "read:org, public_repo")
			fmt.Fprint(w, `{"login":"octo"}`)
		},
		"/api/v3/repos/example-org/platform/contents/apps": jsonAnswer(`[
			{"type":"file","path":"apps/README.md","size":12},
			{"type":"dir","path":"apps/podinfo","size":0},
			{"type":"dir","path":"apps/billing","size":0}]`),
		"/api/v3/repos/example-org/platform/contents/apps/podinfo/deployment.yaml": jsonAnswer(
			`{"type":"file","path":"apps/podinfo/deployment.yaml","sha":"abc123","size":14,"encoding":"base64","content":"` + b64Content("kind: Deployment") + `"}`),
		"/api/v3/repos/example-org/platform/contents/big.bin": jsonAnswer(`{"type":"file","path":"big.bin","sha":"f00","size":5000000,"encoding":"none","content":""}`),
	})
	host, repo := gitHostJSON("github", srv.URL), "https://127.0.0.1/example-org/platform.git"

	out, err := GitForgeTree(host, repo, "main", "/apps/")
	if err != nil {
		t.Fatal(err)
	}

	var tree gitForgeTree
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatal(err)
	}

	if tree.Ref != "main" || len(tree.Entries) != 3 || tree.Entries[0].Path != "apps/billing" || tree.Entries[2].Type != "file" {
		t.Errorf("tree %+v", tree)
	}

	out, err = GitForgeFile(host, repo, "main", "apps/podinfo/deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var f gitForgeFile
	if err := json.Unmarshal([]byte(out), &f); err != nil || f.Content != "kind: Deployment" || f.SHA != "abc123" || f.Truncated {
		t.Errorf("file %+v %v", f, err)
	}

	out, err = GitForgeFile(host, repo, "main", "big.bin")
	if err != nil || !strings.Contains(out, `"truncated":true`) || !strings.Contains(out, `"content":""`) {
		t.Errorf("big file %s %v", out, err)
	}

	out, err = GitForgeCheck(host)
	if err != nil || !strings.Contains(out, `"login":"octo"`) || !strings.Contains(out, `"scopesOK":false`) || !strings.Contains(out, `"repo"`) {
		t.Errorf("check %s %v", out, err)
	}

	if _, err := GitForgeFile(host, repo, "v1.2.0", "missing.yaml"); err == nil || err.Error() != "missing.yaml is not in example-org/platform at v1.2.0" {
		t.Errorf("404: %v", err)
	}

	if _, err := GitForgeTree(host, repo, "main", "../etc"); err == nil {
		t.Error("a path out of the repository was read")
	}
}

func TestGitLabTreePagesAndPrivateToken(t *testing.T) {
	var bearerRefused atomic.Bool

	project := "/api/v4/projects/group%2Fsub%2Finfra"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A proxy that strips Authorization: only PRIVATE-TOKEN gets through.
		if r.Header.Get("PRIVATE-TOKEN") != gitTestToken {
			bearerRefused.Store(true)
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		switch r.URL.EscapedPath() {
		case project + "/repository/tree":
			if r.URL.Query().Get("path") != "clusters" || r.URL.Query().Get("ref") != "main" {
				t.Errorf("query %s", r.URL.RawQuery)
			}

			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("X-Next-Page", "2")
				fmt.Fprint(w, `[{"path":"clusters/prod","type":"tree"}]`)

				return
			}

			fmt.Fprint(w, `[{"path":"clusters/README.md","type":"blob"}]`)
		case project + "/repository/files/clusters%2Fprod%2Fkustomization.yaml":
			fmt.Fprintf(w, `{"file_path":"clusters/prod/kustomization.yaml","size":5,"encoding":"base64","content":"%s","last_commit_id":"c0ffee"}`, b64Content("kind:"))
		case project + "/repository/files/big.bin":
			if r.Method != http.MethodHead {
				t.Error("a file over 1 MiB was downloaded")
			}

			w.Header().Set("X-Gitlab-Size", "5000000")
			w.Header().Set("X-Gitlab-Last-Commit-Id", "b16")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	host, repo := gitHostJSON("gitlab", srv.URL), "ssh://git@127.0.0.1/group/sub/infra.git"

	if out, err := GitForgeFile(host, repo, "main", "big.bin"); err != nil || !strings.Contains(out, `"sha":"b16"`) || !strings.Contains(out, `"truncated":true`) {
		t.Errorf("big file %s %v", out, err)
	}

	out, err := GitForgeTree(host, repo, "main", "clusters")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"path":"clusters/prod","type":"dir"`) || !strings.Contains(out, `"path":"clusters/README.md","type":"file"`) {
		t.Errorf("tree %s", out)
	}

	if !bearerRefused.Load() {
		t.Error("Bearer was never tried first")
	}

	out, err = GitForgeFile(host, repo, "main", "clusters/prod/kustomization.yaml")
	if err != nil || !strings.Contains(out, `"content":"kind:"`) || !strings.Contains(out, `"sha":"c0ffee"`) {
		t.Errorf("file %s %v", out, err)
	}
}

// Gitea sends a file's whole content: one too large to read comes back truncated, described
// from its directory's listing.
func TestGiteaLargeFileIsTruncated(t *testing.T) {
	big := strings.Repeat("A", gitForgeMaxBody)

	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/repos/ops/fleet/contents/charts/big.tgz": jsonAnswer(`{"type":"file","path":"charts/big.tgz","sha":"5ha","size":3000000,"encoding":"base64","content":"` + big + `"}`),
		"/api/v1/repos/ops/fleet/contents/charts":         jsonAnswer(`[{"type":"file","path":"charts/big.tgz","sha":"5ha","size":3000000}]`),
		"/api/v1/repos/ops/fleet/contents/link":           jsonAnswer(`{"type":"symlink","path":"link","sha":"l1","size":4}`),
	})
	host, repo := gitHostJSON("gitea", srv.URL), "https://127.0.0.1/ops/fleet"

	out, err := GitForgeFile(host, repo, "main", "charts/big.tgz")
	if err != nil || !strings.Contains(out, `"sha":"5ha"`) || !strings.Contains(out, `"truncated":true`) || !strings.Contains(out, `"content":""`) {
		t.Errorf("big file %s %v", out, err)
	}

	if _, err := GitForgeFile(host, repo, "main", "link"); err == nil || err.Error() != "link is a symlink, not a file" {
		t.Errorf("symlink: %v", err)
	}
}

// The token is for its own host: a repository on another host, or one whose path climbs, is
// refused before any request.
func TestGitForgeRefusesOtherRepos(t *testing.T) {
	host := gitHostJSON("gitlab", "https://gitlab.example.org")

	if _, err := GitForgeTree(host, "https://github.com/example-org/platform", "main", ""); err == nil || !strings.Contains(err.Error(), "is on github.com, not on the Git host gitlab.example.org") {
		t.Errorf("other host: %v", err)
	}

	if _, err := GitForgeTree(host, "https://gitlab.example.org/example-org/..", "main", ""); err == nil {
		t.Error("a repository path with .. was accepted")
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

func TestGiteaTree(t *testing.T) {
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/repos/ops/fleet/contents": jsonAnswer(`[{"type":"dir","path":"apps","size":0},{"type":"file","path":"README.md","size":3}]`),
	})

	out, err := GitForgeTree(gitHostJSON("gitea", srv.URL), "https://127.0.0.1/ops/fleet", "main", "")
	if err != nil || !strings.Contains(out, `{"path":"apps","type":"dir","size":0}`) {
		t.Errorf("tree %s %v", out, err)
	}
}

// A refused token is said so, and the token never appears in an error.
func TestGitForgeRefusedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"message":"bad credentials %s"}`, gitTestToken)
	}))
	t.Cleanup(srv.Close)

	_, err := GitForgeCheck(gitHostJSON("github", srv.URL))
	if err == nil || !strings.Contains(err.Error(), "the token for 127.0.0.1 was refused") || strings.Contains(err.Error(), gitTestToken) {
		t.Errorf("refused: %v", err)
	}
}

// A redirect to another host is never followed: the token stays where it was sent.
func TestGitForgeRefusesRedirects(t *testing.T) {
	var hits atomic.Int32

	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(other.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	_, err := GitForgeTree(gitHostJSON("gitea", srv.URL), "https://127.0.0.1/ops/fleet", "main", "")
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("redirect: %v", err)
	}

	if hits.Load() != 0 {
		t.Error("the redirect was followed")
	}
}

func TestGitForgeRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1900000000")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	if _, err := GitForgeCheck(gitHostJSON("github", srv.URL)); err == nil || !strings.Contains(err.Error(), "rate limited by 127.0.0.1 until") {
		t.Errorf("rate limit: %v", err)
	}
}

// The demo answers from fixed data, without network.
func TestGitForgeDemo(t *testing.T) {
	host := `{"kind":"demo"}`

	out, err := GitForgeTree(host, "", "main", "apps/podinfo")
	if err != nil || strings.Count(out, `"type":"file"`) != 3 || !strings.Contains(out, "apps/podinfo/deployment.yaml") {
		t.Errorf("tree %s %v", out, err)
	}

	if out, err := GitForgeTree(host, "", "main", ""); err != nil || !strings.Contains(out, `{"path":"apps","type":"dir","size":0}`) {
		t.Errorf("root %s %v", out, err)
	}

	if out, err := GitForgeFile(host, "", "main", "apps/podinfo/hpa.yaml"); err != nil || !strings.Contains(out, "HorizontalPodAutoscaler") {
		t.Errorf("file %s %v", out, err)
	}

	if out, err := GitForgeCheck(host); err != nil || !strings.Contains(out, `"scopesOK":true`) {
		t.Errorf("check %s %v", out, err)
	}
}
