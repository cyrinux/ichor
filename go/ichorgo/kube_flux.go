package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// Flux v2 is read and driven through its custom resources, like Argo CD: Kustomizations and
// HelmReleases (what Flux deploys) and their sources (Git, OCI and Helm repositories,
// buckets), with the admin kubeconfig Talos issues. Actions are the annotations and the
// spec.suspend field the flux CLI writes. See the Linear plan document "D6. Flux".

const (
	groupFluxKustomize = "kustomize.toolkit.fluxcd.io"
	groupFluxHelm      = "helm.toolkit.fluxcd.io"
	groupFluxSource    = "source.toolkit.fluxcd.io"

	// fluxRequestedAt asks a controller to reconcile now; it echoes the value back in
	// status.lastHandledReconcileAt once done.
	fluxRequestedAt = "reconcile.fluxcd.io/requestedAt"
	// fluxForceAt / fluxResetAt (with the same value in requestedAt) make the helm-controller
	// run a one-off upgrade, or forget the failure counts of a release that gave up.
	fluxForceAt = "reconcile.fluxcd.io/forceAt"
	fluxResetAt = "reconcile.fluxcd.io/resetAt"

	// The labels the kustomize-controller puts on what it applies: who manages an object.
	fluxOwnerName      = "kustomize.toolkit.fluxcd.io/name"
	fluxOwnerNamespace = "kustomize.toolkit.fluxcd.io/namespace"

	fluxHistoryMax   = 10
	fluxResourcesMax = 500
)

// fluxSourceKinds are the source kinds listed, with their plural.
var fluxSourceKinds = []struct{ kind, plural string }{
	{"GitRepository", "gitrepositories"},
	{"OCIRepository", "ocirepositories"},
	{"HelmRepository", "helmrepositories"},
	{"Bucket", "buckets"},
}

type fluxStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"` // the Flux distribution ("v2.7.0"), else the kustomize-controller's tag
	// HelmError / SourcesError are set when that listing failed; the rest still shows.
	HelmError    string       `json:"helmError"`
	SourcesError string       `json:"sourcesError"`
	Apps         []fluxApp    `json:"apps"`
	Sources      []fluxSource `json:"sources"`
}

// fluxApp is a Kustomization or a HelmRelease.
type fluxApp struct {
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
	Owner     *fluxRef `json:"owner"`
	Source    *fluxRef `json:"source"`
	SourceURL string   `json:"sourceURL"`
	Path      string   `json:"path"`  // Kustomization
	Chart     string   `json:"chart"` // HelmRelease
	// ChartVersion is the wanted version (a semver range is fine), "" when the source decides.
	ChartVersion    string `json:"chartVersion"`
	TargetNamespace string `json:"targetNamespace"`
	Interval        string `json:"interval"`
	Prune           bool   `json:"prune"`
	// Revision is what is applied (a Git revision, or the chart version of a release);
	// AttemptedRevision the last one tried, which differs while it fails.
	Revision          string          `json:"revision"`
	AttemptedRevision string          `json:"attemptedRevision"`
	DependsOn         []string        `json:"dependsOn"` // "namespace/name"
	Failures          int             `json:"failures"`  // a release's install and upgrade failures in a row
	Conditions        []fluxCondition `json:"conditions"`
	Resources         []fluxResource  `json:"resources"` // a Kustomization's inventory
	History           []fluxHistory   `json:"history"`   // a HelmRelease's releases, newest first
	UnhealthyPods     []kubePod       `json:"unhealthyPods"`
	ReconciledAt      int64           `json:"reconciledAt"` // when Ready last changed

	namespaces []string // where its workloads run, to find its unhealthy pods
}

type fluxRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type fluxCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

type fluxResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type fluxHistory struct {
	Version      int    `json:"version"` // the Helm release revision
	ChartVersion string `json:"chartVersion"`
	AppVersion   string `json:"appVersion"`
	Status       string `json:"status"` // deployed|superseded|failed|uninstalled…
	DeployedAt   int64  `json:"deployedAt"`
}

type fluxSource struct {
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

// KubeFlux lists the Flux Kustomizations, HelmReleases and sources of every namespace
// (os:admin), with their readiness, revisions, a Kustomization's inventory, a release's
// history, and the pods that are not ready next to a failing one. "installed" is false when
// the cluster serves neither Kustomizations nor HelmReleases. kubeServer: see KubePods.
func KubeFlux(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() fluxStatus { return demoFlux(time.Now()) }, readFlux)
}

