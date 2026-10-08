package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// GitOps (Argo CD and Flux) in the AI diagnosis and the support bundle: the apps that are not
// fine, read through the Kubernetes API with the os:admin kubeconfig. Best effort: a role
// without os:admin, or an API server out of reach, leaves a note instead of failing the report.

const (
	// gitopsCollectTimeout bounds the Kubernetes reads, so a slow API server does not hold the
	// whole diagnosis back.
	gitopsCollectTimeout = 20 * time.Second
	// gitopsMaxIssues caps the apps listed in the report.
	gitopsMaxIssues = 15
	adminRole       = "os:admin"
)

// gitopsState is what the cluster's GitOps tools said, nil sections when not installed.
type gitopsState struct {
	Argo *argoStatus `json:"argocd,omitempty"`
	Flux *fluxStatus `json:"flux,omitempty"`
	// Note says why nothing could be read ("" when it was).
	Note string `json:"note,omitempty"`
	// ArgoError / FluxError: that tool could not be read while the other was.
	ArgoError string `json:"argocdError,omitempty"`
	FluxError string `json:"fluxError,omitempty"`
}

// collectGitOps reads Argo CD and Flux, when the Talos roles allow the Kubernetes API.
func collectGitOps(ctx context.Context, target kubeTarget, roles []string) *gitopsState {
	if !slices.Contains(roles, adminRole) {
		return nil
	}

	return gitopsStateOf(ctx, target)
}

// gitopsStateOf reads Argo CD and Flux through the Kubernetes API: nil when neither runs, a
// Note when they could not be read.
func gitopsStateOf(ctx context.Context, target kubeTarget) *gitopsState {
	if isDemoContext(target.config, target.context) {
		argo, flux := demoArgoCD(time.Now()), demoFlux(time.Now())

		return &gitopsState{Argo: &argo, Flux: &flux}
	}

	ctx, cancel := context.WithTimeout(ctx, gitopsCollectTimeout)
	defer cancel()

	state, err := withKubeContext(ctx, target, readGitOps)
	if err != nil {
		return &gitopsState{Note: kubeError(err).Error()}
	}

	if state.Argo == nil && state.Flux == nil && state.ArgoError == "" && state.FluxError == "" {
		return nil
	}

	return &state
}

// readGitOps reads Argo CD and Flux side by side; one that fails while the other answers is
// noted, both failing is an error.
func readGitOps(ctx context.Context, k *kubeClient) (gitopsState, error) {
	var (
		out          gitopsState
		argo         argoStatus
		flux         fluxStatus
		argoE, fluxE error
		wg           sync.WaitGroup
	)

	wg.Go(func() { argo, argoE = readArgoCD(ctx, k) })
	wg.Go(func() { flux, fluxE = readFlux(ctx, k) })
	wg.Wait()

	if argoE == nil && argo.Installed {
		out.Argo = &argo
	}

	if fluxE == nil && flux.Installed {
		out.Flux = &flux
	}

	switch {
	case argoE != nil && fluxE != nil:
		return out, argoE
	case argoE != nil:
		out.ArgoError = kubeError(argoE).Error()
	case fluxE != nil:
		out.FluxError = kubeError(fluxE).Error()
	}

	return out, nil
}

// renderGitOps writes the GitOps section of the diagnosis: a count per tool, then the apps
// that are not fine, worst first, with what explains them.
func renderGitOps(b *strings.Builder, g *gitopsState) {
	if g == nil {
		return
	}

	b.WriteString("GITOPS\n")

	if g.Note != "" {
		fmt.Fprintf(b, "  could not be read: %s\n\n", g.Note)

		return
	}

	if a := g.Argo; a != nil {
		renderArgoIssues(b, a)
	}

	if g.ArgoError != "" {
		fmt.Fprintf(b, "  Argo CD could not be read: %s\n", g.ArgoError)
	}

	if f := g.Flux; f != nil {
		renderFluxIssues(b, f)
	}

	if g.FluxError != "" {
		fmt.Fprintf(b, "  Flux could not be read: %s\n", g.FluxError)
	}

	b.WriteString("\n")
}

