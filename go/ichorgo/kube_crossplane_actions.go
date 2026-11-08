package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// The three safe, reversible Crossplane actions on a managed or composite resource, each a
// merge patch of one annotation: pause (the provider stops calling the cloud API while a
// ReconcileError is looked into), resume, and reconcile now instead of waiting for the poll
// interval. Deleting stays with the generic object delete: for a managed resource it deletes
// the external thing.

const (
	// xpPausedAnnotation pauses reconciliation; only "true" pauses, and removing it resumes.
	xpPausedAnnotation = "crossplane.io/paused"
	// xpReconcileAnnotation triggers an immediate reconciliation when its value changes:
	// handled by crossplane-runtime 2.3+ providers (and core 2.3+ for composites), which write
	// the value to status.lastHandledReconcileAt. Older ones ignore the key, but an annotation
	// change is itself an update their watch predicate lets through, so it still queues one.
	xpReconcileAnnotation = "crossplane.io/reconcile-requested-at"

	xpActionPause     = "pause"
	xpActionResume    = "resume"
	xpActionReconcile = "reconcile"
)

// KubeCrossplaneAction pauses, resumes or asks to reconcile now (action "pause", "resume" or
// "reconcile") the Crossplane managed or composite resource name of group/version/resource,
// in namespace ("" for a cluster-scoped one) (os:admin; patch on that resource). Refused on
// demo contexts and when the credentials may not patch it. kubeServer: see KubePods.
func KubeCrossplaneAction(configYAML, contextName, kubeServer, group, version, resource, namespace, name, action string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	group, version, resource = strings.TrimSpace(group), strings.TrimSpace(version), strings.TrimSpace(resource)
	action = strings.TrimSpace(action)

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "crossplane-" + action, Namespace: namespace, Object: resource + "." + group + "/" + name,
	})

	if action != xpActionPause && action != xpActionResume && action != xpActionReconcile {
		return fmt.Errorf("unknown Crossplane action %q: pause, resume or reconcile", action)
	}

	if group == "" || version == "" || resource == "" {
		return fmt.Errorf("no group, version or resource given for %q", name)
	}

	// A cluster-scoped object has no namespace: the name alone is checked.
	if err := validateKubeName("Crossplane object", cmp.Or(namespace, "cluster"), name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return crossplaneAction(ctx, k, resourceRef{group, version, resource}, namespace, name, action, time.Now())
	})
}

func crossplaneAction(ctx context.Context, k *kubeClient, ref resourceRef, namespace, name, action string, now time.Time) error {
	access, err := canDo(ctx, k, ssarAttributes{Namespace: namespace, Verb: "patch", Group: ref.group, Resource: ref.resource, Name: name})
	if err == nil && !access.Allowed {
		return &kubeAPIError{Code: 403, Reason: "Forbidden",
			Message: strings.TrimSpace(fmt.Sprintf("these credentials may not patch %s.%s %s %s", ref.resource, ref.group, name, access.Reason))}
	}

	return k.patch(ctx, ref.path(namespace)+"/"+url.PathEscape(name), "application/merge-patch+json", crossplaneActionPatch(action, now), nil)
}

// crossplaneActionPatch sets or removes the annotation: null removes it (a "false" would stay,
// and some tooling reads the key alone as paused); the reconcile request takes the time now,
// a new value each time.
func crossplaneActionPatch(action string, now time.Time) map[string]any {
	var annotations map[string]any

	switch action {
	case xpActionPause:
		annotations = map[string]any{xpPausedAnnotation: "true"}
	case xpActionResume:
		annotations = map[string]any{xpPausedAnnotation: nil}
	default:
		annotations = map[string]any{xpReconcileAnnotation: now.UTC().Format(time.RFC3339)}
	}

	return map[string]any{"metadata": map[string]any{"annotations": annotations}}
}
