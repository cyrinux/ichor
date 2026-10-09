package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Actions on a Flux object, each one merge patch, as the flux CLI does: an annotation asks
// the controller to reconcile now, spec.suspend stops it from reconciling at all. Every
// patch carries the resourceVersion read just before, so it fails instead of overwriting a
// change made in between.

const (
	fluxActionReconcile           = "reconcile"
	fluxActionReconcileWithSource = "reconcileWithSource"
	fluxActionSuspend             = "suspend"
	fluxActionResume              = "resume"
	fluxActionForce               = "force" // HelmRelease: a one-off upgrade even with nothing changed
	fluxActionReset               = "reset" // HelmRelease: forget the failures, retry from scratch
)

var fluxActions = []string{fluxActionReconcile, fluxActionReconcileWithSource, fluxActionSuspend, fluxActionResume, fluxActionForce, fluxActionReset}

// fluxKinds are the kinds actions run on, with their group and plural.
var fluxKinds = map[string]struct{ group, plural string }{
	"Kustomization":  {groupFluxKustomize, "kustomizations"},
	"HelmRelease":    {groupFluxHelm, "helmreleases"},
	"HelmChart":      {groupFluxSource, "helmcharts"},
	"GitRepository":  {groupFluxSource, "gitrepositories"},
	"OCIRepository":  {groupFluxSource, "ocirepositories"},
	"HelmRepository": {groupFluxSource, "helmrepositories"},
	"Bucket":         {groupFluxSource, "buckets"},
}

var (
	errFluxHelmOnly   = &kubeAPIError{Code: 400, Reason: "BadRequest", Message: "only a HelmRelease can be forced or reset"}
	errFluxSuspended  = &kubeAPIError{Code: 409, Reason: "Suspended", Message: "it is suspended: resume it first"}
	errFluxNoSource   = &kubeAPIError{Code: 400, Reason: "BadRequest", Message: "only a Kustomization or a HelmRelease has a source to reconcile with"}
	errFluxNotInstall = &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Flux is not installed"}
)

// fluxTarget is what an action reads of the object before patching it.
type fluxTarget struct {
	Metadata fluxMeta `json:"metadata"`
	Spec     struct {
		Suspend   bool          `json:"suspend"`
		SourceRef *fluxCrossRef `json:"sourceRef"` // Kustomization
		ChartRef  *fluxCrossRef `json:"chartRef"`  // HelmRelease
		Chart     *struct {
			Spec struct {
				SourceRef fluxCrossRef `json:"sourceRef"`
			} `json:"spec"`
		} `json:"chart"`
	} `json:"spec"`
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

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "flux-" + action, Namespace: namespace, Object: kind + "/" + name})

	if !slices.Contains(fluxActions, action) {
		return fmt.Errorf("unsupported Flux action %q", action)
	}

	if _, ok := fluxKinds[kind]; !ok || kind == "HelmChart" {
		return fmt.Errorf("unsupported Flux kind %q", kind)
	}

	if err := validateKubeName(strings.ToLower(kind), namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return fluxAction(ctx, k, kind, namespace, name, action, time.Now())
	})
}

func fluxAction(ctx context.Context, k *kubeClient, kind, namespace, name, action string, now time.Time) error {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return err
	}

	path, err := fluxPath(groups, kind, namespace, name)
	if err != nil {
		return err
	}

	var obj fluxTarget
	if err := k.get(ctx, path, &obj); err != nil {
		return err
	}

	stamp := now.UTC().Format(time.RFC3339Nano)

	if action == fluxActionReconcileWithSource {
		src, err := fluxSourceOf(kind, obj)
		if err != nil {
			return err
		}

		if err := fluxAction(ctx, k, src.Kind, src.Namespace, src.Name, fluxActionReconcile, now); err != nil {
			return fmt.Errorf("%s %s: %w", src.Kind, src.Name, err)
		}
	}

	patch, err := fluxPatch(kind, obj, action, stamp)
	if err != nil {
		return err
	}

	meta, _ := patch["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		patch["metadata"] = meta
	}

	meta["resourceVersion"] = obj.Metadata.ResourceVersion

	return k.patch(ctx, path, "application/merge-patch+json", patch, nil)
}

func fluxPath(groups map[string]string, kind, namespace, name string) (string, error) {
	k, ok := fluxKinds[kind]
	if !ok {
		return "", fmt.Errorf("unsupported Flux kind %q", kind)
	}

	version, ok := groups[k.group]
	if !ok {
		return "", errFluxNotInstall
	}

	return "/apis/" + k.group + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/" + k.plural + "/" + url.PathEscape(name), nil
}

// fluxSourceOf is what a Kustomization or HelmRelease is built from. A HelmRelease with an
// inline chart reads it from the HelmChart the helm-controller made for it.
func fluxSourceOf(kind string, obj fluxTarget) (fluxRef, error) {
	ns := obj.Metadata.Namespace

	switch {
	case kind == "Kustomization" && obj.Spec.SourceRef != nil:
		return fluxRef{obj.Spec.SourceRef.Kind, cmp.Or(obj.Spec.SourceRef.Namespace, ns), obj.Spec.SourceRef.Name}, nil
	case kind == "HelmRelease" && obj.Spec.ChartRef != nil:
		return fluxRef{obj.Spec.ChartRef.Kind, cmp.Or(obj.Spec.ChartRef.Namespace, ns), obj.Spec.ChartRef.Name}, nil
	case kind == "HelmRelease" && obj.Spec.Chart != nil:
		return fluxRef{"HelmChart", cmp.Or(obj.Spec.Chart.Spec.SourceRef.Namespace, ns), ns + "-" + obj.Metadata.Name}, nil
	default:
		return fluxRef{}, errFluxNoSource
	}
}

// fluxPatch is the merge patch for action on obj, or why it is refused. stamp is the
// request token the controller echoes back once it handled it.
func fluxPatch(kind string, obj fluxTarget, action, stamp string) (map[string]any, error) {
	annotate := func(extra ...string) map[string]any {
		ann := map[string]any{fluxRequestedAt: stamp}
		for _, a := range extra {
			ann[a] = stamp
		}

		return map[string]any{"metadata": map[string]any{"annotations": ann}}
	}

	switch action {
	case fluxActionSuspend:
		return map[string]any{"spec": map[string]any{"suspend": true}}, nil
	case fluxActionResume:
		p := annotate()
		p["spec"] = map[string]any{"suspend": nil}

		return p, nil
	}

	if obj.Spec.Suspend {
		return nil, errFluxSuspended
	}

	switch action {
	case fluxActionForce, fluxActionReset:
		if kind != "HelmRelease" {
			return nil, errFluxHelmOnly
		}

		if action == fluxActionForce {
			return annotate(fluxForceAt), nil
		}

		return annotate(fluxResetAt), nil
	default:
		return annotate(), nil
	}
}
