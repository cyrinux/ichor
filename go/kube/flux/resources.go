package flux

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/cyrinux/ichor/go/kube"
)

// Flux v2 is read and driven through its custom resources, like Argo CD: Kustomizations and
// HelmReleases (what Flux deploys) and their sources (Git, OCI and Helm repositories,
// buckets), with the admin kubeconfig Talos issues. Actions are the annotations and the
// spec.suspend field the flux CLI writes.

const (
	GroupKustomize = "kustomize.toolkit.fluxcd.io"
	GroupHelm      = "helm.toolkit.fluxcd.io"
	GroupSource    = "source.toolkit.fluxcd.io"
	GroupImage     = "image.toolkit.fluxcd.io" // image automation: the updates read who bumps an image

	// requestedAt asks a controller to reconcile now; it echoes the value back in
	// status.lastHandledReconcileAt once done.
	requestedAt = "reconcile.fluxcd.io/requestedAt"
	// forceAt / resetAt (with the same value in requestedAt) make the helm-controller
	// run a one-off upgrade, or forget the failure counts of a release that gave up.
	forceAt = "reconcile.fluxcd.io/forceAt"
	resetAt = "reconcile.fluxcd.io/resetAt"

	// The labels the kustomize-controller puts on what it applies: who manages an object.
	OwnerName      = "kustomize.toolkit.fluxcd.io/name"
	OwnerNamespace = "kustomize.toolkit.fluxcd.io/namespace"

	historyMax   = 10
	resourcesMax = 500
)

// SourceKinds are the source kinds listed, with their plural.
var SourceKinds = []struct{ Kind, Plural string }{
	{"GitRepository", "gitrepositories"},
	{"OCIRepository", "ocirepositories"},
	{"HelmRepository", "helmrepositories"},
	{"Bucket", "buckets"},
}

// Status is what KubeFlux answers.
type Status struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"` // the Flux distribution ("v2.7.0"), else the kustomize-controller's tag
	// HelmError / SourcesError are set when that listing failed; the rest still shows.
	HelmError    string   `json:"helmError"`
	SourcesError string   `json:"sourcesError"`
	Apps         []App    `json:"apps"`
	Sources      []Source `json:"sources"`
}

// App is a Kustomization or a HelmRelease.
type App struct {
	Kind      string `json:"kind"` // Kustomization|HelmRelease
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Level sums Ready, Stalled and Reconciling up: critical|warning|ok|idle (suspended).
	Level      string `json:"level"`
	Icon       string `json:"icon,omitempty"`
	RemoteIcon string `json:"remoteIcon,omitempty"`
	Ready      string `json:"ready"` // the Ready condition: True|False|Unknown
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	// Reconciling: the controller is at it now; Pending: a requested reconcile was not
	// handled yet.
	Reconciling bool `json:"reconciling"`
	Pending     bool `json:"pending"`
	Stalled     bool `json:"stalled"`
	Suspended   bool `json:"suspended"`
	// Owner is the Kustomization that applies this object from Git, nil when none.
	Owner     *Ref   `json:"owner"`
	Source    *Ref   `json:"source"`
	SourceURL string `json:"sourceURL"`
	Path      string `json:"path"`  // Kustomization
	Chart     string `json:"chart"` // HelmRelease
	// ChartVersion is the wanted version (a semver range is fine), "" when the source decides.
	ChartVersion    string `json:"chartVersion"`
	TargetNamespace string `json:"targetNamespace"`
	Interval        string `json:"interval"`
	Prune           bool   `json:"prune"`
	// Revision is what is applied (a Git revision, or the chart version of a release);
	// AttemptedRevision the last one tried, which differs while it fails.
	Revision          string `json:"revision"`
	AttemptedRevision string `json:"attemptedRevision"`
	// DependsOn are the apps this one waits for, "namespace/name", of its own kind (a
	// Kustomization depends on Kustomizations, a HelmRelease on HelmReleases).
	DependsOn     []string    `json:"dependsOn"`
	Failures      int         `json:"failures"` // a release's install and upgrade failures in a row
	Conditions    []Condition `json:"conditions"`
	Resources     []Resource  `json:"resources"` // a Kustomization's inventory
	History       []History   `json:"history"`   // a HelmRelease's releases, newest first
	UnhealthyPods []kube.Pod  `json:"unhealthyPods"`
	ReconciledAt  int64       `json:"reconciledAt"` // when Ready last changed
	// A HelmRelease's drift detection (spec.driftDetection.mode: warn|enabled|disabled, ""
	// when unset) and what the helm-controller last found (its Drifted condition): the
	// cluster's objects no longer match the release. A warning, not a health change.
	DriftMode    string `json:"driftMode,omitempty"`
	Drifted      bool   `json:"drifted,omitempty"`
	DriftMessage string `json:"driftMessage,omitempty"`
	DriftAt      int64  `json:"driftAt,omitempty"`

	namespaces []string // where its workloads run, to find its unhealthy pods
}

