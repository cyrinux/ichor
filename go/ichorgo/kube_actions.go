package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Small kubectl actions: scale, suspend/resume a CronJob, roll a Deployment back to a
// revision, and pod logs through the Kubernetes API (the previous container's too). See
// plans/roadmap/devops/02-kubectl-actions.md.

const (
	// maxScaleReplicas bounds a scale typed on a phone.
	maxScaleReplicas = 1000
	// maxLogTail and maxLogBytes bound a log read: enough to diagnose, small for the phone.
	maxLogTail  = 5000
	maxLogBytes = 2 << 20

	changeCauseAnnotation = "kubernetes.io/change-cause"
)

// KubeScale sets the replicas of a Deployment or StatefulSet, like `kubectl scale
// KIND/NAME --replicas=N -n NAMESPACE` (os:admin). It returns a warning ("" when none) when
// a HorizontalPodAutoscaler manages the workload: it will change the count again.
// kubeServer: see KubePods.
func KubeScale(configYAML, contextName, kubeServer, kind, namespace, name string, replicas int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	if wk.kind == "DaemonSet" {
		return "", errors.New("a DaemonSet runs one pod per node: it cannot be scaled")
	}

	if replicas < 0 || replicas > maxScaleReplicas {
		return "", fmt.Errorf("replicas must be between 0 and %d", maxScaleReplicas)
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return "", err
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		path := appsPath(wk, namespace, name) + "/scale"
		patch := map[string]any{"spec": map[string]any{"replicas": replicas}}

		if err := k.patch(ctx, path, "application/merge-patch+json", patch, nil); err != nil {
			return err
		}

		out = autoscalerWarning(ctx, k, wk.kind, namespace, name)

		return nil
	})

	return out, err
}

// autoscalerWarning names the HorizontalPodAutoscaler that targets the workload, "" when
// none (or when they cannot be read: the scale itself succeeded).
func autoscalerWarning(ctx context.Context, k *kubeClient, kind, namespace, name string) string {
	var hpas struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				ScaleTargetRef struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"scaleTargetRef"`
				MinReplicas *int32 `json:"minReplicas"`
				MaxReplicas int32  `json:"maxReplicas"`
			} `json:"spec"`
		} `json:"items"`
	}

	if k.get(ctx, "/apis/autoscaling/v2/namespaces/"+url.PathEscape(namespace)+"/horizontalpodautoscalers", &hpas) != nil {
		return ""
	}

	for _, h := range hpas.Items {
		if h.Spec.ScaleTargetRef.Kind == kind && h.Spec.ScaleTargetRef.Name == name {
			lowest := int32(1)
			if h.Spec.MinReplicas != nil {
				lowest = *h.Spec.MinReplicas
			}

			return fmt.Sprintf("HorizontalPodAutoscaler %s manages the replicas (%d to %d): it will change them again",
				h.Metadata.Name, lowest, h.Spec.MaxReplicas)
		}
	}

	return ""
}

// KubeSuspendCronJob suspends (no new runs) or resumes a CronJob, like `kubectl patch
// cronjob NAME -p '{"spec":{"suspend":true}}'` (os:admin). Running jobs are not stopped.
// kubeServer: see KubePods.
func KubeSuspendCronJob(configYAML, contextName, kubeServer, namespace, name string, suspend bool) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("CronJob", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		path := "/apis/batch/v1/namespaces/" + url.PathEscape(namespace) + "/cronjobs/" + url.PathEscape(name)

		return k.patch(ctx, path, "application/merge-patch+json", map[string]any{"spec": map[string]any{"suspend": suspend}}, nil)
	})
}

// deploymentRevision is one revision of a Deployment (one of its ReplicaSets).
type deploymentRevision struct {
	Revision    int      `json:"revision"`
	ReplicaSet  string   `json:"replicaSet"`
	Created     int64    `json:"created"` // unix ms
	Images      []string `json:"images"`
	ChangeCause string   `json:"changeCause"`
	Replicas    int32    `json:"replicas"`
	Current     bool     `json:"current"`
}

