package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cyrinux/ichor/go/gitforge"
)

// proposeContent is long enough that a wrapping base64 encoder would break its lines.
var proposeContent = strings.Repeat("image: ghcr.io/example-org/podinfo:6.7.1\n", 8)

// proposeCall is one request a fake forge received, its JSON body decoded.
type proposeCall struct {
	route string // "METHOD /escaped/path"
	body  map[string]any
}

// proposeAnswer is a fake forge's answer: a status and a JSON body.
type proposeAnswer struct {
	code int
	body string
}

// proposeServer answers each route with its answers in turn (the last one repeats; 404
// for a route not listed) and records every call.
func proposeServer(t *testing.T, answers map[string][]proposeAnswer) (*httptest.Server, func() []proposeCall) {
	t.Helper()

	var (
		mu    sync.Mutex
		calls []proposeCall
		seen  = map[string]int{}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+gitTestToken {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		route := r.Method + " " + r.URL.EscapedPath()

		var body map[string]any
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 { //nolint:errcheck
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("%s: body %q is not JSON", route, raw)
			}

			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("%s: Content-Type %q", route, r.Header.Get("Content-Type"))
			}
		}

		mu.Lock()
		calls = append(calls, proposeCall{route: route, body: body})
		list, n := answers[route], seen[route]
		seen[route]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		if len(list) == 0 {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)

			return
		}

		a := list[min(n, len(list)-1)]
		w.WriteHeader(a.code)
		fmt.Fprint(w, a.body)
	}))
	t.Cleanup(srv.Close)

	return srv, func() []proposeCall {
		mu.Lock()
		defer mu.Unlock()

		return slices.Clone(calls)
	}
}

// fixedBranchSuffix makes the generated branch suffixes 0001, 0002...
func fixedBranchSuffix(t *testing.T) {
	t.Helper()

	n, prev := 0, gitforge.BranchSuffix
	gitforge.BranchSuffix = func() string {
		n++

		return fmt.Sprintf("%04d", n)
	}
	t.Cleanup(func() { gitforge.BranchSuffix = prev })
}

func proposeOptions(t *testing.T, repoURL string, change func(map[string]any)) string {
	t.Helper()

	opts := map[string]any{
		"repoURL": repoURL, "baseRef": "main", "path": "apps/podinfo/deployment.yaml", "content": proposeContent,
		"fileSha": "f11e5ha", "message": "Bump podinfo to 6.7.1", "title": "Bump podinfo to 6.7.1",
		"body": "Proposed from Ichor on lab for Argo CD Application argocd/podinfo",
	}
	if change != nil {
		change(opts)
	}

	out, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}

	return string(out)
}

type proposeCase struct {
	name      string
	change    func(map[string]any)
	answers   map[string][]proposeAnswer
	wantCalls []string
	wantErr   string
	check     func(t *testing.T, calls []proposeCall, out gitforge.ProposeResult)
}

func runProposeCases(t *testing.T, kind, repoURL string, cases []proposeCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixedBranchSuffix(t)

			srv, calls := proposeServer(t, tc.answers)

			out, err := GitForgePropose("", "lab", gitHostJSON(kind, srv.URL), proposeOptions(t, repoURL, tc.change))

			var got []string
			for _, c := range calls() {
				got = append(got, c.route)
			}

			if !slices.Equal(got, tc.wantCalls) {
				t.Errorf("calls\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.wantCalls, "\n"))
			}

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %v, want %q", err, tc.wantErr)
				}

				if strings.Contains(err.Error(), gitTestToken) {
					t.Errorf("the token is in the error: %v", err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var res gitforge.ProposeResult
			if err := json.Unmarshal([]byte(out), &res); err != nil {
				t.Fatalf("%v: %s", err, out)
			}

			if res.Forge != kind {
				t.Errorf("forge %q, want %q", res.Forge, kind)
			}

			if tc.check != nil {
				tc.check(t, calls(), res)
			}
		})
	}
}

