package ichorgo

import (
	"cmp"
	"context"
	"strings"
	"sync"

	"github.com/cyrinux/ichor/go/internal/upstream"
)

const (
	argoImageUpdaterList     = "argocd-image-updater.argoproj.io/image-list"
	helmReleaseNameAnno      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnno = "meta.helm.sh/release-namespace"
	fluxHelmNameLabel        = "helm.toolkit.fluxcd.io/name"
	fluxHelmNamespaceLabel   = "helm.toolkit.fluxcd.io/namespace"
	fluxKustomizeNameLabel   = "kustomize.toolkit.fluxcd.io/name"
	fluxKustomizeNsLabel     = "kustomize.toolkit.fluxcd.io/namespace"
	updateRenderedHint       = "rendered by a chart: find the tag in the values"
)

// updateHelmReleaseObject holds the fields of a Flux HelmRelease the updates read.
type updateHelmReleaseObject struct {
	Metadata fluxMeta `json:"metadata"`
	Spec     struct {
		Chart *struct {
			Spec struct {
				Chart       string       `json:"chart"`
				Version     string       `json:"version"`
				SourceRef   fluxCrossRef `json:"sourceRef"`
				ValuesFiles []string     `json:"valuesFiles"`
			} `json:"spec"`
		} `json:"chart"`
		ChartRef         *fluxCrossRef `json:"chartRef"`
		ReleaseName      string        `json:"releaseName"`
		TargetNamespace  string        `json:"targetNamespace"`
		StorageNamespace string        `json:"storageNamespace"`
	} `json:"spec"`
	Status struct {
		LastAttemptedRevision string `json:"lastAttemptedRevision"`
		History               []struct {
			Name         string `json:"name"`
			Namespace    string `json:"namespace"`
			ChartVersion string `json:"chartVersion"`
		} `json:"history"`
	} `json:"status"`
}

// storageKey is namespace/name of the Helm release the helm-controller writes.
func (hr updateHelmReleaseObject) storageKey() string {
	if h := hr.Status.History; len(h) > 0 && h[0].Name != "" {
		return cmp.Or(h[0].Namespace, hr.Metadata.Namespace) + "/" + h[0].Name
	}

	name := hr.Metadata.Name
	if hr.Spec.TargetNamespace != "" {
		name = hr.Spec.TargetNamespace + "-" + name
	}

	return cmp.Or(hr.Spec.StorageNamespace, hr.Metadata.Namespace) + "/" + cmp.Or(hr.Spec.ReleaseName, name)
}

type updateImageObject struct {
	Metadata fluxMeta `json:"metadata"`
	Spec     struct {
		Image              string       `json:"image"`              // ImageRepository
		ImageRepositoryRef fluxCrossRef `json:"imageRepositoryRef"` // ImagePolicy
	} `json:"spec"`
}

// updateGitOps is what the GitOps tools say about the cluster's objects.
type updateGitOps struct {
	helmReleases []updateHelmReleaseObject
	sources      map[string]fluxSourceObject // Kind/namespace/name of HelmRepositories and OCIRepositories
	argoApps     []argoObject
	argoByObject map[string]int // Kind/namespace/name of a managed resource → index in argoApps
	// automatedRepos are the image repositories a Flux ImagePolicy follows, when an
	// ImageUpdateAutomation writes them back.
	automatedRepos map[string]bool
	skipped        []updateSkipped
}

