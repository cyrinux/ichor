package ichorgo

import "github.com/cyrinux/ichor/go/gitforge"

// The demo's GitOps sources: podinfo deployed from the demo fleet repository, whose files
// are the demo Git repository's (demo_gitforge.go).

const (
	demoGitOpsRepo   = "ssh://git@git.example.com/homelab/fleet.git"
	demoGitOpsCommit = "4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"
)

func demoGitOpsSource(tool string) gitopsSources {
	g := gitopsSource{
		RepoURL: demoGitOpsRepo, WebURL: gitforge.RepoWebURL(demoGitOpsRepo), Tracked: "main", Revision: demoGitOpsCommit,
		Path: "apps/podinfo", Kind: gitopsKindKustomize, ValueFiles: []string{}, Browsable: true,
	}

	if tool == "flux" {
		g.FluxArtifact = true
		g.FluxSource = &fluxRef{Kind: "GitRepository", Namespace: "flux-system", Name: "flux-system"}
	}

	return gitopsSources{Sources: []gitopsSource{g}}
}

func demoFluxTree() *fluxTree {
	t := &fluxTree{files: map[string][]byte{}, revision: "main@sha1:" + demoGitOpsCommit, commit: demoGitOpsCommit}
	for p, c := range demoGitForgeFiles {
		t.files[p] = []byte(c)
		t.size += int64(len(c))
	}

	return t
}
