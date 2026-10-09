package ichorgo

import (
	"encoding/json"
	"testing"
)

func TestArgoRepoWebURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/org/repo.git":           "https://github.com/org/repo",
		"https://user:token@gitlab.com/org/repo":    "https://gitlab.com/org/repo",
		"git@github.com:org/repo.git":               "https://github.com/org/repo",
		"ssh://git@git.example.com:2222/org/repo":   "https://git.example.com/org/repo",
		"https://grafana.github.io/helm-charts":     "https://grafana.github.io/helm-charts",
		"oci://ghcr.io/org/charts":                  "",
		"ghcr.io/org/charts":                        "",
		"https://github.com/":                       "",
		"":                                          "",
		"https://codeberg.org/org/repo/":            "https://codeberg.org/org/repo",
		"git+ssh://git@bitbucket.org/team/repo.git": "https://bitbucket.org/team/repo",
	}

	for in, want := range cases {
		if got := argoRepoWebURL(in); got != want {
			t.Errorf("argoRepoWebURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArgoCommitURL(t *testing.T) {
	const sha = "4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"

	cases := []struct{ repo, rev, want string }{
		{"https://github.com/org/repo.git", sha, "https://github.com/org/repo/commit/" + sha},
		{"git@gitlab.example.com:group/sub/repo.git", sha, "https://gitlab.example.com/group/sub/repo/-/commit/" + sha},
		{"https://bitbucket.org/team/repo", sha, "https://bitbucket.org/team/repo/commits/" + sha},
		{"https://git.homelab.lan/ops/fleet", "4be1d0c", "https://git.homelab.lan/ops/fleet/commit/4be1d0c"},
		{"https://dev.azure.com/org/project/_git/repo", sha, "https://dev.azure.com/org/project/_git/repo/commit/" + sha},
		{"https://github.com/org/repo", "main", ""},
		{"https://github.com/org/repo", "8.6.0", ""},
		{"https://github.com/org/repo", "HEAD", ""},
		{"oci://ghcr.io/org/charts", sha, ""},
	}

	for _, c := range cases {
		if got := argoCommitURL(c.repo, c.rev); got != c.want {
			t.Errorf("argoCommitURL(%q, %q) = %q, want %q", c.repo, c.rev, got, c.want)
		}
	}
}

func TestArgoRevisionURLPairsSources(t *testing.T) {
	const sha1, sha2 = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	chart := argoSourceObject{RepoURL: "https://charts.example.com", Chart: "app", TargetRevision: "1.2.3"}
	git := argoSourceObject{RepoURL: "git@github.com:org/values.git", Path: "values", TargetRevision: "main"}

	// Multi-source: the chart's "revision" is its version; the Git source pairs by position.
	if got := argoRevisionURL([]argoSourceObject{chart, git}, []string{"1.2.3", sha2}, ""); got != "https://github.com/org/values/commit/"+sha2 {
		t.Fatalf("multi-source: %q", got)
	}

	// Single source.
	if got := argoRevisionURL([]argoSourceObject{git}, nil, sha1); got != "https://github.com/org/values/commit/"+sha1 {
		t.Fatalf("single source: %q", got)
	}

	// A chart-only app has no commit to link.
	if got := argoRevisionURL([]argoSourceObject{chart}, nil, "1.2.3"); got != "" {
		t.Fatalf("chart only: %q", got)
	}
}

func TestMapArgoAppLinks(t *testing.T) {
	const sha, old = "4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d", "91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e"

	var o argoObject
	if err := json.Unmarshal([]byte(`{
		"metadata":{"name":"web","namespace":"argocd"},
		"spec":{"source":{"repoURL":"git@github.com:org/gitops.git","path":"apps/web","targetRevision":"main"},"destination":{"namespace":"web"}},
		"status":{"sync":{"status":"Synced","revision":"`+sha+`"},
			"operationState":{"phase":"Succeeded","operation":{"sync":{"revision":"`+sha+`"}},"syncResult":{"revision":"`+sha+`"}},
			"history":[
				{"id":1,"revision":"`+old+`","source":{"repoURL":"git@github.com:org/gitops.git","path":"apps/web","targetRevision":"main"}},
				{"id":2,"revision":"`+sha+`","source":{"repoURL":"git@github.com:org/gitops.git","path":"apps/web","targetRevision":"main"}}]}}`), &o); err != nil {
		t.Fatal(err)
	}

	a := mapArgoApp(o, map[string]bool{})

	if a.RevisionURL != "https://github.com/org/gitops/commit/"+sha {
		t.Fatalf("revision url %q", a.RevisionURL)
	}

	if a.Sources[0].RepoURL != "https://github.com/org/gitops" {
		t.Fatalf("repo url %q", a.Sources[0].RepoURL)
	}

	if a.Operation == nil || a.Operation.RevisionURL != a.RevisionURL {
		t.Fatalf("operation url %+v", a.Operation)
	}

	if len(a.History) != 2 || a.History[0].URL != a.RevisionURL || a.History[1].URL != "https://github.com/org/gitops/commit/"+old {
		t.Fatalf("history urls %+v", a.History)
	}

}