func renderArgoIssues(b *strings.Builder, a *argoStatus) {
	var bad []argoApp

	for _, app := range a.Apps {
		if app.Level == healthCritical || app.Level == healthWarning {
			bad = append(bad, app)
		}
	}

	fmt.Fprintf(b, "  Argo CD %s: %d applications, %d not synced and healthy\n", a.Version, len(a.Apps), len(bad))

	for i, app := range bad {
		if i == gitopsMaxIssues {
			fmt.Fprintf(b, "    … %d more\n", len(bad)-i)

			break
		}

		fmt.Fprintf(b, "    %s/%s [%s]: health %s, sync %s", app.Namespace, app.Name, app.Level, app.Health, app.Sync)

		if app.HealthMessage != "" {
			fmt.Fprintf(b, " (%s)", clipUTF8(app.HealthMessage, diagnosisMaxLineLen))
		}

		b.WriteString("\n")

		if op := app.Operation; op != nil && (op.Phase == "Failed" || op.Phase == "Error" || op.Phase == "Running") {
			fmt.Fprintf(b, "      last sync %s: %s\n", op.Phase, clipUTF8(op.Message, diagnosisMaxLineLen))

			for _, f := range op.Failed {
				fmt.Fprintf(b, "      failed %s %s/%s: %s\n", f.Kind, f.Namespace, f.Name, clipUTF8(f.Message, diagnosisMaxLineLen))
			}
		}

		for _, c := range app.Conditions {
			fmt.Fprintf(b, "      condition %s: %s\n", c.Type, clipUTF8(c.Message, diagnosisMaxLineLen))
		}

		renderGitOpsPods(b, app.UnhealthyPods)
	}
}

func renderFluxIssues(b *strings.Builder, f *fluxStatus) {
	var bad []fluxApp

	for _, app := range f.Apps {
		if app.Level == healthCritical || app.Level == healthWarning {
			bad = append(bad, app)
		}
	}

	fmt.Fprintf(b, "  Flux %s: %d Kustomizations and HelmReleases, %d not ready\n", f.Version, len(f.Apps), len(bad))

	for i, app := range bad {
		if i == gitopsMaxIssues {
			fmt.Fprintf(b, "    … %d more\n", len(bad)-i)

			break
		}

		fmt.Fprintf(b, "    %s %s/%s [%s]: Ready=%s %s", app.Kind, app.Namespace, app.Name, app.Level, app.Ready, app.Reason)

		if app.Stalled {
			b.WriteString(", stalled")
		}

		if app.Failures > 0 {
			fmt.Fprintf(b, ", %d failures in a row", app.Failures)
		}

		b.WriteString("\n")

		if app.Message != "" {
			fmt.Fprintf(b, "      %s\n", clipUTF8(app.Message, diagnosisMaxLineLen))
		}

		renderGitOpsPods(b, app.UnhealthyPods)
	}
}

func renderGitOpsPods(b *strings.Builder, pods []kubePod) {
	for i, p := range pods {
		if i == 5 {
			fmt.Fprintf(b, "      … %d more pods not ready\n", len(pods)-i)

			break
		}

		fmt.Fprintf(b, "      pod %s/%s %s on %s, %d restarts\n", p.Namespace, p.Name, p.Status, cmp.Or(p.Node, "no node"), p.Restarts)
	}
}

// scrubGitOps drops what could hold a credential before the state goes into a support
// bundle: the user and password part of repository URLs, and the apps' external URLs.
func scrubGitOps(g gitopsState) gitopsState {
	if g.Argo != nil {
		a := *g.Argo
		a.Apps = slices.Clone(a.Apps)

		for i := range a.Apps {
			app := &a.Apps[i]
			app.Sources = slices.Clone(app.Sources)

			for j := range app.Sources {
				app.Sources[j].Repo = stripURLUser(app.Sources[j].Repo)
			}

			app.ExternalURLs = []string{}
		}

		g.Argo = &a
	}

	if g.Flux != nil {
		f := *g.Flux
		f.Apps = slices.Clone(f.Apps)

		for i := range f.Apps {
			f.Apps[i].SourceURL = stripURLUser(f.Apps[i].SourceURL)
		}

		f.Sources = slices.Clone(f.Sources)
		for i := range f.Sources {
			f.Sources[i].URL = stripURLUser(f.Sources[i].URL)
		}

		g.Flux = &f
	}

	return g
}

// stripURLUser removes "user:password@" from a URL; "git@host:path" (SSH) keeps its user,
// which is no secret.
func stripURLUser(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}

	u.User = nil

	return u.String()
}
