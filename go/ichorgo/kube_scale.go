package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Scaling any object from the resource browser, like `kubectl scale RESOURCE/NAME
// --replicas=N`: every kind serving the /scale subresource (Deployments, bare ReplicaSets,
// StatefulSets, and CRDs such as Argo Rollouts or operator-managed pools) through its
// autoscaling/v1 Scale, and a Job through its spec.parallelism (Jobs have no /scale).

// kubeObjectScale is how many pods an object wants (Replicas) and runs (Current). Field is
// what the count sets: "replicas" (the /scale subresource) or "parallelism" (a Job).
type kubeObjectScale struct {
	Replicas int    `json:"replicas"`
	Current  int    `json:"current"`
	Field    string `json:"field"`
}

// scaleObject is the autoscaling/v1 Scale of /scale, or a Job: the fields read of either.
type scaleObject struct {
	Spec struct {
		Replicas    *int `json:"replicas"`
		Parallelism *int `json:"parallelism"`
	} `json:"spec"`
	Status struct {
		Replicas int `json:"replicas"`
		Active   int `json:"active"`
	} `json:"status"`
}

func isJobResource(group, resource string) bool {
	return group == "batch" && resource == "jobs"
}

// scaleTarget is the object's path (its /scale subresource but for a Job) and whether it is a
// Job.
func scaleTarget(group, version, resource, namespace, name string) (string, bool, error) {
	ref, err := newResourceRef(group, version, resource)
	if err != nil {
		return "", false, err
	}

	if err := validateNamespace(namespace); err != nil {
		return "", false, err
	}

	if name == "" {
		return "", false, errors.New("no object name given")
	}

	if !kubeNamePattern.MatchString(name) || strings.Contains(name, "..") {
		return "", false, fmt.Errorf("invalid Kubernetes name %q", name)
	}

	path := ref.path(namespace) + "/" + url.PathEscape(name)
	if isJobResource(ref.group, ref.resource) {
		return path, true, nil
	}

	return path + "/scale", false, nil
}

// KubeObjectScale reads the replicas of any scalable object (namespace "" when
// cluster-scoped), as a JSON kubeObjectScale. A Job's are its parallelism (1 when unset) and
// its active pods. kubeServer: see KubePods.
func KubeObjectScale(configYAML, contextName, kubeServer, group, version, resource, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	path, job, err := scaleTarget(group, version, resource, namespace, name)
	if err != nil {
		return "", err
	}

	demo := func() kubeObjectScale { return kubeObjectScale{Replicas: 2, Current: 2, Field: "replicas"} }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (kubeObjectScale, error) {
		var s scaleObject
		if err := k.get(ctx, path, &s); err != nil {
			return kubeObjectScale{}, err
		}

		if job {
			return kubeObjectScale{Replicas: derefOr(s.Spec.Parallelism, 1), Current: s.Status.Active, Field: "parallelism"}, nil
		}

		return kubeObjectScale{Replicas: derefOr(s.Spec.Replicas, 0), Current: s.Status.Replicas, Field: "replicas"}, nil
	})
}

// KubeScaleObject sets the replicas of any scalable object (a Job's parallelism; 0 pauses
// it), like `kubectl scale` (os:admin). kind names the object's kind for the warning: the
// HorizontalPodAutoscaler (a KEDA ScaledObject's too) that will change the count again, ""
// when none. kubeServer: see KubePods.
func KubeScaleObject(configYAML, contextName, kubeServer, group, version, resource, kind, namespace, name string, replicas int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	kind = strings.TrimSpace(kind)

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "scale", Namespace: namespace, Object: kind + "/" + name, Params: fmt.Sprintf("replicas=%d", replicas)})

	if replicas < 0 || replicas > maxScaleReplicas {
		return "", fmt.Errorf("replicas must be between 0 and %d", maxScaleReplicas)
	}

	path, job, err := scaleTarget(group, version, resource, namespace, name)
	if err != nil {
		return "", err
	}

	field := "replicas"
	if job {
		field = "parallelism"
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		patch := map[string]any{"spec": map[string]any{field: replicas}}
		if err := k.patch(ctx, path, "application/merge-patch+json", patch, nil); err != nil {
			return err
		}

		if !job && namespace != "" {
			out = autoscalerWarning(ctx, k, kind, namespace, name)
		}

		return nil
	})

	return out, err
}

func derefOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}

	return *v
}