// bodyOf is the body of the first call to route.
func bodyOf(t *testing.T, calls []proposeCall, route string) map[string]any {
	t.Helper()

	for _, c := range calls {
		if c.route == route {
			return c.body
		}
	}

	t.Fatalf("no call to %s", route)

	return nil
}

func wantFields(t *testing.T, route string, body, want map[string]any) {
	t.Helper()

	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s: %s = %#v, want %#v", route, k, body[k], v)
		}
	}
}

// wantBase64 checks a contents body carries proposeContent in base64 on a single line.
func wantBase64(t *testing.T, body map[string]any) {
	t.Helper()

	enc, _ := body["content"].(string)
	if strings.ContainsAny(enc, "\r\n") {
		t.Errorf("base64 content has line breaks: %q", enc)
	}

	if raw, err := base64.StdEncoding.DecodeString(enc); err != nil || string(raw) != proposeContent {
		t.Errorf("content %q %v", raw, err)
	}
}

func TestGitForgeProposeGitHub(t *testing.T) {
	const (
		repo   = "/api/v3/repos/example-org/platform"
		ref    = "GET " + repo + "/git/ref/heads/main"
		refs   = "POST " + repo + "/git/refs"
		put    = "PUT " + repo + "/contents/apps/podinfo/deployment.yaml"
		pulls  = "POST " + repo + "/pulls"
		branch = "ichor/bump-podinfo-to-6-7-1-0001"
		del    = "DELETE " + repo + "/git/refs/heads/ichor/bump-podinfo-to-6-7-1-0001"
	)

	ok := map[string][]proposeAnswer{
		ref:   {{200, `{"ref":"refs/heads/main","object":{"sha":"ba5e","type":"commit"}}`}},
		refs:  {{201, `{"ref":"refs/heads/` + branch + `"}`}},
		put:   {{200, `{"content":{"sha":"n3w"},"commit":{"sha":"c0mm17"}}`}},
		pulls: {{201, `{"html_url":"https://git.example.org/example-org/platform/pull/42","number":42}`}},
		del:   {{204, ``}},
	}

	with := func(route string, answers ...proposeAnswer) map[string][]proposeAnswer {
		out := map[string][]proposeAnswer{}
		for k, v := range ok {
			out[k] = v
		}

		out[route] = answers

		return out
	}

	runProposeCases(t, "github", "https://127.0.0.1/example-org/platform.git", []proposeCase{
		{
			name: "update", answers: ok, wantCalls: []string{ref, refs, put, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitforge.ProposeResult) {
				if res.URL != "https://git.example.org/example-org/platform/pull/42" || res.Number != 42 || res.Branch != branch || res.Commit != "c0mm17" {
					t.Errorf("result %+v", res)
				}

				wantFields(t, refs, bodyOf(t, calls, refs), map[string]any{"ref": "refs/heads/" + branch, "sha": "ba5e"})
				wantFields(t, put, bodyOf(t, calls, put), map[string]any{"message": "Bump podinfo to 6.7.1", "sha": "f11e5ha", "branch": branch})
				wantBase64(t, bodyOf(t, calls, put))
				wantFields(t, pulls, bodyOf(t, calls, pulls), map[string]any{
					"head": branch, "base": "main", "title": "Bump podinfo to 6.7.1", "draft": false,
					"body": "Proposed from Ichor on lab for Argo CD Application argocd/podinfo",
				})
			},
		},
		{
			name: "new file without sha", change: func(o map[string]any) { o["fileSha"] = ""; o["draft"] = true }, answers: ok,
			wantCalls: []string{ref, refs, put, pulls},
			check: func(t *testing.T, calls []proposeCall, _ gitforge.ProposeResult) {
				if _, has := bodyOf(t, calls, put)["sha"]; has {
					t.Error("a new file was sent with a sha")
				}

				wantFields(t, pulls, bodyOf(t, calls, pulls), map[string]any{"draft": true})
			},
		},
		{
			name: "stale file sha", answers: with(put, proposeAnswer{409, `{"message":"apps/podinfo/deployment.yaml does not match f11e5ha"}`}),
			wantCalls: []string{ref, refs, put, del},
			wantErr:   "the file changed on main since you opened it: reload and try again",
		},
		{
			name:      "branch exists",
			answers:   with(refs, proposeAnswer{422, `{"message":"Reference already exists"}`}, proposeAnswer{201, `{}`}),
			wantCalls: []string{ref, refs, refs, put, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitforge.ProposeResult) {
				if res.Branch != "ichor/bump-podinfo-to-6-7-1-0002" {
					t.Errorf("branch %q", res.Branch)
				}

				if b := calls[2].body["ref"]; b != "refs/heads/ichor/bump-podinfo-to-6-7-1-0002" {
					t.Errorf("retry ref %v", b)
				}
			},
		},
		{
			name:      "pull request refused",
			answers:   with(pulls, proposeAnswer{422, `{"message":"Validation Failed"}`}),
			wantCalls: []string{ref, refs, put, pulls, del},
			wantErr:   "Validation Failed",
		},
		{
			name:      "token cannot see the repository",
			answers:   with(ref, proposeAnswer{404, `{"message":"Not Found"}`}),
			wantCalls: []string{ref},
			wantErr:   "the token cannot see example-org/platform",
		},
		{
			name:      "given branch",
			change:    func(o map[string]any) { o["branch"] = "ichor/podinfo-6-7-1" },
			answers:   ok,
			wantCalls: []string{ref, refs, put, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitforge.ProposeResult) {
				if res.Branch != "ichor/podinfo-6-7-1" || calls[1].body["ref"] != "refs/heads/ichor/podinfo-6-7-1" {
					t.Errorf("branch %q, ref %v", res.Branch, calls[1].body["ref"])
				}
			},
		},
	})
}

