package ichorgo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/gitforge"
)

// decodeAnswer checks a call's answer and decodes it: decodeAnswer[T](t)(Call(...)).
func decodeAnswer[T any](t *testing.T) func(string, error) T {
	t.Helper()

	return func(out string, err error) T {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}

		var v T
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatal(err)
		}

		return v
	}
}

func requestOf(t *testing.T) func(string, error) gitforge.RequestState {
	return decodeAnswer[gitforge.RequestState](t)
}

func TestGitForgeRequest(t *testing.T) {
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		// GitHub: a squash merge, its merge commit differs from the head.
		"/api/v3/repos/example-org/cluster-config/pulls/7": jsonAnswer(`{"state":"closed","merged":true,"title":"Scale podinfo to 3",
			"html_url":"https://github.example.org/example-org/cluster-config/pull/7","merge_commit_sha":"m3rg3d",
			"merged_at":"2026-10-18T08:30:00Z","updated_at":"2026-10-18T08:30:00Z","head":{"sha":"h3ad"}}`),
		"/api/v3/repos/example-org/cluster-config/pulls/8": jsonAnswer(`{"state":"open","merged":false,"draft":true,"title":"WIP",
			"merge_commit_sha":"t3stmerge","head":{"sha":"h3ad2"}}`),
		"/api/v3/repos/example-org/cluster-config/pulls/9": jsonAnswer(`{"state":"closed","merged":false,"title":"Dropped","head":{"sha":"x"}}`),
		// GitLab: merge requests by their iid, "opened" while open.
		"/api/v4/projects/example-org%2Fcluster-config/merge_requests/3": jsonAnswer(`{"state":"merged","title":"Bump","web_url":"u",
			"sha":"h3ad","merge_commit_sha":null,"squash_commit_sha":"squ4sh","merged_at":"2026-10-18T08:30:00Z"}`),
		"/api/v4/projects/example-org%2Fcluster-config/merge_requests/4": jsonAnswer(`{"state":"opened","title":"Open MR","sha":"h3ad"}`),
		// Gitea / Forgejo.
		"/api/v1/repos/example-org/cluster-config/pulls/5": jsonAnswer(`{"state":"closed","merged":true,"merge_commit_sha":"g1tea","head":{"sha":"h"}}`),
	})

	repo := srv.URL + "/example-org/cluster-config"

	merged := requestOf(t)(GitForgeRequest(gitHostJSON("github", srv.URL+"/api/v3"), repo, 7))
	if merged.State != gitforge.RequestMerged || merged.MergeSHA != "m3rg3d" || merged.HeadSHA != "h3ad" || merged.Title != "Scale podinfo to 3" {
		t.Errorf("github merged %+v", merged)
	}

	// An open request's merge commit is GitHub's test merge: not given.
	draft := requestOf(t)(GitForgeRequest(gitHostJSON("github", srv.URL+"/api/v3"), repo, 8))
	if draft.State != gitforge.RequestOpen || !draft.Draft || draft.MergeSHA != "" {
		t.Errorf("github draft %+v", draft)
	}

	if s := requestOf(t)(GitForgeRequest(gitHostJSON("github", srv.URL+"/api/v3"), repo, 9)); s.State != gitforge.RequestClosed {
		t.Errorf("github closed %+v", s)
	}

	lab := requestOf(t)(GitForgeRequest(gitHostJSON("gitlab", srv.URL), repo, 3))
	if lab.State != gitforge.RequestMerged || lab.MergeSHA != "squ4sh" {
		t.Errorf("gitlab merged %+v", lab)
	}

	if s := requestOf(t)(GitForgeRequest(gitHostJSON("gitlab", srv.URL), repo, 4)); s.State != gitforge.RequestOpen {
		t.Errorf("gitlab open %+v", s)
	}

	if s := requestOf(t)(GitForgeRequest(gitHostJSON("gitea", srv.URL), repo, 5)); s.State != gitforge.RequestMerged || s.MergeSHA != "g1tea" {
		t.Errorf("gitea %+v", s)
	}

	if _, err := GitForgeRequest(gitHostJSON("github", srv.URL+"/api/v3"), repo, 404); err == nil || !strings.Contains(err.Error(), "#404") {
		t.Errorf("missing: %v", err)
	}

	if _, err := GitForgeRequest(gitHostJSON("github", srv.URL+"/api/v3"), repo, 0); err != gitforge.ErrRequestNumber {
		t.Errorf("no number: %v", err)
	}
}

// GitHub and Gitea share one pull-request reader: the same answer reads the same on both.
func TestGitForgeRequestGitHubStyle(t *testing.T) {
	const pr = `{"state":"closed","merged":true,"draft":false,"title":"Pin the chart",
		"html_url":"https://forge.example.org/example-org/cluster-config/pulls/12","merge_commit_sha":"m3rg3",
		"merged_at":"2026-10-19T09:00:00Z","updated_at":"2026-10-19T09:01:00Z","head":{"sha":"h3ad"}}`

	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v3/repos/example-org/cluster-config/pulls/12": jsonAnswer(pr),
		"/api/v1/repos/example-org/cluster-config/pulls/12": jsonAnswer(pr),
	})

	want := gitforge.RequestState{
		State: gitforge.RequestMerged, Title: "Pin the chart", URL: "https://forge.example.org/example-org/cluster-config/pulls/12",
		HeadSHA: "h3ad", MergeSHA: "m3rg3", MergedAt: "2026-10-19T09:00:00Z", UpdatedAt: "2026-10-19T09:01:00Z",
	}

	for _, tc := range []struct{ kind, api string }{
		{"github", srv.URL + "/api/v3"},
		{"gitea", srv.URL},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			got := requestOf(t)(GitForgeRequest(gitHostJSON(tc.kind, tc.api), srv.URL+"/example-org/cluster-config", 12))
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}
