package ichorgo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube/flux"
)

// Flux v2 is read and driven through its custom resources: the logic is in go/kube/flux. Here
// are the exported entry points, what comes from the rest of the facade (a pod's status, the
// controller's image, the icon catalog, the sign-in error) and the names the other kube_* files
// call.

const (
	groupFluxKustomize = flux.GroupKustomize
	groupFluxHelm      = flux.GroupHelm
	groupFluxSource    = flux.GroupSource
	groupFluxImage     = flux.GroupImage

	fluxOwnerName      = flux.OwnerName
	fluxOwnerNamespace = flux.OwnerNamespace

	fluxActionReconcile = flux.ActionReconcile
)

type (
	fluxStatus              = flux.Status
	fluxApp                 = flux.App
	fluxSource              = flux.Source
	fluxRef                 = flux.Ref
	fluxMeta                = flux.Meta
	fluxCrossRef            = flux.CrossRef
	fluxKustomizationObject = flux.KustomizationObject
	fluxHelmReleaseObject   = flux.HelmReleaseObject
	fluxSourceObject        = flux.SourceObject
	fluxFollow              = flux.FollowRef
	fluxResource            = flux.Resource
	fluxCondition           = flux.Condition
	fluxHistory             = flux.History
)

func fluxPath(groups map[string]string, kind, namespace, name string) (string, error) {
	return flux.Path(groups, kind, namespace, name)
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
	st, err := flux.Read(ctx, fluxEnv(k))
	if err != nil {
		return fluxStatus{}, err
	}

	for i := range st.Apps {
		st.Apps[i].Icon, st.Apps[i].RemoteIcon = fluxIcon(st.Apps[i].Chart, st.Apps[i].Name)
	}

	return st, nil
}

// fluxEnv is what go/kube/flux needs of k and of the rest of the facade.
func fluxEnv(k *kubeClient) flux.Env {
	return flux.Env{
		Client: k.api(),
		Error:  kubeError,
		Version: func(ctx context.Context) string {
			pods, _ := listDSPods(ctx, k, "app=kustomize-controller")

			return fluxVersion(pods)
		},
		UnhealthyPods: func(ctx context.Context, namespaces []string) ([]kubePod, error) {
			return unhealthyPods(ctx, k, namespaces)
		},
	}
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

// fluxIcon names the icon from the catalog: by chart, then by name.
func fluxIcon(words ...string) (icon, remote string) {
	catalog := loadAppCatalog()

	for _, w := range words {
		if w == "" {
			continue
		}

		switch id := catalog.byWord(w); {
		case id.app != nil && id.app.hasIcon():
			return id.app.ID, ""
		case id.slug != "":
			return "", id.slug
		}
	}

	return "", ""
}

// KubeFluxAction runs an action on the Flux object kind namespace/name (os:admin):
// reconcile, reconcileWithSource (its source first, then it), suspend, resume, and for a
// HelmRelease force (upgrade now) or reset (forget the failure counts). kind is
// Kustomization, HelmRelease, GitRepository, OCIRepository, HelmRepository or Bucket.
// kubeServer: see KubePods.
func KubeFluxAction(configYAML, contextName, kubeServer, kind, namespace, name, action string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "flux-" + action, Namespace: namespace, Object: kind + "/" + name})

	if !flux.IsAction(action) {
		return fmt.Errorf("unsupported Flux action %q", action)
	}

	if !flux.HasKind(kind) || kind == "HelmChart" {
		return fmt.Errorf("unsupported Flux kind %q", kind)
	}

	if err := validateKubeName(strings.ToLower(kind), namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return flux.Action(ctx, k.api(), kind, namespace, name, action, time.Now())
	})
}