func TestGitForgeProposeGitLab(t *testing.T) {
	const (
		project = "/api/v4/projects/group%2Fsub%2Fplatform"
		create  = "POST " + project + "/repository/branches"
		put     = "PUT " + project + "/repository/files/apps%2Fpodinfo%2Fdeployment.yaml"
		post    = "POST " + project + "/repository/files/apps%2Fpodinfo%2Fdeployment.yaml"
		mrs     = "POST " + project + "/merge_requests"
		branch  = "ichor/bump-podinfo-to-6-7-1-0001"
		del     = "DELETE " + project + "/repository/branches/ichor%2Fbump-podinfo-to-6-7-1-0001"
	)

	ok := map[string][]proposeAnswer{
		create: {{201, `{"name":"` + branch + `"}`}},
		put:    {{200, `{"file_path":"apps/podinfo/deployment.yaml","branch":"` + branch + `"}`}},
		post:   {{201, `{"file_path":"apps/podinfo/deployment.yaml","branch":"` + branch + `"}`}},
		mrs:    {{201, `{"web_url":"https://git.example.org/group/sub/platform/-/merge_requests/7","iid":7,"sha":"m3r93"}`}},
		del:    {{204, ``}},
	}

	with := func(route string, answers ...proposeAnswer) map[string][]proposeAnswer {
		out := map[string][]proposeAnswer{}
		for k, v := range ok {
			out[k] = v
		}

		out[route] = answers

		return out
	}

	runProposeCases(t, "gitlab", "https://127.0.0.1/group/sub/platform.git", []proposeCase{
		{
			name: "update", change: func(o map[string]any) { o["draft"] = true }, answers: ok, wantCalls: []string{create, put, mrs},
			check: func(t *testing.T, calls []proposeCall, res gitforge.ProposeResult) {
				if res.URL != "https://git.example.org/group/sub/platform/-/merge_requests/7" || res.Number != 7 || res.Branch != branch || res.Commit != "m3r93" {
					t.Errorf("result %+v", res)
				}

				wantFields(t, create, bodyOf(t, calls, create), map[string]any{"branch": branch, "ref": "main"})
				wantFields(t, put, bodyOf(t, calls, put), map[string]any{
					"branch": branch, "content": proposeContent, "commit_message": "Bump podinfo to 6.7.1", "last_commit_id": "f11e5ha",
				})
				wantFields(t, mrs, bodyOf(t, calls, mrs), map[string]any{
					"source_branch": branch, "target_branch": "main", "title": "Draft: Bump podinfo to 6.7.1",
					"description": "Proposed from Ichor on lab for Argo CD Application argocd/podinfo", "remove_source_branch": true,
				})
			},
		},
		{
			name: "new file is a POST", change: func(o map[string]any) { o["fileSha"] = "" }, answers: ok,
			wantCalls: []string{create, post, mrs},
			check: func(t *testing.T, calls []proposeCall, _ gitforge.ProposeResult) {
				if _, has := bodyOf(t, calls, post)["last_commit_id"]; has {
					t.Error("a new file was sent with last_commit_id")
				}
			},
		},
		{
			name:      "stale file sha",
			answers:   with(put, proposeAnswer{400, `{"message":"You are attempting to update a file that has changed since you started editing it."}`}),
			wantCalls: []string{create, put, del},
			wantErr:   "the file changed on main since you opened it: reload and try again",
		},
		{
			name:      "branch exists",
			answers:   with(create, proposeAnswer{400, `{"message":"Branch already exists"}`}, proposeAnswer{201, `{}`}),
			wantCalls: []string{create, create, put, mrs},
			check: func(t *testing.T, calls []proposeCall, res gitforge.ProposeResult) {
				if res.Branch != "ichor/bump-podinfo-to-6-7-1-0002" || calls[1].body["branch"] != res.Branch {
					t.Errorf("branch %q, retried %v", res.Branch, calls[1].body["branch"])
				}
			},
		},
		{
			name:      "merge request refused",
			answers:   with(mrs, proposeAnswer{500, `{"message":"500 Internal Server Error"}`}),
			wantCalls: []string{create, put, mrs, del},
			wantErr:   "500",
		},
	})
}