// revisionReplicaSet holds the fields of a ReplicaSet a revision list and a rollback read.
type revisionReplicaSet struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
		Annotations       map[string]string `json:"annotations"`
		OwnerReferences   []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Controller *bool  `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Template json.RawMessage `json:"template"`
	} `json:"spec"`
	Status struct {
		Replicas int32 `json:"replicas"`
	} `json:"status"`
}

func (rs revisionReplicaSet) ownedBy(deployment string) bool {
	for _, o := range rs.Metadata.OwnerReferences {
		if o.Kind == "Deployment" && o.Name == deployment && o.Controller != nil && *o.Controller {
			return true
		}
	}

	return false
}

func (rs revisionReplicaSet) revision() int {
	n, _ := strconv.Atoi(rs.Metadata.Annotations[deploymentRevisionKey])

	return n
}

// KubeDeploymentRevisions lists a Deployment's revisions, newest first, like `kubectl
// rollout history` (os:admin): {"revisions":[{revision,replicaSet,created,images,
// changeCause,replicas,current}]}. kubeServer: see KubePods.
func KubeDeploymentRevisions(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("Deployment", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoDeploymentRevisions,
		func(ctx context.Context, k *kubeClient) (map[string][]deploymentRevision, error) {
			revs, _, err := deploymentRevisions(ctx, k, namespace, name)

			return map[string][]deploymentRevision{"revisions": revs}, err
		})
}

// deploymentRevisions reads the Deployment and its ReplicaSets; the ReplicaSets come back
// by revision for a rollback.
func deploymentRevisions(ctx context.Context, k *kubeClient, namespace, name string) ([]deploymentRevision, map[int]revisionReplicaSet, error) {
	var dep appsObject
	if err := k.get(ctx, appsPath(workloadKinds[0], namespace, name), &dep); err != nil {
		return nil, nil, err
	}

	var list struct {
		Items []revisionReplicaSet `json:"items"`
	}

	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/replicasets?labelSelector=" + url.QueryEscape(matchLabelsSelector(dep.Spec.Selector.MatchLabels))
	if err := k.get(ctx, path, &list); err != nil {
		return nil, nil, err
	}

	current, _ := strconv.Atoi(dep.Metadata.Annotations[deploymentRevisionKey])
	byRevision := map[int]revisionReplicaSet{}
	revs := []deploymentRevision{}

	for _, rs := range list.Items {
		n := rs.revision()
		if !rs.ownedBy(name) || n == 0 {
			continue
		}

		byRevision[n] = rs
		revs = append(revs, deploymentRevision{
			Revision:    n,
			ReplicaSet:  rs.Metadata.Name,
			Created:     rs.Metadata.CreationTimestamp.UnixMilli(),
			Images:      templateImages(rs.Spec.Template),
			ChangeCause: rs.Metadata.Annotations[changeCauseAnnotation],
			Replicas:    rs.Status.Replicas,
			Current:     n == current,
		})
	}

	sort.Slice(revs, func(i, j int) bool { return revs[i].Revision > revs[j].Revision })

	return revs, byRevision, nil
}

func templateImages(template json.RawMessage) []string {
	var t struct {
		Spec struct {
			Containers []struct {
				Image string `json:"image"`
			} `json:"containers"`
		} `json:"spec"`
	}

	_ = json.Unmarshal(template, &t) //nolint:errcheck

	images := []string{}
	for _, c := range t.Spec.Containers {
		images = append(images, c.Image)
	}

	return images
}

// KubeRollbackDeployment rolls a Deployment back to revision, like `kubectl rollout undo
// deployment/NAME --to-revision=N` (os:admin): its pod template becomes that ReplicaSet's
// (without the pod-template-hash label), which starts a rolling update to it (it then
// shows as the newest revision). A paused Deployment, and the current revision, are
// refused. kubeServer: see KubePods.
func KubeRollbackDeployment(configYAML, contextName, kubeServer, namespace, name string, revision int) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("Deployment", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return rollbackDeployment(ctx, k, namespace, name, revision)
	})
}

