package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// The two autoscaling fixes made at 3 a.m.: hold a KEDA-scaled workload (pause its
// ScaledObject, at a count or where it is), and raise an HPA's ceiling (or its floor). Both
// are one merge patch, reversible, audited; demo contexts are refused by kubeMutate.

// KEDA's pause annotations are the reader's (kube_autoscaling_keda.go); it reads
// paused-replicas first.
const (
	// scaledObjectHold and scaledObjectResume are KubePauseScaledObject's two special counts.
	scaledObjectHold   = -1
	scaledObjectResume = -2
)

// errKEDAOwnsHPA refuses to edit an HPA KEDA made for a ScaledObject: KEDA would put its own
// bounds back at the next reconcile.
var errKEDAOwnsHPA = &kubeAPIError{Code: 403, Reason: "Forbidden",
	Message: "KEDA owns this HPA: change the ScaledObject's minReplicaCount or maxReplicaCount instead"}

// KubePauseScaledObject pauses the KEDA ScaledObject namespace/name at replicas (≥ 0: KEDA
// scales the target to that count and stops), holds it at its current count (-1), or resumes
// it (-2: both pause annotations removed) (os:admin; patch on scaledobjects.keda.sh). Refused
// on demo contexts. kubeServer: see KubePods.
func KubePauseScaledObject(configYAML, contextName, kubeServer, namespace, name string, replicas int) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	action, params := "pause-scaledobject", fmt.Sprintf("replicas=%d", replicas)
	switch replicas {
	case scaledObjectHold:
		params = "hold"
	case scaledObjectResume:
		action, params = "resume-scaledobject", ""
	}

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: action, Namespace: namespace, Object: "ScaledObject/" + name, Params: params,
	})

	if replicas < scaledObjectResume || replicas > maxScaleReplicas {
		return fmt.Errorf("replicas must be between 0 and %d, -1 to hold or -2 to resume", maxScaleReplicas)
	}

	if err := validateKubeName("ScaledObject", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		path := "/apis/" + groupKEDA + "/v1alpha1/namespaces/" + url.PathEscape(namespace) + "/scaledobjects/" + url.PathEscape(name)

		return k.patch(ctx, path, "application/merge-patch+json", scaledObjectPausePatch(replicas), nil)
	})
}

// scaledObjectPausePatch sets one pause annotation and removes the other (null removes a key:
// KEDA reads paused-replicas first, so a resume must remove both).
func scaledObjectPausePatch(replicas int) map[string]any {
	annotations := map[string]any{kedaPausedReplicasAnnotation: nil, kedaPausedAnnotation: nil}

	switch {
	case replicas >= 0:
		annotations[kedaPausedReplicasAnnotation] = fmt.Sprint(replicas)
	case replicas == scaledObjectHold:
		annotations[kedaPausedAnnotation] = "true"
	}

	return map[string]any{"metadata": map[string]any{"annotations": annotations}}
}

// KubeSetHPABounds sets the HorizontalPodAutoscaler namespace/name's minReplicas and
// maxReplicas (os:admin; patch on horizontalpodautoscalers.autoscaling). Refused for min below
// 1, max below min or above the scale limit, an HPA KEDA owns, and demo contexts.
// kubeServer: see KubePods.
func KubeSetHPABounds(configYAML, contextName, kubeServer, namespace, name string, minReplicas, maxReplicas int) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "hpa-bounds", Namespace: namespace, Object: "HorizontalPodAutoscaler/" + name,
		Params: fmt.Sprintf("min=%d max=%d", minReplicas, maxReplicas),
	})

	if err := validateHPABounds(minReplicas, maxReplicas); err != nil {
		return err
	}

	if err := validateKubeName("HorizontalPodAutoscaler", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return setHPABounds(ctx, k, namespace, name, minReplicas, maxReplicas)
	})
}

// validateHPABounds refuses what the HPA would (min below 1 without the HPAScaleToZero gate,
// max below min) and what the app does not scale to.
func validateHPABounds(minReplicas, maxReplicas int) error {
	switch {
	case minReplicas < 1:
		return fmt.Errorf("min replicas must be at least 1")
	case maxReplicas < minReplicas:
		return fmt.Errorf("max replicas (%d) must not be below min (%d)", maxReplicas, minReplicas)
	case maxReplicas > maxScaleReplicas:
		return fmt.Errorf("max replicas must be at most %d", maxScaleReplicas)
	}

	return nil
}

func setHPABounds(ctx context.Context, k *kubeClient, namespace, name string, minReplicas, maxReplicas int) error {
	path := "/apis/autoscaling/v2/namespaces/" + url.PathEscape(namespace) + "/horizontalpodautoscalers/" + url.PathEscape(name)

	var hpa hpaObject
	if err := k.get(ctx, path, &hpa); err != nil {
		return err
	}

	for _, ref := range hpa.Metadata.OwnerReferences {
		if ref.Kind == "ScaledObject" {
			return errKEDAOwnsHPA
		}
	}

	return k.patch(ctx, path, "application/merge-patch+json",
		map[string]any{"spec": map[string]any{"minReplicas": minReplicas, "maxReplicas": maxReplicas}}, nil)
}