func TestGitForgeProposeGitea(t *testing.T) {
	const (
		repo     = "/api/v1/repos/example-org/platform"
		create   = "POST " + repo + "/branches"
		put      = "PUT " + repo + "/contents/apps/podinfo/deployment.yaml"
		post     = "POST " + repo + "/contents/apps/podinfo/deployment.yaml"
		pulls    = "POST " + repo + "/pulls"
		branch   = "ichor/bump-podinfo-to-6-7-1-0001"
		del      = "DELETE " + repo + "/branches/ichor/bump-podinfo-to-6-7-1-0001"
		contents = `{"content":{"sha":"n3w"},"commit":{"sha":"9173a"}}`
	)

	ok := map[string][]proposeAnswer{
		create: {{201, `{"name":"` + branch + `"}`}},
		put:    {{200, contents}},
		post:   {{201, contents}},
		pulls:  {{201, `{"html_url":"https://git.example.org/example-org/platform/pulls/3","number":3}`}},
		del:    {{204, ``}},
	}

	with := func(route string, answers ...proposeAnswer) map[string][]proposeAnswer {
		out := map[string][]proposeAnswer{}
		for k, v := range ok {
			out[k] = v
		}

		out[route] = answers

		return out
	}

	runProposeCases(t, "gitea", "https://127.0.0.1/example-org/platform", []proposeCase{
		{
			name: "update", change: func(o map[string]any) { o["draft"] = true }, answers: ok, wantCalls: []string{create, put, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitforge.ProposeResult) {
				if res.URL != "https://git.example.org/example-org/platform/pulls/3" || res.Number != 3 || res.Branch != branch || res.Commit != "9173a" {
					t.Errorf("result %+v", res)
				}

				wantFields(t, create, bodyOf(t, calls, create), map[string]any{"new_branch_name": branch, "old_branch_name": "main"})
				wantFields(t, put, bodyOf(t, calls, put), map[string]any{"branch": branch, "sha": "f11e5ha", "message": "Bump podinfo to 6.7.1"})
				wantBase64(t, bodyOf(t, calls, put))
				wantFields(t, pulls, bodyOf(t, calls, pulls), map[string]any{"head": branch, "base": "main", "title": "WIP: Bump podinfo to 6.7.1"})
			},
		},
		{
			name: "new file is a POST", change: func(o map[string]any) { o["fileSha"] = "" }, answers: ok,
			wantCalls: []string{create, post, pulls},
			check: func(t *testing.T, calls []proposeCall, _ gitforge.ProposeResult) {
				if _, has := bodyOf(t, calls, post)["sha"]; has {
					t.Error("a new file was sent with a sha")
				}

				wantBase64(t, bodyOf(t, calls, post))
			},
		},
		{
			name:      "stale file sha",
			answers:   with(put, proposeAnswer{422, `{"message":"sha does not match [given: f11e5ha, expected: 0ther]"}`}),
			wantCalls: []string{create, put, del},
			wantErr:   "the file changed on main since you opened it: reload and try again",
		},
		{
			name:      "branch exists",
			answers:   with(create, proposeAnswer{409, `{"message":"The branch already exists."}`}, proposeAnswer{201, `{}`}),
			wantCalls: []string{create, create, put, pulls},
			check: func(t *testing.T, _ []proposeCall, res gitforge.ProposeResult) {
				if res.Branch != "ichor/bump-podinfo-to-6-7-1-0002" {
					t.Errorf("branch %q", res.Branch)
				}
			},
		},
		{
			name:      "pull request refused",
			answers:   with(pulls, proposeAnswer{500, `{"message":"internal error"}`}),
			wantCalls: []string{create, put, pulls, del},
			wantErr:   "internal error",
		},
	})
}

