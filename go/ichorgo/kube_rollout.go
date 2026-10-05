package ichorgo

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Labels and annotations the controllers put on what they create, to tell the pods of the
// latest pod template from the older ones.
const (
	podTemplateHashLabel       = "pod-template-hash"        // Deployment, through its ReplicaSets
	controllerRevisionLabel    = "controller-revision-hash" // StatefulSet
	podTemplateGenerationLabel = "pod-template-generation"  // DaemonSet
	// The template generation a DaemonSet stamps on its pods: metadata.generation moves on
	// any spec change, this one only when the pod template changes.
	daemonSetTemplateGenerationKey = "deprecated.daemonset.template.generation"
	deploymentRevisionKey          = "deployment.kubernetes.io/revision" // on a Deployment and its ReplicaSets
)

// kubeRolloutPod is a pod of a workload during a rollout.
type kubeRolloutPod struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // as `kubectl get pods`
	Healthy    bool   `json:"healthy"`
	Ready      int    `json:"ready"`
	Containers int    `json:"containers"`
	Restarts   int    `json:"restarts"`
	Node       string `json:"node"`
	Created    int64  `json:"created"` // unix ms
	Updated    bool   `json:"updated"` // runs the latest pod template
}

// kubeRolloutStatus is what `kubectl rollout status` follows, with the pods being replaced.
type kubeRolloutStatus struct {
	Workload kubeWorkload `json:"workload"`
	// Done: every pod runs the latest template and is ready, as `kubectl rollout status` ends.
	Done bool `json:"done"`
	// Failed: the Deployment exceeded its progress deadline.
	Failed bool `json:"failed"`
	// Manual: pods are only replaced when deleted (OnDelete, StatefulSet partition), so a
	// restart replaces none on its own.
	Manual bool             `json:"manual"`
	Pods   []kubeRolloutPod `json:"pods"`
}

// KubeRolloutStatus is the rollout state of one Deployment, StatefulSet or DaemonSet and its
// pods, old and new, like `kubectl rollout status KIND/NAME -n NAMESPACE` (os:admin), polled
// by the apps while a restart rolls out: {workload:{...},done,failed,pods:[{name,status,
// healthy,ready,containers,restarts,node,created,updated}]}. kubeServer: see KubePods.
func KubeRolloutStatus(configYAML, contextName, kubeServer, kind, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeRolloutStatus { return demoRolloutStatus(wk.kind, namespace, name) },
		func(ctx context.Context, k *kubeClient) (kubeRolloutStatus, error) {
			return rolloutStatus(ctx, k, wk, namespace, name)
		})
}

func rolloutStatus(ctx context.Context, k *kubeClient, wk workloadKind, namespace, name string) (kubeRolloutStatus, error) {
	base := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/"

	var obj appsObject
	if err := k.get(ctx, base+wk.resource+"/"+url.PathEscape(name), &obj); err != nil {
		return kubeRolloutStatus{}, err
	}

	w := mapWorkload(wk.kind, obj)
	st := kubeRolloutStatus{
		Workload: w,
		Done:     w.State == workloadReady || w.State == workloadScaledDown,
		Failed:   progressDeadlineExceeded(obj),
		Manual:   manualUpdates(wk.kind, obj),
		Pods:     []kubeRolloutPod{},
	}

	selector, ok := selectorQuery(obj.Spec.Selector)
	if !ok {
		return st, nil
	}

	query := "?labelSelector=" + url.QueryEscape(selector)

	owns, updated, err := podRevision(ctx, k, wk, obj, base+"replicasets"+query)
	if err != nil {
		return kubeRolloutStatus{}, err
	}

	var pods kubeList[podObject]
	if err := getList(ctx, k, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods"+query, &pods); err != nil {
		return kubeRolloutStatus{}, err
	}

	// Until the controller sees the restart, the revision it reports is still the old one.
	stale := obj.Status.ObservedGeneration < obj.Metadata.Generation

	for _, p := range pods.Items {
		if owns(p) {
			st.Pods = append(st.Pods, mapRolloutPod(p, !stale && updated(p)))
		}
	}

	// The new pods first, the newest first: the ones the rollout is waiting for.
	sort.SliceStable(st.Pods, func(i, j int) bool {
		a, b := st.Pods[i], st.Pods[j]
		if a.Updated != b.Updated {
			return a.Updated
		}

		if a.Created != b.Created {
			return a.Created > b.Created
		}

		return a.Name < b.Name
	})

	return st, nil
}