func rollbackDeployment(ctx context.Context, k *kubeClient, namespace, name string, revision int) error {
	path := appsPath(workloadKinds[0], namespace, name)

	var dep appsObject
	if err := k.get(ctx, path, &dep); err != nil {
		return err
	}

	if dep.Spec.Paused {
		return errPausedDeployment
	}

	revs, byRevision, err := deploymentRevisions(ctx, k, namespace, name)
	if err != nil {
		return err
	}

	rs, ok := byRevision[revision]
	if !ok {
		return fmt.Errorf("revision %d of %s is not kept anymore (see revisionHistoryLimit)", revision, name)
	}

	for _, r := range revs {
		if r.Revision == revision && r.Current {
			return fmt.Errorf("revision %d is already the current one", revision)
		}
	}

	template, err := rollbackTemplate(rs.Spec.Template)
	if err != nil {
		return err
	}

	// A JSON patch replaces the whole template, as kubectl does: a merge would keep
	// containers or env vars the old revision did not have.
	patch := []map[string]any{
		{"op": "replace", "path": "/spec/template", "value": template},
		{"op": "replace", "path": "/metadata/annotations", "value": rollbackAnnotations(dep.Metadata.Annotations, rs.Metadata.Annotations)},
	}

	return k.patch(ctx, path, "application/json-patch+json", patch, nil)
}

// rollbackSkippedAnnotations are kept from the Deployment on a rollback: the controller owns
// them. Every other annotation comes from the revision's ReplicaSet (its change-cause, for
// one), like `kubectl rollout undo`.
var rollbackSkippedAnnotations = []string{
	"kubectl.kubernetes.io/last-applied-configuration",
	deploymentRevisionKey,
	"deployment.kubernetes.io/revision-history",
	"deployment.kubernetes.io/desired-replicas",
	"deployment.kubernetes.io/max-replicas",
	"deprecated.deployment.rollback.to",
}

func rollbackAnnotations(deployment, replicaSet map[string]string) map[string]string {
	out := map[string]string{}

	for _, key := range rollbackSkippedAnnotations {
		if v, ok := deployment[key]; ok {
			out[key] = v
		}
	}

	for k, v := range replicaSet {
		if !slices.Contains(rollbackSkippedAnnotations, k) {
			out[k] = v
		}
	}

	return out
}

// rollbackTemplate is a ReplicaSet's pod template without the label the Deployment
// controller adds to it, so the controller recognises the template as that revision.
func rollbackTemplate(raw json.RawMessage) (map[string]any, error) {
	var template map[string]any
	if err := json.Unmarshal(raw, &template); err != nil || template == nil {
		return nil, errors.New("the revision's pod template cannot be read")
	}

	if meta, ok := template["metadata"].(map[string]any); ok {
		if labels, ok := meta["labels"].(map[string]any); ok {
			delete(labels, podTemplateHashLabel)
		}
	}

	return template, nil
}

// KubePodLogs reads a container's log through the Kubernetes API, like `kubectl logs POD
// -c CONTAINER [--previous] --tail=N` (os:admin): with previous, the log of the container's
// last terminated run, which Talos no longer has (it only keeps the running container's).
// tailLines is capped at 5000 and the answer at its newest 2 MiB. container may be "" for a pod with
// one container. kubeServer: see KubePods.
func KubePodLogs(configYAML, contextName, kubeServer, namespace, pod, container string, previous bool, tailLines int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	container = privacy.reveal(strings.TrimSpace(container))

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if container != "" && !kubeNamePattern.MatchString(container) {
		return "", fmt.Errorf("invalid container name %q", container)
	}

	if tailLines <= 0 || tailLines > maxLogTail {
		tailLines = maxLogTail
	}

	// No limitBytes: Kubernetes applies it to the start of the tail, which would drop the
	// newest lines (the crash the log was opened for). The size is bounded below instead.
	query := url.Values{}
	query.Set("tailLines", strconv.Itoa(tailLines))

	if container != "" {
		query.Set("container", container)
	}

	if previous {
		query.Set("previous", "true")
	}

	if isDemoContext(configYAML, contextName) {
		return demoPodLog(pod, previous), nil
	}

	return withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (string, error) {
		log, err := k.getText(ctx, podPath(namespace, pod)+"/log?"+query.Encode())

		return newestBytes(log, maxLogBytes), err
	})
}

// newestBytes keeps the end of log, at most max bytes, cut at a line start.
func newestBytes(log string, max int) string {
	if len(log) <= max {
		return log
	}

	log = log[len(log)-max:]
	if i := strings.IndexByte(log, '\n'); i >= 0 {
		log = log[i+1:]
	}

	return log
}

func appsPath(wk workloadKind, namespace, name string) string {
	return "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/" + wk.resource + "/" + url.PathEscape(name)
}
