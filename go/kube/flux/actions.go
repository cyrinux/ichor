package flux

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

// Actions on a Flux object, each one merge patch, as the flux CLI does: an annotation asks
// the controller to reconcile now, spec.suspend stops it from reconciling at all. Every
// patch carries the resourceVersion read just before, so it fails instead of overwriting a
// change made in between.

const (
	// ActionReconcile asks for a reconcile now.
	ActionReconcile           = "reconcile"
	ActionReconcileWithSource = "reconcileWithSource"
	ActionSuspend             = "suspend"
	ActionResume              = "resume"
	ActionForce               = "force" // HelmRelease: a one-off upgrade even with nothing changed
	ActionReset               = "reset" // HelmRelease: forget the failures, retry from scratch
)

var actions = []string{ActionReconcile, ActionReconcileWithSource, ActionSuspend, ActionResume, ActionForce, ActionReset}

// kinds are the kinds actions run on, with their group and plural.
var kinds = map[string]struct{ group, plural string }{
	"Kustomization":  {GroupKustomize, "kustomizations"},
	"HelmRelease":    {GroupHelm, "helmreleases"},
	"HelmChart":      {GroupSource, "helmcharts"},
	"GitRepository":  {GroupSource, "gitrepositories"},
	"OCIRepository":  {GroupSource, "ocirepositories"},
	"HelmRepository": {GroupSource, "helmrepositories"},
	"Bucket":         {GroupSource, "buckets"},
}

var (
	errHelmOnly     = &kube.APIError{Code: 400, Reason: "BadRequest", Message: "only a HelmRelease can be forced or reset"}
	errSuspended    = &kube.APIError{Code: 409, Reason: "Suspended", Message: "it is suspended: resume it first"}
	ErrNoSource     = &kube.APIError{Code: 400, Reason: "BadRequest", Message: "only a Kustomization or a HelmRelease has a source to reconcile with"}
	errNotInstalled = &kube.APIError{Code: 404, Reason: "NotFound", Message: "Flux is not installed"}
)

// target is what an action reads of the object before patching it.
type target struct {
	Metadata Meta `json:"metadata"`
	Spec     struct {
		Suspend   bool      `json:"suspend"`
		SourceRef *CrossRef `json:"sourceRef"` // Kustomization
		ChartRef  *CrossRef `json:"chartRef"`  // HelmRelease
		Chart     *struct {
			Spec struct {
				SourceRef CrossRef `json:"sourceRef"`
			} `json:"spec"`
		} `json:"chart"`
	} `json:"spec"`
}

// IsAction tells an action Action runs.
func IsAction(action string) bool { return slices.Contains(actions, action) }

// HasKind tells a kind actions and follows run on, HelmChart included.
func HasKind(kind string) bool {
	_, ok := kinds[kind]

	return ok
}

// Suspend stops the controller from reconciling the HelmRelease namespace/name, as a Helm
// rollback does first so that Flux does not undo it.
func Suspend(ctx context.Context, k kube.Client, namespace, name string, now time.Time) error {
	return Action(ctx, k, "HelmRelease", namespace, name, ActionSuspend, now)
}

// Action runs action on the Flux object kind namespace/name: reconcile, reconcileWithSource
// (its source first, then it), suspend, resume, and for a HelmRelease force (upgrade now) or
// reset (forget the failure counts). The caller checked the arguments.
func Action(ctx context.Context, k kube.Client, kind, namespace, name, action string, now time.Time) error {
	groups, err := kube.APIGroups(ctx, k)
	if err != nil {
		return err
	}

	path, err := Path(groups, kind, namespace, name)
	if err != nil {
		return err
	}

	var obj target
	if err := k.Get(ctx, path, &obj); err != nil {
		return err
	}

	stamp := now.UTC().Format(time.RFC3339Nano)

	if action == ActionReconcileWithSource {
		src, err := sourceOf(kind, obj)
		if err != nil {
			return err
		}

		if err := Action(ctx, k, src.Kind, src.Namespace, src.Name, ActionReconcile, now); err != nil {
			return fmt.Errorf("%s %s: %w", src.Kind, src.Name, err)
		}
	}

	patch, err := patchFor(kind, obj, action, stamp)
	if err != nil {
		return err
	}

	meta, _ := patch["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		patch["metadata"] = meta
	}

	meta["resourceVersion"] = obj.Metadata.ResourceVersion

	return k.Patch(ctx, path, "application/merge-patch+json", patch, nil)
}

// Path is the API path of the object, from the served API groups (kube.APIGroups).
func Path(groups map[string]string, kind, namespace, name string) (string, error) {
	k, ok := kinds[kind]
	if !ok {
		return "", fmt.Errorf("unsupported Flux kind %q", kind)
	}

	version, ok := groups[k.group]
	if !ok {
		return "", errNotInstalled
	}

	return "/apis/" + k.group + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/" + k.plural + "/" + url.PathEscape(name), nil
}

// sourceOf is what a Kustomization or HelmRelease is built from. A HelmRelease with an
// inline chart reads it from the HelmChart the helm-controller made for it.
func sourceOf(kind string, obj target) (Ref, error) {
	ns := obj.Metadata.Namespace

	switch {
	case kind == "Kustomization" && obj.Spec.SourceRef != nil:
		return Ref{obj.Spec.SourceRef.Kind, cmp.Or(obj.Spec.SourceRef.Namespace, ns), obj.Spec.SourceRef.Name}, nil
	case kind == "HelmRelease" && obj.Spec.ChartRef != nil:
		return Ref{obj.Spec.ChartRef.Kind, cmp.Or(obj.Spec.ChartRef.Namespace, ns), obj.Spec.ChartRef.Name}, nil
	case kind == "HelmRelease" && obj.Spec.Chart != nil:
		return Ref{"HelmChart", cmp.Or(obj.Spec.Chart.Spec.SourceRef.Namespace, ns), ns + "-" + obj.Metadata.Name}, nil
	default:
		return Ref{}, ErrNoSource
	}
}

// patchFor is the merge patch for action on obj, or why it is refused. stamp is the
// request token the controller echoes back once it handled it.
func patchFor(kind string, obj target, action, stamp string) (map[string]any, error) {
	annotate := func(extra ...string) map[string]any {
		ann := map[string]any{requestedAt: stamp}
		for _, a := range extra {
			ann[a] = stamp
		}

		return map[string]any{"metadata": map[string]any{"annotations": ann}}
	}

	switch action {
	case ActionSuspend:
		return map[string]any{"spec": map[string]any{"suspend": true}}, nil
	case ActionResume:
		p := annotate()
		p["spec"] = map[string]any{"suspend": nil}

		return p, nil
	}

	if obj.Spec.Suspend {
		return nil, errSuspended
	}

	switch action {
	case ActionForce, ActionReset:
		if kind != "HelmRelease" {
			return nil, errHelmOnly
		}

		if action == ActionForce {
			return annotate(forceAt), nil
		}

		return annotate(resetAt), nil
	default:
		return annotate(), nil
	}
}