// A proposal that cannot be right is refused before any request (git.example.org is never
// reached).
func TestGitForgeProposeValidation(t *testing.T) {
	host, repo := gitHostJSON("github", "https://git.example.org"), "https://git.example.org/example-org/platform"

	for name, tc := range map[string]struct {
		change  func(map[string]any)
		wantErr string
	}{
		"path climbs":      {func(o map[string]any) { o["path"] = "apps/../../etc/passwd" }, "leaves the repository"},
		"path not clean":   {func(o map[string]any) { o["path"] = "apps/./podinfo.yaml" }, "is not a file path"},
		"no path":          {func(o map[string]any) { o["path"] = "" }, "is not a file path"},
		"path is root":     {func(o map[string]any) { o["path"] = "." }, "is not a file path"},
		"content too big":  {func(o map[string]any) { o["content"] = strings.Repeat("a", gitforge.MaxFile+1) }, "over 1 MiB"},
		"no message":       {func(o map[string]any) { o["message"] = " " }, "no commit message"},
		"no title":         {func(o map[string]any) { o["title"] = "" }, "no title"},
		"no base":          {func(o map[string]any) { o["baseRef"] = "" }, "no branch, tag or commit"},
		"bad branch":       {func(o map[string]any) { o["branch"] = "ichor/a b" }, "is not a branch name"},
		"branch climbs":    {func(o map[string]any) { o["branch"] = "ichor/../main" }, "is not a branch name"},
		"branch is base":   {func(o map[string]any) { o["branch"] = "main" }, "not on main"},
		"other host":       {func(o map[string]any) { o["repoURL"] = "https://github.com/example-org/platform" }, "not on the Git host git.example.org"},
		"not a repository": {func(o map[string]any) { o["repoURL"] = "oci://registry.example.org/chart" }, "not a Git repository"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := GitForgePropose("", "lab", host, proposeOptions(t, repo, tc.change))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %v, want %q", err, tc.wantErr)
			}
		})
	}

	if _, err := GitForgePropose("", "lab", host, "{"); err == nil || !strings.Contains(err.Error(), "invalid proposal") {
		t.Errorf("bad JSON: %v", err)
	}
}
