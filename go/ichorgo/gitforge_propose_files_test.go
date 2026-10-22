package ichorgo

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

const kustomizationContent = "resources:\n  - deployment.yaml\nimages:\n  - name: podinfo\n    newTag: 6.7.1\n"

// twoFiles turns the proposal into a Deployment update and a new kustomization.yaml.
func twoFiles(o map[string]any) {
	o["path"], o["content"], o["fileSha"] = "", "", ""
	o["files"] = []any{
		map[string]any{"path": "apps/podinfo/deployment.yaml", "content": proposeContent, "fileSha": "f11e5ha"},
		map[string]any{"path": "apps/podinfo/kustomization.yaml", "content": kustomizationContent},
	}
}

// withDelete adds the deletion of apps/podinfo/old.yaml to twoFiles.
func withDelete(o map[string]any) {
	twoFiles(o)
	o["files"] = append(o["files"].([]any), map[string]any{"path": "apps/podinfo/old.yaml", "fileSha": "01d", "delete": true})
}

// answering is ok with route's answers replaced.
func answering(ok map[string][]proposeAnswer, route string, answers ...proposeAnswer) map[string][]proposeAnswer {
	out := make(map[string][]proposeAnswer, len(ok)+1)
	for k, v := range ok {
		out[k] = v
	}

	out[route] = answers

	return out
}

func TestGitForgeProposeFilesGitHub(t *testing.T) {
	const (
		repo   = "/api/v3/repos/example-org/platform"
		ref    = "GET " + repo + "/git/ref/heads/main"
		refs   = "POST " + repo + "/git/refs"
		putA   = "PUT " + repo + "/contents/apps/podinfo/deployment.yaml"
		putB   = "PUT " + repo + "/contents/apps/podinfo/kustomization.yaml"
		delC   = "DELETE " + repo + "/contents/apps/podinfo/old.yaml"
		pulls  = "POST " + repo + "/pulls"
		branch = "ichor/bump-podinfo-to-6-7-1-0001"
		del    = "DELETE " + repo + "/git/refs/heads/" + branch
	)

	ok := map[string][]proposeAnswer{
		ref:   {{200, `{"object":{"sha":"ba5e"}}`}},
		refs:  {{201, `{}`}},
		putA:  {{200, `{"commit":{"sha":"c1"}}`}},
		putB:  {{201, `{"commit":{"sha":"c2"}}`}},
		delC:  {{200, `{"commit":{"sha":"c3"}}`}},
		pulls: {{201, `{"html_url":"https://git.example.org/example-org/platform/pull/43","number":43}`}},
		del:   {{204, ``}},
	}

	runProposeCases(t, "github", "https://127.0.0.1/example-org/platform", []proposeCase{
		{
			name: "two files, two commits", change: twoFiles, answers: ok, wantCalls: []string{ref, refs, putA, putB, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitProposeResult) {
				if res.Number != 43 || res.Commit != "c2" || !slices.Equal(res.Commits, []string{"c1", "c2"}) {
					t.Errorf("result %+v", res)
				}

				wantFields(t, putA, bodyOf(t, calls, putA), map[string]any{"message": "Bump podinfo to 6.7.1 (1/2)", "sha": "f11e5ha", "branch": branch})
				wantBase64(t, bodyOf(t, calls, putA))
				wantFields(t, putB, bodyOf(t, calls, putB), map[string]any{"message": "Bump podinfo to 6.7.1 (2/2)", "branch": branch})

				if _, has := bodyOf(t, calls, putB)["sha"]; has {
					t.Error("a new file was sent with a sha")
				}
			},
		},
		{
			name: "a deletion", change: withDelete, answers: ok, wantCalls: []string{ref, refs, putA, putB, delC, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitProposeResult) {
				wantFields(t, delC, bodyOf(t, calls, delC), map[string]any{"message": "Bump podinfo to 6.7.1 (3/3)", "sha": "01d", "branch": branch})

				if len(res.Commits) != 3 {
					t.Errorf("commits %v", res.Commits)
				}
			},
		},
		{
			name: "second file fails: the branch is rolled back", change: twoFiles,
			answers:   answering(ok, putB, proposeAnswer{422, `{"message":"Invalid request. \"sha\" wasn't supplied."}`}),
			wantCalls: []string{ref, refs, putA, putB, del},
			wantErr:   "apps/podinfo/kustomization.yaml changed on main since you opened it",
		},
		{
			name: "second file refused otherwise", change: twoFiles,
			answers:   answering(ok, putB, proposeAnswer{403, `{"message":"Resource not accessible by integration"}`}),
			wantCalls: []string{ref, refs, putA, putB, del},
			wantErr:   "apps/podinfo/kustomization.yaml",
		},
	})
}

