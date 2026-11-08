package ichorgo

import "testing"

func runsOfJSON(t *testing.T) func(string, error) revisionRuns { return decodeAnswer[revisionRuns](t) }

func TestGitOpsRevisionRunsArgo(t *testing.T) {
	const app = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/shop"

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": argoFollowGroups,
		"GET " + app: `{"status":{"sync":{"status":"Synced","revision":"aaaaaaa1111111111111111111111111111111111"},
			"history":[{"revision":"bbbbbbb2222222222222222222222222222222222"},{"revision":"aaaaaaa1111111111111111111111111111111111"}]}}`,
	})
	cfg := f.kubeconfigFor(f.URL)

	runs := runsOfJSON(t)(GitOpsRevisionRuns(cfg, "admin@test", "", "argocd", "Application", "argocd", "shop", "AAAAAAA1"))
	if !runs.Runs || !runs.Synced || runs.Revision != "aaaaaaa1111111111111111111111111111111111" {
		t.Errorf("runs %+v", runs)
	}

	// An older commit, now replaced: a newer one is deployed.
	older := runsOfJSON(t)(GitOpsRevisionRuns(cfg, "admin@test", "", "argocd", "Application", "argocd", "shop", "bbbbbbb2222"))
	if older.Runs || older.Revision == "" {
		t.Errorf("older %+v", older)
	}

	if _, err := GitOpsRevisionRuns(cfg, "admin@test", "", "argocd", "Application", "argocd", "shop", " "); err != errRevisionSHA {
		t.Errorf("no sha: %v", err)
	}

	if _, err := GitOpsRevisionRuns(cfg, "admin@test", "", "jenkins", "Job", "ci", "x", "abc1234"); err == nil {
		t.Error("unknown tool accepted")
	}
}

func TestGitOpsRevisionRunsFlux(t *testing.T) {
	const ks = "/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/apps"

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + ks: `{"status":{"lastAppliedRevision":"main@sha1:ccccccc3333333333333333333333333333333333",
			"lastAttemptedRevision":"main@sha1:ccccccc3333333333333333333333333333333333",
			"conditions":[{"type":"Ready","status":"True"}]}}`,
	})
	cfg := f.kubeconfigFor(f.URL)

	runs := runsOfJSON(t)(GitOpsRevisionRuns(cfg, "admin@test", "", "flux", "Kustomization", "flux-system", "apps", "ccccccc3333333333333333333333333333333333"))
	if !runs.Runs || !runs.Synced {
		t.Errorf("flux runs %+v", runs)
	}

	other := runsOfJSON(t)(GitOpsRevisionRuns(cfg, "admin@test", "", "flux", "Kustomization", "flux-system", "apps", "ddddddd4444"))
	if other.Runs || other.Revision != "ccccccc3333333333333333333333333333333333" {
		t.Errorf("flux other %+v", other)
	}
}

func TestRevisionCommit(t *testing.T) {
	for in, want := range map[string]string{
		"main@sha1:ABCDEF0123": "abcdef0123",
		"abcdef0":              "abcdef0",
		"8.5.2":                "",
		"v1.18.2":              "",
		"":                     "",
	} {
		if got := revisionCommit(in); got != want {
			t.Errorf("revisionCommit(%q) = %q, want %q", in, got, want)
		}
	}

	if sameCommit("abc", "abcdef0") || !sameCommit("abcdef0", "abcdef0123") {
		t.Error("sameCommit")
	}
}

func TestGitOpsRevisionRunsDemo(t *testing.T) {
	demo := demoConfigForTest(t)

	runs := runsOfJSON(t)(GitOpsRevisionRuns(demo, "", "", "argocd", "Application", "argocd", "hello-ichor", demoArgoRevision))
	if !runs.Runs {
		t.Errorf("demo %+v", runs)
	}
}
