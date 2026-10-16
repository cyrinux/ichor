package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

// Where an Argo CD or Flux app's files are: the repository, the revision the cluster runs,
// the directory and the kind of source, so the apps can browse them through the Git host or,
// for Flux, from the artifact the source-controller already holds (kube_gitops_tree.go).

// The kind of a source: kustomize and helm are declared; directory is a plain path whose
// tool Argo CD detects from its files; chart is a packaged Helm chart (no Git tree); values
// is an Argo CD source only read for its value files ($ref/values.yaml).
const (
	gitopsKindKustomize = "kustomize"
	gitopsKindHelm      = "helm"
	gitopsKindDirectory = "directory"
	gitopsKindChart     = "chart"
	gitopsKindValues    = "values"
)

type gitopsSource struct {
	RepoURL string `json:"repoURL"`
	// WebURL is the repository's page, "" when it is not a Git repository on a web host.
	WebURL string `json:"webURL"`
	// Tracked is the branch, tag, semver range or commit the app follows.
	Tracked string `json:"tracked"`
	// Revision is the commit the cluster runs, "" when unknown or not a commit.
	Revision string `json:"revision"`
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Chart    string `json:"chart"`
	// ValueFiles are relative to the repository root, or Argo CD's "$ref/…" form.
	ValueFiles []string `json:"valueFiles"`
	// Browsable: the files can be read through the Git host's API.
	Browsable bool `json:"browsable"`
	// FluxArtifact: the files can be read from FluxSource's artifact, with no Git token.
	FluxArtifact bool     `json:"fluxArtifact"`
	FluxSource   *fluxRef `json:"fluxSource,omitempty"`
}

type gitopsSources struct {
	Sources []gitopsSource `json:"sources"`
	Note    string         `json:"note"`
}

var errGitOpsSourceKind = errors.New("only an Argo CD Application or a Flux Kustomization or HelmRelease has a Git source")

// GitOpsSource tells where the files of an app are: tool argocd (kind Application) or flux
// (kind Kustomization or HelmRelease). {"sources":[{repoURL, webURL, tracked, revision,
// path, kind, chart, valueFiles, browsable, fluxArtifact, fluxSource}], "note"}.
// kubeServer: see KubePods.
func GitOpsSource(configYAML, contextName, kubeServer, tool, kind, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	switch {
	case tool == "argocd" && kind == "Application":
	case tool == "flux" && (kind == "Kustomization" || kind == "HelmRelease"):
	default:
		return "", errGitOpsSourceKind
	}

	if err := validateKubeName("app", namespace, name); err != nil {
		return "", err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}

	return kubeReadJSON(target, func() gitopsSources { return demoGitOpsSource(tool) }, func(ctx context.Context, k *kubeClient) (gitopsSources, error) {
		if tool == "argocd" {
			return readArgoSources(ctx, k, namespace, name)
		}

		return readFluxSources(ctx, k, kind, namespace, name)
	})
}

func readArgoSources(ctx context.Context, k *kubeClient, namespace, name string) (gitopsSources, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return gitopsSources{}, err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return gitopsSources{}, errors.New("the cluster runs no Argo CD")
	}

	var o argoObject
	if err := k.get(ctx, "/apis/"+groupArgo+"/"+version+"/namespaces/"+url.PathEscape(namespace)+"/applications/"+url.PathEscape(name), &o); err != nil {
		return gitopsSources{}, err
	}

	return gitopsSources{Sources: argoGitOpsSources(o)}, nil
}

