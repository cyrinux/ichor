package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestGitForgeRepoDemo(t *testing.T) {
	out, err := GitForgeRepo(`{"kind":"demo"}`, "", "")

	var info gitForgeRepoInfo
	if err != nil || json.Unmarshal([]byte(out), &info) != nil || info.Branch != "main" || info.HeadSha != demoClusterRepoHead || !info.CanPush || info.Forge != gitForgeDemo {
		t.Errorf("demo %s %v", out, err)
	}

	root, err := GitForgeTree(`{"kind":"demo"}`, demoClusterRepoURL, info.HeadSha, "")
	for _, want := range []string{`"path":"talos","type":"dir"`, `"path":"kubernetes","type":"dir"`, `"path":".sops.yaml"`, `"path":"README.md"`} {
		if err != nil || !strings.Contains(root, want) {
			t.Errorf("root lacks %s: %s %v", want, root, err)
		}
	}

	if out, err := GitForgeFile(`{"kind":"demo"}`, demoClusterRepoURL, info.HeadSha, "talos/talsecret.sops.yaml"); err != nil || !strings.Contains(out, "ENC[AES256_GCM") {
		t.Errorf("sops file %s %v", out, err)
	}

	opts := `{"repoURL":"` + demoClusterRepoURL + `","baseRef":"main","path":"README.md","content":"x","message":"m","title":"t"}`
	if _, err := GitForgePropose("", "", `{"kind":"demo"}`, opts); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Errorf("demo propose: %v", err)
	}
}

func TestGitForgeRepoProposalDefaults(t *testing.T) {
	fixedBranchSuffix(t)

	out, err := GitForgeRepoProposalDefaults("git@git.example.org:example-org/cluster-config.git", "/talos/nodes/cp-1.yaml", "lab")
	if err != nil {
		t.Fatal(err)
	}

	want := gitOpsProposal{
		Title: "Update talos/nodes/cp-1.yaml for lab", Message: "Update talos/nodes/cp-1.yaml for lab",
		Branch: "ichor/lab-cp-1-0001", Body: "Proposed from Ichor in the cluster repository example-org/cluster-config of lab.",
	}

	var got gitOpsProposal
	if err := json.Unmarshal([]byte(out), &got); err != nil || got != want {
		t.Errorf("got %+v %v, want %+v", got, err, want)
	}

	p, err := gitForgeRepoProposalDefaults(demoClusterRepoURL, "README.md", "")
	if err != nil || p.Title != "Update README.md" || p.Branch != "ichor/readme-0002" || strings.Contains(p.Body, " of ") {
		t.Errorf("no cluster: %+v %v", p, err)
	}

	for _, bad := range [][2]string{{"not a repo", "a.yaml"}, {demoClusterRepoURL, " / "}} {
		if _, err := GitForgeRepoProposalDefaults(bad[0], bad[1], "lab"); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestGitForgeRequestDemo(t *testing.T) {
	open := requestOf(t)(GitForgeRequest(`{"kind":"demo"}`, demoClusterRepoURL, 1))
	merged := requestOf(t)(GitForgeRequest(`{"kind":"demo"}`, demoClusterRepoURL, demoMergedRequest))

	if open.State != gitRequestOpen || merged.State != gitRequestMerged || merged.MergeSHA != demoArgoRevision {
		t.Errorf("demo %+v %+v", open, merged)
	}
}

func TestGitForgeProposePrivacy(t *testing.T) {
	t.Cleanup(func() { SetPrivacyMask(false, "") })
	SetPrivacyMask(true, "")

	host, repo := gitHostJSON("github", "https://git.example.org"), "https://git.example.org/example-org/platform"
	if _, err := GitForgePropose("", "lab", host, proposeOptions(t, repo, nil)); err == nil || !strings.Contains(err.Error(), "screenshot mode") {
		t.Errorf("privacy on: %v", err)
	}
}

func TestGitForgeProposeDemo(t *testing.T) {
	_, err := GitForgePropose("", "lab", `{"kind":"demo"}`, proposeOptions(t, "", nil))
	if err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Errorf("demo: %v", err)
	}
}

// The audit entry names the repository, the file and the request, never the token nor the
// content.
func TestGitForgeProposeAudit(t *testing.T) {
	withDataDir(t)
	fixedBranchSuffix(t)

	srv, _ := proposeServer(t, map[string][]proposeAnswer{
		"POST /api/v1/repos/example-org/platform/branches":                             {{201, `{}`}},
		"PUT /api/v1/repos/example-org/platform/contents/apps/podinfo/deployment.yaml": {{200, `{"commit":{"sha":"9173a"}}`}},
		"POST /api/v1/repos/example-org/platform/pulls":                                {{201, `{"html_url":"https://git.example.org/example-org/platform/pulls/3","number":3}`}},
	})

	if _, err := GitForgePropose("", "lab", gitHostJSON("gitea", srv.URL), proposeOptions(t, "https://127.0.0.1/example-org/platform", nil)); err != nil {
		t.Fatal(err)
	}

	entries := readAudit(t, "lab", "gitops-propose")
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	e := entries[0]
	if e.Object != "example-org/platform:apps/podinfo/deployment.yaml" || e.Params != "https://git.example.org/example-org/platform/pulls/3" || e.Outcome != auditOK {
		t.Errorf("entry %+v", e)
	}

	raw, _ := json.Marshal(entries) //nolint:errcheck
	if strings.Contains(string(raw), gitTestToken) || strings.Contains(string(raw), "podinfo:6.7.1") {
		t.Errorf("the token or the content is in the entry: %s", raw)
	}

	// A refused proposal is recorded too.
	if _, err := GitForgePropose("", "lab", gitHostJSON("gitea", srv.URL), proposeOptions(t, "https://127.0.0.1/example-org/platform", func(o map[string]any) { o["title"] = "" })); err == nil {
		t.Fatal("a proposal without a title was accepted")
	}

	if entries := readAudit(t, "lab", "gitops-propose"); len(entries) != 2 || entries[0].Outcome != auditFailed {
		t.Errorf("failed entry %+v", entries)
	}
}

// The audit entry of a multi-file proposal names every path.
func TestGitForgeProposeFilesAudit(t *testing.T) {
	withDataDir(t)
	fixedBranchSuffix(t)

	srv, _ := proposeServer(t, map[string][]proposeAnswer{
		"POST /api/v1/repos/example-org/platform/branches": {{201, `{}`}},
		"POST /api/v1/repos/example-org/platform/contents": {{201, `{"commit":{"sha":"6a7c4"}}`}},
		"POST /api/v1/repos/example-org/platform/pulls":    {{201, `{"html_url":"https://git.example.org/example-org/platform/pulls/4","number":4}`}},
	})

	if _, err := GitForgePropose("", "lab", gitHostJSON("gitea", srv.URL), proposeOptions(t, "https://127.0.0.1/example-org/platform", twoFiles)); err != nil {
		t.Fatal(err)
	}

	entries := readAudit(t, "lab", "gitops-propose")
	if len(entries) != 1 || entries[0].Object != "example-org/platform:apps/podinfo/deployment.yaml, apps/podinfo/kustomization.yaml" {
		t.Errorf("entries %+v", entries)
	}
}
