package ichorgo

import (
	"strings"
	"time"
)

// demoObjectSummary sums up an object of the demo cluster: a pod leads to its ReplicaSet,
// then its Deployment, then the Flux HelmRelease that applied it; the worker's crash-looping
// pod and Deployment show the bad tones. Any other kind has metadata only.
func demoObjectSummary(ref resourceRef, namespace, name string, now time.Time) kubeObjectSummary {
	ago := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	broken := strings.HasPrefix(name, "worker")

	s := kubeObjectSummary{
		Kind: demoKind(ref), APIVersion: demoAPIVersion(ref), Namespace: namespace, Name: name,
		Conditions: []summaryCondition{}, Owners: []kubeOwner{}, Labels: map[string]string{"app.kubernetes.io/name": appName(name)},
		Annotations: map[string]string{}, Created: demoBoot.UnixMilli(), Finalizers: []string{}, Highlights: []kubeHighlight{},
	}

	cond := func(typ, status, reason, message string, since time.Duration) summaryCondition {
		c := summaryCondition{Type: typ, Status: status, Reason: reason, Message: message, LastTransition: ago(since)}
		c.Tone = conditionTone(c)

		return c
	}

	switch ref.resource {
	case "pods":
		s.Phase = "Running"
		s.Owners = []kubeOwner{demoOwner("apps", "replicasets", "ReplicaSet", namespace, replicaSetOf(name))}
		s.Highlights = []kubeHighlight{{highlightNode, "demo-worker-1"}, {highlightImage, demoImage(name)}}

		if broken {
			s.Conditions = []summaryCondition{
				cond("Ready", "False", "ContainersNotReady", "containers with unready status: [worker]", 6*time.Minute),
				cond("ContainersReady", "False", "ContainersNotReady", "containers with unready status: [worker]", 6*time.Minute),
				cond("PodScheduled", "True", "", "", 72*time.Hour),
			}
		} else {
			s.Conditions = []summaryCondition{cond("Ready", "True", "", "", 26*time.Hour), cond("PodScheduled", "True", "", "", 72*time.Hour)}
		}
	case "replicasets":
		s.Owners = []kubeOwner{demoOwner("apps", "deployments", "Deployment", namespace, appName(name))}
		s.Highlights = demoWorkloadHighlights(name, broken)
	case "deployments":
		s.Labels["helm.toolkit.fluxcd.io/name"] = appName(name)
		s.Labels["helm.toolkit.fluxcd.io/namespace"] = "flux-system"
		s.Owners = []kubeOwner{{
			Via: ownerViaFlux, Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases", Kind: "HelmRelease",
			Namespace: "flux-system", Name: appName(name), Namespaced: true, Verbs: []string{"get", "list"},
		}}
		s.Highlights = demoWorkloadHighlights(name, broken)
		s.Conditions = []summaryCondition{cond("Progressing", "True", "NewReplicaSetAvailable", "ReplicaSet has successfully progressed.", 72*time.Hour)}

		if broken {
			s.Conditions = append(s.Conditions, cond("Available", "False", "MinimumReplicasUnavailable", "Deployment does not have minimum availability.", 6*time.Minute))
		} else {
			s.Conditions = append(s.Conditions, cond("Available", "True", "MinimumReplicasAvailable", "Deployment has minimum availability.", 26*time.Hour))
		}
	case "applications":
		s.Conditions = []summaryCondition{cond("Synced", "True", "Succeeded", "", 2*time.Hour), cond("Healthy", "True", "", "", 2*time.Hour)}
	}

	sortConditions(s.Conditions)
	s.Health, s.HealthReason = objectHealth(s.Conditions, s.Phase, false)
	s.Events = demoEvents(namespace, s.Kind, name, now).Events

	return s
}

func demoOwner(group, resource, kind, namespace, name string) kubeOwner {
	return kubeOwner{
		Via: ownerViaReference, Group: group, Version: "v1", Resource: resource, Kind: kind, Namespace: namespace, Name: name,
		Namespaced: true, Verbs: []string{"get", "list", "update"}, Controller: true,
	}
}

func demoWorkloadHighlights(name string, broken bool) []kubeHighlight {
	replicas := "3/3"
	if broken {
		replicas = "1/2"
	}

	return []kubeHighlight{
		{highlightReplicas, replicas},
		{highlightSelector, "app.kubernetes.io/name=" + appName(name)},
		{highlightImage, demoImage(name)},
	}
}

// appName is the Deployment of a demo ReplicaSet or pod name ("worker-6f4b8-uvwxy": "worker").
func appName(name string) string {
	for _, w := range demoKubeWorkloads() {
		if name == w.Name || strings.HasPrefix(name, w.Name+"-") {
			return w.Name
		}
	}

	return name
}

func replicaSetOf(pod string) string {
	if i := strings.LastIndex(pod, "-"); i > 0 {
		return pod[:i]
	}

	return pod
}

func demoImage(name string) string {
	for _, w := range demoKubeWorkloads() {
		if w.Name == appName(name) && len(w.Images) > 0 {
			return w.Images[0]
		}
	}

	return "nginx:1.27"
}

func demoKind(ref resourceRef) string {
	for _, r := range demoAPIResources().Resources {
		if r.Group == ref.group && r.Resource == ref.resource {
			return r.Kind
		}
	}

	switch ref.resource {
	case "replicasets":
		return "ReplicaSet"
	case "helmreleases":
		return "HelmRelease"
	default:
		return strings.TrimSuffix(ref.resource, "s")
	}
}

func demoAPIVersion(ref resourceRef) string {
	if ref.group == "" {
		return ref.version
	}

	return ref.group + "/" + ref.version
}
