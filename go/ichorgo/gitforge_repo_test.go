package ichorgo

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestGitForgeRepoInfo(t *testing.T) {
	for _, tc := range []struct {
		name, kind, branch string
		routes             map[string]func(w http.ResponseWriter, r *http.Request)
		want               gitForgeRepoInfo
	}{
		{
			name: "github default branch", kind: gitForgeGitHub,
			routes: map[string]func(w http.ResponseWriter, r *http.Request){
				"/api/v3/repos/example-org/cluster-config":               jsonAnswer(`{"default_branch":"main","html_url":"https://git.example.org/example-org/cluster-config","private":true,"permissions":{"admin":false,"push":true,"pull":true}}`),
				"/api/v3/repos/example-org/cluster-config/branches/main": jsonAnswer(`{"name":"main","commit":{"sha":"aaa111"}}`),
			},
			want: gitForgeRepoInfo{DefaultBranch: "main", Branch: "main", HeadSha: "aaa111", WebURL: "https://git.example.org/example-org/cluster-config", Private: true, CanPush: true, Forge: gitForgeGitHub},
		},
		{
			name: "gitlab developer through the group, other branch", kind: gitForgeGitLab, branch: "staging",
			routes: map[string]func(w http.ResponseWriter, r *http.Request){
				"/api/v4/projects/example-org%2Fcluster-config":                             jsonAnswer(`{"default_branch":"main","web_url":"https://gitlab.example.org/example-org/cluster-config","visibility":"internal","permissions":{"project_access":null,"group_access":{"access_level":30}}}`),
				"/api/v4/projects/example-org%2Fcluster-config/repository/branches/staging": jsonAnswer(`{"name":"staging","commit":{"id":"bbb222"}}`),
			},
			want: gitForgeRepoInfo{DefaultBranch: "main", Branch: "staging", HeadSha: "bbb222", WebURL: "https://gitlab.example.org/example-org/cluster-config", Private: true, CanPush: true, Forge: gitForgeGitLab},
		},
		{
			name: "gitlab reporter cannot push", kind: gitForgeGitLab,
			routes: map[string]func(w http.ResponseWriter, r *http.Request){
				"/api/v4/projects/example-org%2Fcluster-config":                          jsonAnswer(`{"default_branch":"main","web_url":"https://gitlab.example.org/example-org/cluster-config","visibility":"public","permissions":{"project_access":{"access_level":20},"group_access":null}}`),
				"/api/v4/projects/example-org%2Fcluster-config/repository/branches/main": jsonAnswer(`{"commit":{"id":"ccc333"}}`),
			},
			want: gitForgeRepoInfo{DefaultBranch: "main", Branch: "main", HeadSha: "ccc333", WebURL: "https://gitlab.example.org/example-org/cluster-config", Forge: gitForgeGitLab},
		},
		{
			name: "gitea read only", kind: gitForgeGitea,
			routes: map[string]func(w http.ResponseWriter, r *http.Request){
				"/api/v1/repos/example-org/cluster-config":                jsonAnswer(`{"default_branch":"trunk","html_url":"https://gitea.home.example/example-org/cluster-config","private":false,"permissions":{"push":false}}`),
				"/api/v1/repos/example-org/cluster-config/branches/trunk": jsonAnswer(`{"name":"trunk","commit":{"id":"ddd444"}}`),
			},
			want: gitForgeRepoInfo{DefaultBranch: "trunk", Branch: "trunk", HeadSha: "ddd444", WebURL: "https://gitea.home.example/example-org/cluster-config", Forge: gitForgeGitea},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := gitServer(t, tc.routes)

			out, err := GitForgeRepo(gitHostJSON(tc.kind, srv.URL), "https://127.0.0.1/example-org/cluster-config.git", tc.branch)
			if err != nil {
				t.Fatal(err)
			}

			var got gitForgeRepoInfo
			if err := json.Unmarshal([]byte(out), &got); err != nil || got != tc.want {
				t.Errorf("got %+v %v, want %+v", got, err, tc.want)
			}
		})
	}
}

func TestGitForgeRepoNotFound(t *testing.T) {
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v3/repos/example-org/cluster-config": jsonAnswer(`{"default_branch":"main","permissions":{"push":true}}`),
	})
	host := gitHostJSON(gitForgeGitHub, srv.URL)

	for _, tc := range []struct{ repo, branch, want string }{
		{"https://127.0.0.1/example-org/other", "", "example-org/other: not found or no access with this token (a fine-grained token must have this repository selected)"},
		{"https://127.0.0.1/example-org/cluster-config", "", "branch main is not in example-org/cluster-config"},
		{"https://127.0.0.1/example-org/cluster-config", "dev", "branch dev is not in example-org/cluster-config"},
	} {
		if _, err := GitForgeRepo(host, tc.repo, tc.branch); err == nil || err.Error() != tc.want {
			t.Errorf("%s %s: %v, want %q", tc.repo, tc.branch, err, tc.want)
		}
	}

	gitlab := gitServer(t, nil)
	if _, err := GitForgeRepo(gitHostJSON(gitForgeGitLab, gitlab.URL), "https://127.0.0.1/a/b", ""); err == nil || err.Error() != "a/b: not found or no access with this token" {
		t.Errorf("gitlab 404: %v", err)
	}

	for _, bad := range []struct{ repo, branch string }{
		{"https://127.0.0.1/onlyowner", ""},
		{"https://elsewhere.example/example-org/cluster-config", ""},
		{"https://127.0.0.1/example-org/cluster-config", "bad branch"},
	} {
		if _, err := GitForgeRepo(host, bad.repo, bad.branch); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}