func readFlux(ctx context.Context, k *kubeClient) (fluxStatus, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return fluxStatus{}, err
	}

	kustomizeVersion, hasKustomize := groups[groupFluxKustomize]
	helmVersion, hasHelm := groups[groupFluxHelm]
	sourceVersion, hasSource := groups[groupFluxSource]

	if !hasKustomize && !hasHelm {
		return fluxStatus{Apps: []fluxApp{}, Sources: []fluxSource{}}, nil
	}

	var (
		ks       kubeList[fluxKustomizationObject]
		hrs      kubeList[fluxHelmReleaseObject]
		sources  = make([]kubeList[fluxSourceObject], len(fluxSourceKinds))
		srcErrs  = make([]error, len(fluxSourceKinds))
		ctrl     []dsPod
		ksErr    error
		helmErr  error
		wg       sync.WaitGroup
		listPath = func(group, version, plural string) string { return "/apis/" + group + "/" + version + "/" + plural }
	)

	if hasKustomize {
		wg.Go(func() { ksErr = getList(ctx, k, listPath(groupFluxKustomize, kustomizeVersion, "kustomizations"), &ks) })
	}

	if hasHelm {
		wg.Go(func() { helmErr = getList(ctx, k, listPath(groupFluxHelm, helmVersion, "helmreleases"), &hrs) })
	}

	if hasSource {
		for i, s := range fluxSourceKinds {
			wg.Go(func() { srcErrs[i] = getList(ctx, k, listPath(groupFluxSource, sourceVersion, s.plural), &sources[i]) })
		}
	}

	wg.Go(func() { ctrl, _ = listDSPods(ctx, k, "app=kustomize-controller") })
	wg.Wait()

	if ksErr != nil && !isNotFound(ksErr) {
		return fluxStatus{}, ksErr
	}

	var srcObjects []fluxSourceObject

	for i, list := range sources {
		for _, o := range list.Items {
			o.Kind = fluxSourceKinds[i].kind
			srcObjects = append(srcObjects, o)
		}
	}

	out := mapFlux(ks.Items, hrs.Items, srcObjects)
	out.Version = fluxVersion(ctrl)

	if helmErr != nil && !isNotFound(helmErr) {
		out.HelmError = sectionError(helmErr)
	}

	// A kind the source API serves in another version only (OCIRepository before Flux 2.6)
	// answers 404: none of it, not an error.
	var others []error
	for _, e := range srcErrs {
		if e != nil && !isNotFound(e) {
			others = append(others, e)
		}
	}

	out.SourcesError = sectionError(others...)

	var troubled []string

	for _, a := range out.Apps {
		if a.Level == healthCritical || a.Level == healthWarning {
			troubled = append(troubled, a.namespaces...)
		}
	}

	if len(troubled) > 0 {
		if pods, err := unhealthyPods(ctx, k, troubled); err == nil {
			attachFluxUnhealthyPods(out.Apps, pods)
		}
	}

	return out, nil
}

// fluxVersion is the Flux release the controllers were installed from (the
// app.kubernetes.io/version label of `flux install`), else the kustomize-controller's tag.
func fluxVersion(pods []dsPod) string {
	for _, p := range pods {
		if v := p.Metadata.Labels["app.kubernetes.io/version"]; v != "" {
			return v
		}
	}

	return argoVersion(pods)
}

// attachFluxUnhealthyPods gives each critical or warning app the pods that are not healthy
// in the namespaces it deploys to.
func attachFluxUnhealthyPods(apps []fluxApp, pods []kubePod) {
	byNamespace := map[string][]kubePod{}

	for _, p := range pods {
		if !p.Healthy {
			byNamespace[p.Namespace] = append(byNamespace[p.Namespace], p)
		}
	}

	for i := range apps {
		a := &apps[i]
		if a.Level != healthCritical && a.Level != healthWarning {
			continue
		}

		for _, ns := range a.namespaces {
			a.UnhealthyPods = append(a.UnhealthyPods, byNamespace[ns]...)
		}
	}
}

func mapFlux(ks []fluxKustomizationObject, hrs []fluxHelmReleaseObject, srcs []fluxSourceObject) fluxStatus {
	out := fluxStatus{Installed: true, Apps: make([]fluxApp, 0, len(ks)+len(hrs)), Sources: make([]fluxSource, 0, len(srcs))}

	for _, s := range srcs {
		out.Sources = append(out.Sources, mapFluxSource(s))
	}

	urls := map[fluxRef]string{}
	for _, s := range out.Sources {
		urls[fluxRef{s.Kind, s.Namespace, s.Name}] = s.URL
	}

	for _, o := range ks {
		out.Apps = append(out.Apps, mapFluxKustomization(o, urls))
	}

	for _, o := range hrs {
		out.Apps = append(out.Apps, mapFluxHelmRelease(o, urls))
	}

	for i := range out.Sources {
		s := &out.Sources[i]
		s.Apps = len(slices.DeleteFunc(slices.Clone(out.Apps), func(a fluxApp) bool {
			return a.Source == nil || *a.Source != fluxRef{s.Kind, s.Namespace, s.Name}
		}))
	}

	slices.SortFunc(out.Apps, func(a, b fluxApp) int {
		return cmp.Or(healthRank(a.Level)-healthRank(b.Level), strings.Compare(a.Name, b.Name), strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Kind, b.Kind))
	})

	slices.SortFunc(out.Sources, func(a, b fluxSource) int {
		return cmp.Or(healthRank(a.Level)-healthRank(b.Level), strings.Compare(a.Name, b.Name), strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Kind, b.Kind))
	})

	return out
}

// fluxLevel: idle when suspended, critical when stalled or not ready for a reason other than
// waiting, warning while it reconciles or waits for a dependency.
func fluxLevel(suspended bool, conds kubeConditions) string {
	ready := conds.get("Ready")

	switch {
	case suspended:
		return healthIdle
	case conds.is("Stalled"):
		return healthCritical
	case ready.Status == "True":
		return healthOK
	case ready.Status == "False" && !conds.is("Reconciling") && ready.Reason != "DependencyNotReady" && ready.Reason != "Progressing":
		return healthCritical
	default:
		return healthWarning
	}
}