// argoGitOpsSources maps each source of an Application. status.sync.revisions pairs with
// spec.sources by position; a single source has status.sync.revision. A chart source's
// revision is a chart version, not a commit, so it is left out.
func argoGitOpsSources(o argoObject) []gitopsSource {
	sources := argoSourcesOf(o.Spec)

	revisions := o.Status.Sync.Revisions
	if len(o.Spec.Sources) == 0 {
		revisions = []string{o.Status.Sync.Revision}
	}

	out := make([]gitopsSource, 0, len(sources))

	for i, s := range sources {
		g := gitopsSource{
			RepoURL: s.RepoURL, WebURL: argoRepoWebURL(s.RepoURL), Tracked: cmp.Or(s.TargetRevision, "HEAD"),
			Path: cleanSourcePath(s.Path), Chart: s.Chart, ValueFiles: []string{},
		}

		switch {
		case s.Chart != "":
			g.Kind = gitopsKindChart
		case s.Ref != "" && s.Path == "":
			g.Kind = gitopsKindValues
		case s.Helm != nil:
			g.Kind = gitopsKindHelm
		case len(s.Kustomize) > 0 && string(s.Kustomize) != "null":
			g.Kind = gitopsKindKustomize
		default:
			g.Kind = gitopsKindDirectory
		}

		if s.Helm != nil {
			g.ValueFiles = argoValueFiles(g.Path, s.Helm.ValueFiles)
		}

		if s.Chart == "" && i < len(revisions) && gitCommitSHA.MatchString(revisions[i]) {
			g.Revision = revisions[i]
		}

		g.Browsable = s.Chart == "" && g.WebURL != ""
		out = append(out, g)
	}

	return out
}

// argoValueFiles makes Argo CD's value files (relative to the source path) relative to the
// repository root; "$ref/…" and URLs are kept.
func argoValueFiles(dir string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasPrefix(f, "$") || strings.Contains(f, "://") {
			out = append(out, f)

			continue
		}

		out = append(out, cleanSourcePath(path.Join(dir, f)))
	}

	return out
}

func readFluxSources(ctx context.Context, k *kubeClient, kind, namespace, name string) (gitopsSources, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return gitopsSources{}, err
	}

	objPath, err := fluxPath(groups, kind, namespace, name)
	if err != nil {
		return gitopsSources{}, err
	}

	if kind == "Kustomization" {
		var ks fluxKustomizationObject
		if err := k.get(ctx, objPath, &ks); err != nil {
			return gitopsSources{}, err
		}

		ref := ks.Spec.SourceRef
		ref.Namespace = cmp.Or(ref.Namespace, namespace)

		g, err := fluxGitOpsSource(ctx, k, groups, ref)
		if err != nil {
			return gitopsSources{}, err
		}

		g.Path, g.Kind = cleanSourcePath(ks.Spec.Path), gitopsKindKustomize

		return gitopsSources{Sources: []gitopsSource{g}}, nil
	}

	var hr fluxHelmReleaseObject
	if err := k.get(ctx, objPath, &hr); err != nil {
		return gitopsSources{}, err
	}

	g, err := fluxHelmReleaseSource(ctx, k, groups, hr, namespace)
	if err != nil {
		return gitopsSources{}, err
	}

	return gitopsSources{Sources: []gitopsSource{g}}, nil
}

// fluxHelmReleaseSource maps a HelmRelease's chart: from a GitRepository or Bucket it is a
// directory of the tree (browsable like a Kustomization's), from a Helm repository or an
// OCI artifact it is a packaged chart.
func fluxHelmReleaseSource(ctx context.Context, k *kubeClient, groups map[string]string, hr fluxHelmReleaseObject, namespace string) (gitopsSource, error) {
	switch {
	case hr.Spec.Chart != nil:
		cs := hr.Spec.Chart.Spec
		ref := cs.SourceRef
		ref.Namespace = cmp.Or(ref.Namespace, namespace)

		g, err := fluxGitOpsSource(ctx, k, groups, ref)
		if err != nil {
			return gitopsSource{}, err
		}

		g.ValueFiles = orEmpty(cs.ValuesFiles)

		if ref.Kind == "GitRepository" || ref.Kind == "Bucket" {
			g.Path, g.Kind = cleanSourcePath(cs.Chart), gitopsKindHelm

			return g, nil
		}

		// A HelmRepository's artifact is its index, not the chart.
		g.Kind, g.Chart, g.Tracked, g.Revision = gitopsKindChart, cs.Chart, cmp.Or(cs.Version, "*"), ""
		g.Browsable, g.FluxArtifact, g.FluxSource = false, false, nil

		return g, nil
	case hr.Spec.ChartRef != nil:
		ref := *hr.Spec.ChartRef
		ref.Namespace = cmp.Or(ref.Namespace, namespace)

		g, err := fluxGitOpsSource(ctx, k, groups, ref)
		if err != nil {
			return gitopsSource{}, err
		}

		g.Kind, g.Browsable = gitopsKindChart, false

		// A HelmChart's artifact is a packaged chart the tree reader does not open.
		if ref.Kind == "HelmChart" {
			g.FluxArtifact, g.FluxSource = false, nil
		}

		return g, nil
	}

	return gitopsSource{}, errFluxNoSource
}

