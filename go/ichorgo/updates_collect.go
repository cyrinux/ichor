package ichorgo

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/internal/upstream"
	"github.com/cyrinux/ichor/go/kube/helm"
)

type updatePodSpec struct {
	Containers       []updateContainer `json:"containers"`
	InitContainers   []updateContainer `json:"initContainers"`
	ImagePullSecrets []struct {
		Name string `json:"name"`
	} `json:"imagePullSecrets"`
}

type updateContainer struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// updateWorkloadObject holds the fields of a Deployment, StatefulSet, DaemonSet or CronJob
// the updates read: its images, pull secrets, and the marks of who applied it.
type updateWorkloadObject struct {
	Metadata fluxMeta `json:"metadata"`
	Spec     struct {
		Template struct {
			Spec updatePodSpec `json:"spec"`
		} `json:"template"`
		JobTemplate struct {
			Spec struct {
				Template struct {
					Spec updatePodSpec `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"jobTemplate"`
	} `json:"spec"`
}

func (o updateWorkloadObject) podSpec(kind string) updatePodSpec {
	if kind == "CronJob" {
		return o.Spec.JobTemplate.Spec.Template.Spec
	}

	return o.Spec.Template.Spec
}

var updateWorkloadKinds = []struct{ kind, path string }{
	{"Deployment", "/apis/apps/v1/deployments"},
	{"StatefulSet", "/apis/apps/v1/statefulsets"},
	{"DaemonSet", "/apis/apps/v1/daemonsets"},
	{"CronJob", "/apis/batch/v1/cronjobs"},
}

// updateWorkload is one workload as read, with its kind.
type updateWorkload struct {
	kind string
	obj  updateWorkloadObject
}

// collectUpdates reads what the lookups need: the workloads, the GitOps objects that own
// them and the charts, then the pull secrets. Every read is optional: what fails is skipped.
func collectUpdates(ctx context.Context, k *kubeClient, opts updateOptions) (clusterUpdates, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return clusterUpdates{}, err
	}

	r := clusterUpdates{At: time.Now().UnixMilli(), Images: []*updateImage{}, Charts: []*updateChart{}, Skipped: []updateSkipped{}}

	var (
		lists = make([][]updateWorkload, len(updateWorkloadKinds))
		errs  = make([]error, len(updateWorkloadKinds))
		g     updateGitOps
		wg    sync.WaitGroup
	)

	for i, w := range updateWorkloadKinds {
		wg.Go(func() {
			var list kubeList[updateWorkloadObject]
			if errs[i] = getList(ctx, k, w.path, &list); errs[i] == nil {
				for _, o := range list.Items {
					lists[i] = append(lists[i], updateWorkload{kind: w.kind, obj: o})
				}
			}
		})
	}

	wg.Go(func() { g = readUpdateGitOps(ctx, k, groups) })
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			r.skip(updateWorkloadKinds[i].kind+"s", e)
		}
	}

	r.Skipped = append(r.Skipped, g.skipped...)
	r.Images = collectImages(slices.Concat(lists...), g, opts)
	r.Charts = collectCharts(ctx, k, g, opts, &r)
	r.auth = readUpdateCredentials(ctx, k, r.Images)

	markAutomation(&r, g)

	return r, nil
}

func (r *clusterUpdates) skip(what string, err error) {
	reason := friendlyError(err)
	switch {
	case isForbidden(err):
		reason = "not allowed to list them"
	case isNotFound(err):
		reason = "not served by this cluster"
	}

	r.Skipped = append(r.Skipped, updateSkipped{What: what, Reason: reason})
}

// collectImages lists every image of the workloads once, with the workloads running it and
// their pull secrets, in the order first met.
func collectImages(workloads []updateWorkload, g updateGitOps, opts updateOptions) []*updateImage {
	out := []*updateImage{}
	index := map[string]*updateImage{}

	for _, w := range workloads {
		m := w.obj.Metadata
		if !opts.wants(m.Namespace) {
			continue
		}

		spec := w.obj.podSpec(w.kind)
		owner, rendered, valueFiles := g.ownerOf(w.kind, m)

		for _, c := range slices.Concat(spec.Containers, spec.InitContainers) {
			if c.Image == "" {
				continue
			}

			img, ok := index[c.Image]
			if !ok {
				ref := parseImageRef(c.Image)
				img = &updateImage{ref: c.Image, Repo: imageRepo(c.Image), Tag: ref.Tag, Digest: ref.Digest, Workloads: []updateWorkloadRef{}, Owner: owner, rendered: rendered, valueFiles: valueFiles}
				index[c.Image] = img
				out = append(out, img)
			}

			img.addWorkload(w.kind, m.Namespace, m.Name, c.Name)

			for _, s := range spec.ImagePullSecrets {
				img.secrets = appendNew(img.secrets, m.Namespace+"/"+s.Name)
			}
		}
	}

	return out
}

func (img *updateImage) addWorkload(kind, namespace, name, container string) {
	for i, w := range img.Workloads {
		if w.Kind == kind && w.Namespace == namespace && w.Name == name {
			img.Workloads[i].Containers = appendNew(w.Containers, container)

			return
		}
	}

	if len(img.Workloads) >= updatesMaxWorkloads {
		img.More++

		return
	}

	img.Workloads = append(img.Workloads, updateWorkloadRef{Kind: kind, Namespace: namespace, Name: name, Containers: []string{container}})
}