func readUpdateGitOps(ctx context.Context, k *kubeClient, groups map[string]string) updateGitOps {
	g := updateGitOps{sources: map[string]fluxSourceObject{}, argoByObject: map[string]int{}, automatedRepos: map[string]bool{}}

	var (
		hrs                  kubeList[updateHelmReleaseObject]
		helmRepos, ociRepos  kubeList[fluxSourceObject]
		apps                 kubeList[argoObject]
		imageRepos, policies kubeList[updateImageObject]
		automations          kubeList[updateImageObject]
		mu                   sync.Mutex
		wg                   sync.WaitGroup
		path                 = func(group, plural string) string { return "/apis/" + group + "/" + groups[group] + "/" + plural }
		list                 = func(what, group, plural string, read func() error) {
			if _, ok := groups[group]; !ok {
				return
			}

			wg.Go(func() {
				if err := read(); err != nil && !isNotFound(err) {
					mu.Lock()
					g.skipped = append(g.skipped, updateSkipped{What: what, Reason: listReason(err)})
					mu.Unlock()
				}
			})
		}
	)

	list("Flux HelmReleases", groupFluxHelm, "helmreleases", func() error { return getList(ctx, k, path(groupFluxHelm, "helmreleases"), &hrs) })
	list("Flux HelmRepositories", groupFluxSource, "helmrepositories", func() error {
		return getList(ctx, k, path(groupFluxSource, "helmrepositories"), &helmRepos)
	})
	list("Flux OCIRepositories", groupFluxSource, "ocirepositories", func() error {
		return getList(ctx, k, path(groupFluxSource, "ocirepositories"), &ociRepos)
	})
	list("Argo CD Applications", groupArgo, "applications", func() error { return getList(ctx, k, path(groupArgo, "applications"), &apps) })
	list("Flux ImageRepositories", groupFluxImage, "imagerepositories", func() error {
		return getList(ctx, k, path(groupFluxImage, "imagerepositories"), &imageRepos)
	})
	list("Flux ImagePolicies", groupFluxImage, "imagepolicies", func() error { return getList(ctx, k, path(groupFluxImage, "imagepolicies"), &policies) })
	list("Flux ImageUpdateAutomations", groupFluxImage, "imageupdateautomations", func() error {
		return getList(ctx, k, path(groupFluxImage, "imageupdateautomations"), &automations)
	})
	wg.Wait()

	g.helmReleases = hrs.Items

	for _, s := range helmRepos.Items {
		g.sources["HelmRepository/"+s.Metadata.Namespace+"/"+s.Metadata.Name] = s
	}

	for _, s := range ociRepos.Items {
		g.sources["OCIRepository/"+s.Metadata.Namespace+"/"+s.Metadata.Name] = s
	}

	g.argoApps = apps.Items
	for i, app := range apps.Items {
		for _, res := range app.Status.Resources {
			g.argoByObject[res.Kind+"/"+res.Namespace+"/"+res.Name] = i
		}
	}

	if len(automations.Items) > 0 {
		byName := map[string]string{}
		for _, r := range imageRepos.Items {
			byName[r.Metadata.Namespace+"/"+r.Metadata.Name] = r.Spec.Image
		}

		for _, p := range policies.Items {
			ref := p.Spec.ImageRepositoryRef
			if image, ok := byName[cmp.Or(ref.Namespace, p.Metadata.Namespace)+"/"+ref.Name]; ok {
				g.automatedRepos[repoKey(image)] = true
			}
		}
	}

	return g
}

func listReason(err error) string {
	if isForbidden(err) {
		return "not allowed to list them"
	}

	return friendlyError(err)
}

// repoKey is an image repository as registry/path, docker.io and library/ made explicit.
func repoKey(image string) string {
	r := parseImageRef(image)

	return r.Registry + "/" + r.Path
}

// ownerOf says who applied a workload: the Argo CD app that manages it, the Flux
// HelmRelease or Kustomization whose labels it carries, or the Helm release it belongs to.
// rendered is true when a chart renders it (its image is set in the chart's values).
func (g updateGitOps) ownerOf(kind string, m fluxMeta) (owner *updateOwner, rendered bool, valueFiles []string) {
	if i, ok := g.argoByObject[kind+"/"+m.Namespace+"/"+m.Name]; ok {
		app := g.argoApps[i]
		owner = &updateOwner{Tool: "argocd", Kind: "Application", Namespace: app.Metadata.Namespace, Name: app.Metadata.Name}

		for _, s := range argoSourcesOf(app.Spec) {
			if s.Chart != "" || s.Helm != nil {
				rendered = true
				if s.Helm != nil {
					valueFiles = append(valueFiles, s.Helm.ValueFiles...)
				}
			}
		}

		return owner, rendered, valueFiles
	}

	if name := m.Labels[fluxHelmNameLabel]; name != "" {
		ns := cmp.Or(m.Labels[fluxHelmNamespaceLabel], m.Namespace)
		for _, hr := range g.helmReleases {
			if hr.Metadata.Name == name && hr.Metadata.Namespace == ns && hr.Spec.Chart != nil {
				valueFiles = hr.Spec.Chart.Spec.ValuesFiles
			}
		}

		return &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: ns, Name: name}, true, valueFiles
	}

	if name := m.Labels[fluxKustomizeNameLabel]; name != "" {
		return &updateOwner{Tool: "flux", Kind: "Kustomization", Namespace: cmp.Or(m.Labels[fluxKustomizeNsLabel], m.Namespace), Name: name}, false, nil
	}

	if name := m.Annotations[helmReleaseNameAnno]; name != "" {
		return &updateOwner{Tool: "helm", Kind: "Release", Namespace: cmp.Or(m.Annotations[helmReleaseNamespaceAnno], m.Namespace), Name: name}, true, nil
	}

	return nil, false, nil
}