// podRevision tells which pods belong to the workload (its selector may match others) and
// which of them run its latest pod template. replicaSets lists a Deployment's ReplicaSets.
func podRevision(ctx context.Context, k *kubeClient, wk workloadKind, obj appsObject, replicaSets string) (owns, updated func(podObject) bool, err error) {
	name := obj.Metadata.Name
	ownedBy := func(kind, owner string) func(podObject) bool {
		return func(p podObject) bool {
			for _, o := range p.Metadata.OwnerReferences {
				if o.Kind == kind && o.Name == owner {
					return true
				}
			}

			return false
		}
	}
	label := func(key, value string) func(podObject) bool {
		return func(p podObject) bool { return value != "" && p.Metadata.Labels[key] == value }
	}

	switch wk.kind {
	case "StatefulSet":
		return ownedBy(wk.kind, name), label(controllerRevisionLabel, obj.Status.UpdateRevision), nil
	case "DaemonSet":
		generation := obj.Metadata.Annotations[daemonSetTemplateGenerationKey]
		if generation == "" {
			generation = strconv.FormatInt(obj.Metadata.Generation, 10)
		}

		return ownedBy(wk.kind, name), label(podTemplateGenerationLabel, generation), nil
	}

	var list kubeList[replicaSetObject]
	if err := getList(ctx, k, replicaSets, &list); err != nil {
		return nil, nil, err
	}

	mine := map[string]bool{}
	newHash := ""

	for _, rs := range list.Items {
		if !rs.ownedBy(name) {
			continue
		}

		mine[rs.Metadata.Name] = true
		if rev := obj.Metadata.Annotations[deploymentRevisionKey]; rev != "" && rs.Metadata.Annotations[deploymentRevisionKey] == rev {
			newHash = rs.Metadata.Labels[podTemplateHashLabel]
		}
	}

	owns = func(p podObject) bool {
		for _, o := range p.Metadata.OwnerReferences {
			if o.Kind == "ReplicaSet" && mine[o.Name] {
				return true
			}
		}

		return false
	}

	return owns, label(podTemplateHashLabel, newHash), nil
}

// replicaSetObject holds the fields of a ReplicaSet podRevision reads.
type replicaSetObject struct {
	Metadata struct {
		Name            string            `json:"name"`
		Labels          map[string]string `json:"labels"`
		Annotations     map[string]string `json:"annotations"`
		OwnerReferences []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
}

func (rs replicaSetObject) ownedBy(deployment string) bool {
	for _, o := range rs.Metadata.OwnerReferences {
		if o.Kind == "Deployment" && o.Name == deployment {
			return true
		}
	}

	return false
}

// matchLabelsSelector is the labelSelector query of matchLabels, "" when there are none.
func matchLabelsSelector(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}

	sort.Strings(parts)

	return strings.Join(parts, ",")
}

// progressDeadlineExceeded tells a Deployment whose rollout the controller gave up on.
func progressDeadlineExceeded(obj appsObject) bool {
	for _, c := range obj.Status.Conditions {
		if c.Type == "Progressing" && c.Status == "False" && c.Reason == "ProgressDeadlineExceeded" {
			return true
		}
	}

	return false
}

func mapRolloutPod(obj podObject, updated bool) kubeRolloutPod {
	p := mapPod(obj)

	return kubeRolloutPod{
		Name: p.Name, Status: p.Status, Healthy: p.Healthy, Ready: p.Ready, Containers: p.Containers,
		Restarts: p.Restarts, Node: p.Node, Created: p.Created, Updated: updated,
	}
}

// demoRolloutStatus is a finished rollout of a demo workload, with its demo pods.
func demoRolloutStatus(kind, namespace, name string) kubeRolloutStatus {
	st := kubeRolloutStatus{Pods: []kubeRolloutPod{}}

	for _, w := range demoKubeWorkloads() {
		if w.Kind == kind && w.Namespace == namespace && w.Name == name {
			st.Workload = w
			st.Done = w.State == workloadReady
		}
	}

	for _, p := range demoPods() {
		owner := strings.TrimPrefix(p.Owner, kind+"/")
		if kind == "Deployment" {
			owner = strings.TrimPrefix(p.Owner, "ReplicaSet/")
			owner = owner[:max(strings.LastIndex(owner, "-"), 0)]
		}

		if p.Namespace == namespace && owner == name {
			st.Pods = append(st.Pods, kubeRolloutPod{
				Name: p.Name, Status: p.Status, Healthy: p.Healthy, Ready: p.Ready, Containers: p.Containers,
				Restarts: p.Restarts, Node: p.Node, Created: p.Created, Updated: true,
			})
		}
	}

	return st
}