// collectCharts lists the charts of the Flux HelmReleases, the Argo CD apps with a chart
// source and the Helm releases neither owns.
func collectCharts(ctx context.Context, k *kubeClient, g updateGitOps, opts updateOptions, r *clusterUpdates) []*updateChart {
	out := []*updateChart{}
	fluxReleases := map[string]bool{}

	for _, hr := range g.helmReleases {
		m := hr.Metadata
		fluxReleases[hr.storageKey()] = true

		if !opts.wants(m.Namespace) {
			continue
		}

		if ch := g.fluxChart(hr); ch != nil {
			out = append(out, ch)
		}
	}

	for _, app := range g.argoApps {
		if opts.wants(app.Metadata.Namespace) {
			out = append(out, argoCharts(app)...)
		}
	}

	refs, err := helm.SecretRefs(ctx, k.api(), "", "")
	if err != nil {
		r.skip("Helm releases", err)

		return out
	}

	for _, ref := range helm.LatestRevisions(refs) {
		if fluxReleases[ref.Namespace+"/"+ref.Name] || !opts.wants(ref.Namespace) {
			continue
		}

		rel, err := helm.ReadRelease(ctx, k.api(), ref)
		if err != nil {
			r.Skipped = append(r.Skipped, updateSkipped{What: "Helm release " + ref.Namespace + "/" + ref.Name, Reason: friendlyError(err)})

			continue
		}

		s := helm.Summarize(rel, ref)
		out = append(out, &updateChart{
			Name: s.Chart, Version: s.ChartVersion,
			Owner: &updateOwner{Tool: "helm", Kind: "Release", Namespace: ref.Namespace, Name: ref.Name},
		})
	}

	return out
}

// argoCharts are the chart sources of an Argo CD app: a pinned targetRevision is edited in
// place, a range is the version policy and only reports what it leaves out.
func argoCharts(app argoObject) []*updateChart {
	var out []*updateChart

	sources := argoSourcesOf(app.Spec)
	for i, s := range sources {
		if s.Chart == "" {
			continue
		}

		ch := &updateChart{
			Name: s.Chart, RepoURL: argoChartRepo(s.RepoURL),
			Owner: &updateOwner{Tool: "argocd", Kind: "Application", Namespace: app.Metadata.Namespace, Name: app.Metadata.Name},
		}

		ch.Version = s.TargetRevision
		if isVersionRange(s.TargetRevision) {
			ch.Constraint, ch.Version = s.TargetRevision, argoSyncedRevision(app, i, len(sources))
		} else if len(app.Spec.Sources) > 0 {
			ch.editPath = "spec.sources." + strconv.Itoa(i) + ".targetRevision"
		} else {
			ch.editPath = "spec.source.targetRevision"
		}

		out = append(out, ch)
	}

	return out
}

// argoChartRepo is an Argo CD chart repoURL as the lookup takes it: Argo CD names an OCI
// registry without a scheme.
func argoChartRepo(repo string) string {
	if strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "oci://") {
		return repo
	}

	return "oci://" + repo
}

func argoSyncedRevision(app argoObject, i, n int) string {
	if n > 1 || len(app.Spec.Sources) > 0 {
		if i < len(app.Status.Sync.Revisions) {
			return app.Status.Sync.Revisions[i]
		}

		return ""
	}

	return app.Status.Sync.Revision
}

// readUpdateCredentials reads the pull secrets of the images, one namespace at a time so a
// namespace the role cannot read only marks its own images.
func readUpdateCredentials(ctx context.Context, k *kubeClient, images []*updateImage) upstream.Auth {
	auth := upstream.Auth{Auths: map[string]upstream.AuthEntry{}}
	byNamespace := map[string][]string{}
	want := map[string]bool{}

	for _, img := range images {
		for _, s := range img.secrets {
			ns, _, _ := strings.Cut(s, "/")
			byNamespace[ns] = appendNew(byNamespace[ns], s)
		}

		want[parseImageRef(img.ref).Registry] = true
	}

	unreadable := map[string]bool{}

	for _, ns := range slices.Sorted(maps.Keys(byNamespace)) {
		data, _, err := readPullCredentials(ctx, k, byNamespace[ns], want)
		if err != nil {
			unreadable[ns] = true

			continue
		}

		var cfg struct {
			Auths map[string]upstream.AuthEntry `json:"auths"`
		}

		if data != nil && json.Unmarshal(data, &cfg) == nil {
			for host, e := range cfg.Auths {
				if _, ok := auth.Auths[host]; !ok {
					auth.Auths[host] = e
				}
			}
		}
	}

	for _, img := range images {
		for _, s := range img.secrets {
			if ns, _, _ := strings.Cut(s, "/"); unreadable[ns] {
				img.Credentials = "not readable"
			}
		}
	}

	return auth
}

// fluxChartURL is the chart a HelmRepository or OCIRepository serves, as the lookup takes it.
func fluxChartURL(src fluxSourceObject) string {
	u := strings.TrimSuffix(src.Spec.URL, "/")
	if src.Spec.Type == "oci" && !strings.HasPrefix(u, "oci://") {
		return "oci://" + strings.TrimPrefix(u, "https://")
	}

	return u
}
