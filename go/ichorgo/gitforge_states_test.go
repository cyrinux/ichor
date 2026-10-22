package ichorgo

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestGitForgeRequestStates(t *testing.T) {
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v3/repos/example-org/cluster-config/pulls/7": jsonAnswer(`{"state":"closed","merged":true,"title":"Raise ingest replicas",
			"merge_commit_sha":"m3rg3d","merged_at":"2026-10-18T08:30:00Z","updated_at":"2026-10-18T08:30:00Z","head":{"sha":"h3ad"}}`),
		"/api/v3/repos/example-org/cluster-config/pulls/9": jsonAnswer(`{"state":"closed","merged":false,"title":"Dropped",
			"updated_at":"2026-10-19T10:00:00Z","head":{"sha":"x"}}`),
		"/api/v3/repos/example-org/cluster-config/pulls/11": jsonAnswer(`{"state":"open","merged":false,"title":"Still open","head":{"sha":"0pen"}}`),
		"/api/v1/repos/example-org/cluster-config/pulls/5": jsonAnswer(`{"state":"closed","merged":true,"merge_commit_sha":"g1tea",
			"merged_at":"2026-10-20T12:00:00Z","head":{"sha":"h"}}`),
	})
	repo := srv.URL + "/example-org/cluster-config"

	cases := map[string]struct {
		host     string
		requests string
		want     []gitRequestStateItem
	}{
		"github": {
			gitHostJSON("github", srv.URL+"/api/v3"),
			fmt.Sprintf(`[{"repoURL":%q,"number":7},{"repoURL":%q,"number":9},{"repoURL":%q,"number":11},{"repoURL":%q,"number":404}]`, repo, repo, repo, repo),
			[]gitRequestStateItem{
				{RepoURL: repo, Number: 7, State: "merged", MergedAt: "2026-10-18T08:30:00Z", SHA: "m3rg3d"},
				{RepoURL: repo, Number: 9, State: "closed", ClosedAt: "2026-10-19T10:00:00Z", SHA: "x"},
				{RepoURL: repo, Number: 11, State: "open", SHA: "0pen"},
				{RepoURL: repo, Number: 404},
			},
		},
		"gitea": {
			gitHostJSON("gitea", srv.URL),
			fmt.Sprintf(`[{"repoURL":%q,"number":5}]`, repo),
			[]gitRequestStateItem{{RepoURL: repo, Number: 5, State: "merged", MergedAt: "2026-10-20T12:00:00Z", SHA: "g1tea"}},
		},
		"none": {gitHostJSON("github", srv.URL+"/api/v3"), `[]`, []gitRequestStateItem{}},
	}

	for name, c := range cases {
		got := decodeAnswer[[]gitRequestStateItem](t)(GitForgeRequestStates(c.host, c.requests))
		if len(got) != len(c.want) {
			t.Fatalf("%s: %d items, want %d: %+v", name, len(got), len(c.want), got)
		}

		for i, w := range c.want {
			g := got[i]
			// A failed item says why, without failing the batch.
			if w.State == "" {
				if g.Error == "" || g.State != "" {
					t.Errorf("%s #%d: want an error, got %+v", name, i, g)
				}

				continue
			}

			if g != w {
				t.Errorf("%s #%d = %+v, want %+v", name, i, g, w)
			}
		}
	}
}

func TestGitForgeRequestStatesRefusals(t *testing.T) {
	if _, err := GitForgeRequestStates(gitHostJSON("github", "https://ghe.invalid/api/v3"), "not json"); err == nil {
		t.Error("invalid requests were read")
	}

	many := "[" + strings.TrimSuffix(strings.Repeat(`{"repoURL":"https://github.com/example-org/x","number":1},`, gitRequestStatesMax+1), ",") + "]"
	if _, err := GitForgeRequestStates(gitHostJSON("github", "https://ghe.invalid/api/v3"), many); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("more than %d requests: %v", gitRequestStatesMax, err)
	}
}