// fluxChart is the chart a HelmRelease installs. A version range is Flux's own update
// policy: it is reported, never edited into a fixed version.
func (g updateGitOps) fluxChart(hr updateHelmReleaseObject) *updateChart {
	m := hr.Metadata
	ch := &updateChart{Owner: &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: m.Namespace, Name: m.Name}}

	current := ""
	if h := hr.Status.History; len(h) > 0 {
		current = h[0].ChartVersion
	}

	current = cmp.Or(current, hr.Status.LastAttemptedRevision)

	switch {
	case hr.Spec.Chart != nil:
		cs := hr.Spec.Chart.Spec
		ch.Name = cs.Chart

		src, ok := g.sources[cs.SourceRef.Kind+"/"+cmp.Or(cs.SourceRef.Namespace, m.Namespace)+"/"+cs.SourceRef.Name]
		switch {
		case cs.SourceRef.Kind != "HelmRepository":
			ch.Reason = "chart in a Git repository or bucket: it moves with that source"
		case ok:
			ch.RepoURL = fluxChartURL(src)
		}

		g.fluxVersion(ch, cs.Version, current, "spec.chart.spec.version", nil)
	case hr.Spec.ChartRef != nil && hr.Spec.ChartRef.Kind == "OCIRepository":
		ref := *hr.Spec.ChartRef
		ns := cmp.Or(ref.Namespace, m.Namespace)
		src, ok := g.sources["OCIRepository/"+ns+"/"+ref.Name]
		if !ok || src.Spec.Ref == nil {
			ch.Name, ch.Reason = ref.Name, "chart repository unknown"

			return ch
		}

		u := fluxChartURL(src)
		ch.RepoURL, ch.Name = u[:strings.LastIndex(u, "/")], u[strings.LastIndex(u, "/")+1:]

		revision := ""
		if src.Status.Artifact != nil {
			revision, _, _ = strings.Cut(src.Status.Artifact.Revision, "@")
		}

		wanted, path := src.Spec.Ref.Tag, "spec.ref.tag"
		if src.Spec.Ref.SemVer != "" {
			wanted, path = src.Spec.Ref.SemVer, ""
		}

		g.fluxVersion(ch, wanted, cmp.Or(revision, current), path, &updateEdit{Kind: "OCIRepository", Name: ref.Name})
	default:
		ch.Name, ch.Reason = m.Name, "chart from a HelmChart object: not looked up"
	}

	return ch
}

// fluxVersion sets the chart's version from what the manifest asks (wanted) and what runs.
func (g updateGitOps) fluxVersion(ch *updateChart, wanted, current, path string, target *updateEdit) {
	if wanted == "" || isVersionRange(wanted) {
		ch.Constraint, ch.Version, ch.AutomatedBy = cmp.Or(wanted, "*"), current, "flux-semver"

		return
	}

	ch.Version, ch.editPath, ch.editTarget = cmp.Or(current, wanted), path, target
}

// isVersionRange tells a semver constraint from a pinned version.
func isVersionRange(v string) bool {
	return strings.ContainsAny(v, "<>~^*|, ") || strings.HasSuffix(v, ".x") || strings.HasSuffix(v, ".X") || v == "x"
}

// markAutomation notes the images a Flux image automation or Argo CD Image Updater bumps.
func markAutomation(r *clusterUpdates, g updateGitOps) {
	for _, img := range r.Images {
		key := repoKey(img.ref)

		switch {
		case g.automatedRepos[key]:
			img.AutomatedBy = "flux-image-automation"
		case img.Owner != nil && img.Owner.Tool == "argocd" && g.argoUpdaterFollows(*img.Owner, key):
			img.AutomatedBy = "argocd-image-updater"
		}
	}
}

func (g updateGitOps) argoUpdaterFollows(owner updateOwner, key string) bool {
	for _, app := range g.argoApps {
		if app.Metadata.Namespace != owner.Namespace || app.Metadata.Name != owner.Name {
			continue
		}

		// "alias=repo:constraint, repo2": the repositories, without alias and constraint.
		for _, entry := range strings.Split(app.Metadata.Annotations[argoImageUpdaterList], ",") {
			entry = strings.TrimSpace(entry)
			if _, after, ok := strings.Cut(entry, "="); ok {
				entry = after
			}

			if entry != "" && repoKey(entry) == key {
				return true
			}
		}
	}

	return false
}

// imageEdit is the quick edit bumping img to its first candidate, nil when nothing to do or
// something already bumps it.
func imageEdit(img *updateImage) *updateEdit {
	next := firstCandidate(img.Candidate)
	if next == "" || img.AutomatedBy != "" || len(img.Workloads) == 0 {
		return nil
	}

	if img.rendered {
		return &updateEdit{Op: "path", Hint: updateRenderedHint, ValueFiles: img.valueFiles}
	}

	w := img.Workloads[0]

	return &updateEdit{Op: "image", Kind: w.Kind, Name: w.Name, Container: w.Containers[0], Image: img.Repo + ":" + next}
}

// chartEdit is the quick edit pinning the chart to its first candidate, nil for a range or
// a release no manifest declares.
func chartEdit(ch *updateChart) *updateEdit {
	next := firstCandidate(ch.Candidate)
	if next == "" || ch.editPath == "" || ch.AutomatedBy != "" {
		return nil
	}

	e := updateEdit{Op: "path", Path: ch.editPath, Value: next}
	if ch.editTarget != nil {
		e.Kind, e.Name = ch.editTarget.Kind, ch.editTarget.Name
	}

	return &e
}

// dropInRange clears the candidates constraint already accepts: the tool follows them alone.
func dropInRange(c *upstream.Candidate, constraint string) {
	for _, tag := range []*string{&c.Patch, &c.Minor, &c.Major} {
		if *tag != "" && satisfiesRange(constraint, *tag) {
			*tag = ""
		}
	}
}