// Ref names a Flux object.
type Ref struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Condition is a status condition as the apps read it.
type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

// Resource is an entry of a Kustomization's inventory.
type Resource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// History is a release of a HelmRelease.
type History struct {
	Version      int    `json:"version"` // the Helm release revision
	ChartVersion string `json:"chartVersion"`
	AppVersion   string `json:"appVersion"`
	Status       string `json:"status"` // deployed|superseded|failed|uninstalled…
	DeployedAt   int64  `json:"deployedAt"`
}

// Source is a GitRepository, OCIRepository, HelmRepository or Bucket.
type Source struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Level     string `json:"level"`
	URL       string `json:"url"`
	Ref       string `json:"ref"` // branch, tag, semver range or digest followed
	// Revision is the fetched artifact's ("main@sha1:…", a tag@digest, an index digest).
	Revision    string `json:"revision"`
	Ready       string `json:"ready"`
	Reason      string `json:"reason"`
	Message     string `json:"message"`
	Reconciling bool   `json:"reconciling"`
	Pending     bool   `json:"pending"`
	Suspended   bool   `json:"suspended"`
	Interval    string `json:"interval"`
	FetchedAt   int64  `json:"fetchedAt"`
	Apps        int    `json:"apps"` // the Kustomizations and HelmReleases using it
}

// Read lists the Flux Kustomizations, HelmReleases and sources of every namespace, with their
// readiness, revisions, a Kustomization's inventory, a release's history, and the pods that
// are not ready next to a failing one. Installed is false when the cluster serves neither
// Kustomizations nor HelmReleases. A section that fails only says so.
func Read(ctx context.Context, k Env) (Status, error) {
	groups, err := kube.APIGroups(ctx, k.Client)
	if err != nil {
		return Status{}, err
	}

	kustomizeVersion, hasKustomize := groups[GroupKustomize]
	helmVersion, hasHelm := groups[GroupHelm]
	sourceVersion, hasSource := groups[GroupSource]

	if !hasKustomize && !hasHelm {
		return Status{Apps: []App{}, Sources: []Source{}}, nil
	}

	var (
		ks       kube.List[KustomizationObject]
		hrs      kube.List[HelmReleaseObject]
		sources  = make([]kube.List[SourceObject], len(SourceKinds))
		srcErrs  = make([]error, len(SourceKinds))
		version  string
		ksErr    error
		helmErr  error
		wg       sync.WaitGroup
		listPath = func(group, version, plural string) string { return "/apis/" + group + "/" + version + "/" + plural }
	)

	if hasKustomize {
		wg.Go(func() {
			ksErr = kube.GetList(ctx, k.Client, listPath(GroupKustomize, kustomizeVersion, "kustomizations"), &ks)
		})
	}

	if hasHelm {
		wg.Go(func() { helmErr = kube.GetList(ctx, k.Client, listPath(GroupHelm, helmVersion, "helmreleases"), &hrs) })
	}

	if hasSource {
		for i, s := range SourceKinds {
			wg.Go(func() {
				srcErrs[i] = kube.GetList(ctx, k.Client, listPath(GroupSource, sourceVersion, s.Plural), &sources[i])
			})
		}
	}

	wg.Go(func() { version = k.version(ctx) })
	wg.Wait()

	if ksErr != nil && !kube.IsNotFound(ksErr) {
		return Status{}, ksErr
	}

	var srcObjects []SourceObject

	for i, list := range sources {
		for _, o := range list.Items {
			o.Kind = SourceKinds[i].Kind
			srcObjects = append(srcObjects, o)
		}
	}

	out := mapStatus(ks.Items, hrs.Items, srcObjects)
	out.Version = version

	if helmErr != nil && !kube.IsNotFound(helmErr) {
		out.HelmError = k.sectionError(helmErr)
	}

	// A kind the source API serves in another version only (OCIRepository before Flux 2.6)
	// answers 404: none of it, not an error.
	var others []error
	for _, e := range srcErrs {
		if e != nil && !kube.IsNotFound(e) {
			others = append(others, e)
		}
	}

	out.SourcesError = k.sectionError(others...)

	var troubled []string

	for _, a := range out.Apps {
		if a.Level == kube.HealthCritical || a.Level == kube.HealthWarning {
			troubled = append(troubled, a.namespaces...)
		}
	}

	if len(troubled) > 0 && k.UnhealthyPods != nil {
		if pods, err := k.UnhealthyPods(ctx, troubled); err == nil {
			attachUnhealthyPods(out.Apps, pods)
		}
	}

	return out, nil
}