func TestGitForgeProposeFilesGitLab(t *testing.T) {
	const (
		project = "/api/v4/projects/group%2Fplatform"
		create  = "POST " + project + "/repository/branches"
		commits = "POST " + project + "/repository/commits"
		mrs     = "POST " + project + "/merge_requests"
		del     = "DELETE " + project + "/repository/branches/ichor%2Fbump-podinfo-to-6-7-1-0001"
	)

	ok := map[string][]proposeAnswer{
		create:  {{201, `{}`}},
		commits: {{201, `{"id":"0ne"}`}},
		mrs:     {{201, `{"web_url":"https://git.example.org/group/platform/-/merge_requests/8","iid":8,"sha":"0ne"}`}},
		del:     {{204, ``}},
	}

	runProposeCases(t, "gitlab", "https://127.0.0.1/group/platform.git", []proposeCase{
		{
			name: "one commit with three actions", change: withDelete, answers: ok, wantCalls: []string{create, commits, mrs},
			check: func(t *testing.T, calls []proposeCall, res gitProposeResult) {
				if res.Number != 8 || !slices.Equal(res.Commits, []string{"0ne"}) {
					t.Errorf("result %+v", res)
				}

				body := bodyOf(t, calls, commits)
				wantFields(t, commits, body, map[string]any{"branch": "ichor/bump-podinfo-to-6-7-1-0001", "commit_message": "Bump podinfo to 6.7.1"})

				actions, _ := body["actions"].([]any)
				want := []map[string]any{
					{"action": "update", "file_path": "apps/podinfo/deployment.yaml", "content": proposeContent, "last_commit_id": "f11e5ha"},
					{"action": "create", "file_path": "apps/podinfo/kustomization.yaml", "content": kustomizationContent},
					{"action": "delete", "file_path": "apps/podinfo/old.yaml", "last_commit_id": "01d"},
				}

				if len(actions) != len(want) {
					t.Fatalf("actions %v", actions)
				}

				for i, a := range actions {
					got, _ := a.(map[string]any)
					if len(got) != len(want[i]) {
						t.Errorf("action %d: %v, want %v", i, got, want[i])
					}

					wantFields(t, commits, got, want[i])
				}
			},
		},
		{
			name: "commit refused: the branch is rolled back", change: twoFiles,
			answers:   answering(ok, commits, proposeAnswer{400, `{"message":"A file with this name already exists"}`}),
			wantCalls: []string{create, commits, del},
			wantErr:   "a staged file changed on main",
		},
	})
}

