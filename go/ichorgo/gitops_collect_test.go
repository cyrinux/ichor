package ichorgo

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCollectGitOpsNeedsAdmin(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	target := kubeTarget{cfg, "", ""}

	if g := collectGitOps(context.Background(), target, []string{"os:reader"}); g != nil {
		t.Fatalf("reader got %+v", g)
	}

	g := collectGitOps(context.Background(), target, []string{adminRole})
	if g == nil || g.Argo == nil || g.Flux == nil || g.Note != "" {
		t.Fatalf("demo admin got %+v", g)
	}
}

func TestCollectGitOpsUnreachableLeavesANote(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	target := kubeTarget{f.kubeconfigFor("https://127.0.0.1:1"), "admin@test", ""}

	g := collectGitOps(context.Background(), target, []string{adminRole})
	if g == nil || g.Note == "" || g.Argo != nil {
		t.Fatalf("got %+v", g)
	}
}

func TestRenderGitOps(t *testing.T) {
	now := time.Now()
	argo, flux := demoArgoCD(now), demoFlux(now)

	var b strings.Builder
	renderGitOps(&b, &gitopsState{Argo: &argo, Flux: &flux})
	out := b.String()

	for _, want := range []string{
		"GITOPS\n",
		"Argo CD v3.4.5: 8 applications, 4 not synced and healthy",
		"argocd/grafana [critical]: health Healthy, sync OutOfSync",
		"last sync Failed: one or more objects failed to apply",
		"failed Deployment monitoring/grafana:",
		"condition SyncError:",
		"pod demo/worker-6f4b8-uvwxy CrashLoopBackOff on demo-worker-1, 14 restarts",
		"Flux ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}

	// Healthy apps are counted, not listed.
	if strings.Contains(out, "argocd/cilium") {
		t.Errorf("healthy app listed:\n%s", out)
	}

	b.Reset()
	renderGitOps(&b, &gitopsState{Note: "Kubernetes API: permission denied"})

	if b.String() != "GITOPS\n  could not be read: Kubernetes API: permission denied\n\n" {
		t.Errorf("note: %q", b.String())
	}

	b.Reset()
	renderGitOps(&b, nil)

	if b.Len() != 0 {
		t.Errorf("nil wrote %q", b.String())
	}
}

func TestRenderGitOpsCapsTheList(t *testing.T) {
	a := argoStatus{Version: "v3"}
	for range gitopsMaxIssues + 3 {
		a.Apps = append(a.Apps, argoApp{Namespace: "argocd", Name: "x", Level: healthWarning, Health: "Progressing", Sync: "OutOfSync"})
	}

	var b strings.Builder
	renderArgoIssues(&b, &a)

	if !strings.Contains(b.String(), "… 3 more") || strings.Count(b.String(), "argocd/x") != gitopsMaxIssues {
		t.Fatalf("got\n%s", b.String())
	}
}

func TestScrubGitOps(t *testing.T) {
	argo := argoStatus{Apps: []argoApp{{
		Sources:      []argoSrc{{Repo: "https://bot:s3cret@git.example.com/ops.git"}, {Repo: "git@git.example.com:ops.git"}},
		ExternalURLs: []string{"https://app.example.com"},
	}}}
	flux := fluxStatus{
		Apps:    []fluxApp{{SourceURL: "https://u:p@git.example.com/flux.git"}},
		Sources: []fluxSource{{URL: "oci://user:tok@ghcr.io/org/charts"}},
	}

	out := scrubGitOps(gitopsState{Argo: &argo, Flux: &flux})

	if got := out.Argo.Apps[0].Sources; got[0].Repo != "https://git.example.com/ops.git" || got[1].Repo != "git@git.example.com:ops.git" {
		t.Errorf("argo sources %+v", got)
	}

	if len(out.Argo.Apps[0].ExternalURLs) != 0 {
		t.Error("external URLs kept")
	}

	if out.Flux.Apps[0].SourceURL != "https://git.example.com/flux.git" || out.Flux.Sources[0].URL != "oci://ghcr.io/org/charts" {
		t.Errorf("flux %+v %+v", out.Flux.Apps[0], out.Flux.Sources[0])
	}

	// The input is not changed.
	if argo.Apps[0].Sources[0].Repo != "https://bot:s3cret@git.example.com/ops.git" || flux.Sources[0].URL != "oci://user:tok@ghcr.io/org/charts" {
		t.Error("scrub mutated its input")
	}
}

func TestCollectGitOpsOneToolUnreadable(t *testing.T) {
	// Flux is not installed; listing the Argo CD Applications is refused.
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
	})
	f.answers["GET /apis/argoproj.io/v1alpha1/applications"] = "{" // undecodable

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	g, err := readGitOps(context.Background(), k)
	if err != nil || g.ArgoError == "" || g.Argo != nil || g.Flux != nil || g.FluxError != "" {
		t.Fatalf("got %+v %v", g, err)
	}

	var b strings.Builder
	renderGitOps(&b, &g)

	if !strings.Contains(b.String(), "  Argo CD could not be read: ") {
		t.Fatalf("report %q", b.String())
	}
}