// fluxGitOpsSource reads the Flux source ref: its URL, the ref it follows and the revision
// of its artifact.
func fluxGitOpsSource(ctx context.Context, k *kubeClient, groups map[string]string, ref fluxCrossRef) (gitopsSource, error) {
	p, err := fluxPath(groups, ref.Kind, ref.Namespace, ref.Name)
	if err != nil {
		return gitopsSource{}, err
	}

	var src fluxSourceObject
	if err := k.get(ctx, p, &src); err != nil {
		return gitopsSource{}, fmt.Errorf("%s %s: %w", ref.Kind, ref.Name, err)
	}

	g := gitopsSource{
		RepoURL: src.Spec.URL, ValueFiles: []string{},
		FluxSource: &fluxRef{Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name},
	}

	if ref.Kind == "Bucket" {
		g.RepoURL = strings.TrimSuffix(src.Spec.Endpoint, "/") + "/" + src.Spec.BucketName
	}

	r := src.Spec.Ref
	if r == nil {
		r = &fluxFollow{}
	}

	switch ref.Kind {
	case "GitRepository":
		// Flux's precedence: commit, name, semver, tag, branch (master by default).
		g.Tracked = cmp.Or(r.Commit, r.Name, r.SemVer, r.Tag, r.Branch, "master")
		g.WebURL = argoRepoWebURL(src.Spec.URL)
		g.Browsable = g.WebURL != ""
	case "OCIRepository":
		g.Tracked = cmp.Or(r.Digest, r.SemVer, r.Tag, "latest")
	case "HelmChart":
		g.RepoURL, g.Chart, g.Tracked = "", src.Spec.Chart, cmp.Or(src.Spec.Version, "*")
	}

	if a := src.Status.Artifact; a != nil {
		g.FluxArtifact = true

		// Only a Git artifact's revision names a commit; others are digests or versions.
		if ref.Kind == "GitRepository" {
			g.Revision = fluxCommitOf(a.Revision)
		}
	}

	return g, nil
}

// fluxCommitOf is the commit of an artifact revision: "main@sha1:<sha>", "v1.2.3@sha1:<sha>",
// "sha1:<sha>" or the legacy "main/<sha>"; "" for anything else (an OCI digest, a chart
// version).
func fluxCommitOf(revision string) string {
	if _, sha, ok := strings.Cut(revision, "sha1:"); ok && gitCommitSHA.MatchString(sha) {
		return sha
	}

	// The legacy form names a SHA-1 commit: 40 characters, never a sha256 digest.
	if i := strings.LastIndex(revision, "/"); i >= 0 && len(revision)-i-1 == 40 && gitCommitSHA.MatchString(revision[i+1:]) {
		return revision[i+1:]
	}

	return ""
}

// cleanSourcePath is a source directory relative to the repository root: "./apps/x/" is
// "apps/x", "./" is "".
func cleanSourcePath(p string) string {
	return strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(p)), "/")
}