func TestGitForgeProposeFilesGitea(t *testing.T) {
	const (
		repo   = "/api/v1/repos/example-org/platform"
		create = "POST " + repo + "/branches"
		batch  = "POST " + repo + "/contents"
		putA   = "PUT " + repo + "/contents/apps/podinfo/deployment.yaml"
		postB  = "POST " + repo + "/contents/apps/podinfo/kustomization.yaml"
		pulls  = "POST " + repo + "/pulls"
		del    = "DELETE " + repo + "/branches/ichor/bump-podinfo-to-6-7-1-0001"
	)

	ok := map[string][]proposeAnswer{
		create: {{201, `{}`}},
		batch:  {{201, `{"commit":{"sha":"6a7c4"}}`}},
		putA:   {{200, `{"commit":{"sha":"g1"}}`}},
		postB:  {{201, `{"commit":{"sha":"g2"}}`}},
		pulls:  {{201, `{"html_url":"https://git.example.org/example-org/platform/pulls/4","number":4}`}},
		del:    {{204, ``}},
	}

	runProposeCases(t, "gitea", "https://127.0.0.1/example-org/platform", []proposeCase{
		{
			name: "one commit", change: withDelete, answers: ok, wantCalls: []string{create, batch, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitProposeResult) {
				if res.Commit != "6a7c4" || !slices.Equal(res.Commits, []string{"6a7c4"}) {
					t.Errorf("result %+v", res)
				}

				files, _ := bodyOf(t, calls, batch)["files"].([]any)
				if len(files) != 3 {
					t.Fatalf("files %v", files)
				}

				ops := make([]string, len(files))

				for i, f := range files {
					e, _ := f.(map[string]any)
					ops[i], _ = e["operation"].(string)
				}

				if !slices.Equal(ops, []string{"update", "create", "delete"}) {
					t.Errorf("operations %v", ops)
				}

				first, _ := files[0].(map[string]any)
				wantFields(t, batch, first, map[string]any{"path": "apps/podinfo/deployment.yaml", "sha": "f11e5ha"})
				wantBase64(t, first)

				if _, has := files[2].(map[string]any)["content"]; has {
					t.Error("a deletion was sent with content")
				}
			},
		},
		{
			name: "older server: one commit per file", change: twoFiles,
			answers:   answering(ok, batch, proposeAnswer{404, `{"message":"Not Found"}`}),
			wantCalls: []string{create, batch, putA, postB, pulls},
			check: func(t *testing.T, calls []proposeCall, res gitProposeResult) {
				if !slices.Equal(res.Commits, []string{"g1", "g2"}) {
					t.Errorf("commits %v", res.Commits)
				}

				raw, _ := bodyOf(t, calls, postB)["content"].(string)
				if dec, err := base64.StdEncoding.DecodeString(raw); err != nil || string(dec) != kustomizationContent {
					t.Errorf("content %q %v", dec, err)
				}
			},
		},
		{
			name: "commit refused: the branch is rolled back", change: twoFiles,
			answers:   answering(ok, batch, proposeAnswer{422, `{"message":"sha does not match"}`}),
			wantCalls: []string{create, batch, del},
			wantErr:   "a staged file changed on main",
		},
	})
}

// A multi-file proposal that cannot be right is refused before any request.
func TestGitForgeProposeFilesValidation(t *testing.T) {
	host, repo := gitHostJSON("github", "https://git.example.org"), "https://git.example.org/example-org/platform"

	file := func(path string) map[string]any { return map[string]any{"path": path, "content": "a: 1\n"} }

	many := make([]any, gitProposeMaxFiles+1)
	for i := range many {
		many[i] = file("apps/f" + strings.Repeat("x", i) + ".yaml")
	}

	for name, tc := range map[string]struct {
		change  func(map[string]any)
		wantErr string
	}{
		"too many":       {func(o map[string]any) { twoFiles(o); o["files"] = many }, "propose at most 20"},
		"none":           {func(o map[string]any) { twoFiles(o); o["files"] = []any{} }, "no file to propose"},
		"twice":          {func(o map[string]any) { twoFiles(o); o["files"] = []any{file("a.yaml"), file("/a.yaml")} }, "a.yaml is in the proposal twice"},
		"path and files": {func(o map[string]any) { twoFiles(o); o["path"] = "a.yaml" }, "not both"},
		"climbs":         {func(o map[string]any) { twoFiles(o); o["files"] = []any{file("apps/../../x")} }, "leaves the repository"},
		"root":           {func(o map[string]any) { twoFiles(o); o["files"] = []any{file("/")} }, "is not a file path"},
		"too big": {func(o map[string]any) {
			twoFiles(o)
			o["files"] = []any{map[string]any{"path": "a.yaml", "content": strings.Repeat("a", gitForgeMaxFile+1)}}
		}, "a.yaml is over 1 MiB"},
		"delete without sha": {func(o map[string]any) {
			twoFiles(o)
			o["files"] = []any{map[string]any{"path": "a.yaml", "delete": true}}
		}, "needs the sha"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := GitForgePropose("", "lab", host, proposeOptions(t, repo, tc.change))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %v, want %q", err, tc.wantErr)
			}
		})
	}
}