// attachUnhealthyPods gives each critical or warning app the pods that are not healthy
// in the namespaces it deploys to.
func attachUnhealthyPods(apps []App, pods []kube.Pod) {
	byNamespace := map[string][]kube.Pod{}

	for _, p := range pods {
		if !p.Healthy {
			byNamespace[p.Namespace] = append(byNamespace[p.Namespace], p)
		}
	}

	for i := range apps {
		a := &apps[i]
		if a.Level != kube.HealthCritical && a.Level != kube.HealthWarning {
			continue
		}

		for _, ns := range a.namespaces {
			a.UnhealthyPods = append(a.UnhealthyPods, byNamespace[ns]...)
		}
	}
}

func mapStatus(ks []KustomizationObject, hrs []HelmReleaseObject, srcs []SourceObject) Status {
	out := Status{Installed: true, Apps: make([]App, 0, len(ks)+len(hrs)), Sources: make([]Source, 0, len(srcs))}

	for _, s := range srcs {
		out.Sources = append(out.Sources, mapSource(s))
	}

	urls := map[Ref]string{}
	for _, s := range out.Sources {
		urls[Ref{s.Kind, s.Namespace, s.Name}] = s.URL
	}

	for _, o := range ks {
		out.Apps = append(out.Apps, mapKustomization(o, urls))
	}

	for _, o := range hrs {
		out.Apps = append(out.Apps, mapHelmRelease(o, urls))
	}

	for i := range out.Sources {
		s := &out.Sources[i]
		s.Apps = len(slices.DeleteFunc(slices.Clone(out.Apps), func(a App) bool {
			return a.Source == nil || *a.Source != Ref{s.Kind, s.Namespace, s.Name}
		}))
	}

	slices.SortFunc(out.Apps, func(a, b App) int {
		return cmp.Or(kube.HealthRank(a.Level)-kube.HealthRank(b.Level), strings.Compare(a.Name, b.Name), strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Kind, b.Kind))
	})

	slices.SortFunc(out.Sources, func(a, b Source) int {
		return cmp.Or(kube.HealthRank(a.Level)-kube.HealthRank(b.Level), strings.Compare(a.Name, b.Name), strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Kind, b.Kind))
	})

	return out
}

// level: idle when suspended, critical when stalled or not ready for a reason other than
// waiting, warning while it reconciles or waits for a dependency.
func level(suspended bool, conds kube.Conditions) string {
	ready := conds.Get("Ready")

	switch {
	case suspended:
		return kube.HealthIdle
	case conds.Is("Stalled"):
		return kube.HealthCritical
	case ready.Status == "True":
		return kube.HealthOK
	case ready.Status == "False" && !conds.Is("Reconciling") && ready.Reason != "DependencyNotReady" && ready.Reason != "Progressing":
		return kube.HealthCritical
	default:
		return kube.HealthWarning
	}
}
