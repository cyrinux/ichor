package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// restartedAtAnnotation is what `kubectl rollout restart` sets on the pod template: any
// change of the template makes the controller replace the pods with a rolling update.
const restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// workloadKind is a restartable workload type of the apps/v1 API.
type workloadKind struct {
	kind     string // Deployment
	resource string // deployments
}

var workloadKinds = []workloadKind{
	{kind: "Deployment", resource: "deployments"},
	{kind: "StatefulSet", resource: "statefulsets"},
	{kind: "DaemonSet", resource: "daemonsets"},
}

func findWorkloadKind(kind string) (workloadKind, error) {
	for _, k := range workloadKinds {
		if strings.EqualFold(k.kind, strings.TrimSpace(kind)) {
			return k, nil
		}
	}

	return workloadKind{}, fmt.Errorf("unsupported workload kind %q (Deployment, StatefulSet, DaemonSet)", kind)
}

// Workload states, from the counters the controllers report.
const (
	workloadReady       = "ready"
	workloadProgressing = "progressing"
	workloadDegraded    = "degraded"
	workloadPaused      = "paused"
	workloadScaledDown  = "scaledDown"
)

type kubeWorkload struct {
	Kind        string   `json:"kind"`
	Namespace   string   `json:"namespace"`
	Name        string   `json:"name"`
	Desired     int32    `json:"desired"`
	Ready       int32    `json:"ready"`
	Updated     int32    `json:"updated"`
	Available   int32    `json:"available"`
	State       string   `json:"state"`
	RestartedAt int64    `json:"restartedAt"` // unix ms of the last rollout restart, 0 when never
	Created     int64    `json:"created"`     // unix ms
	Images      []string `json:"images"`
}

type kubeWorkloadList struct {
	Workloads []kubeWorkload `json:"workloads"`
}

// appsObject holds the fields of a Deployment, StatefulSet or DaemonSet the app reads.
type appsObject struct {
	Metadata struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		Generation        int64     `json:"generation"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int32 `json:"replicas"`
		Paused   bool   `json:"paused"`
		Template struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration int64 `json:"observedGeneration"`
		// Deployment and StatefulSet.
		Replicas          int32 `json:"replicas"`
		ReadyReplicas     int32 `json:"readyReplicas"`
		UpdatedReplicas   int32 `json:"updatedReplicas"`
		AvailableReplicas int32 `json:"availableReplicas"`
		// DaemonSet.
		DesiredNumberScheduled int32 `json:"desiredNumberScheduled"`
		NumberReady            int32 `json:"numberReady"`
		UpdatedNumberScheduled int32 `json:"updatedNumberScheduled"`
		NumberAvailable        int32 `json:"numberAvailable"`
	} `json:"status"`
}

type appsObjectList struct {
	Items []appsObject `json:"items"`
}

// KubeWorkloads lists the Deployments, StatefulSets and DaemonSets of every namespace with
// their rollout state, through the Kubernetes API with the admin kubeconfig Talos issues
// (os:admin): {"workloads":[{kind,namespace,name,desired,ready,updated,available,state,
// restartedAt,created,images}]}.
func KubeWorkloads(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return toJSON(kubeWorkloadList{Workloads: demoKubeWorkloads()})
	}

	list, err := withKube(configYAML, contextName, listWorkloads)
	if err != nil {
		return "", err
	}

	return toJSON(list)
}

func listWorkloads(ctx context.Context, k *kubeClient) (kubeWorkloadList, error) {
	results := make([][]kubeWorkload, len(workloadKinds))
	errs := make([]error, len(workloadKinds))

	var wg sync.WaitGroup

	for i, kind := range workloadKinds {
		wg.Go(func() {
			var list appsObjectList

			errs[i] = k.get(ctx, "/apis/apps/v1/"+kind.resource, &list)
			for _, obj := range list.Items {
				results[i] = append(results[i], mapWorkload(kind.kind, obj))
			}
		})
	}

	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return kubeWorkloadList{}, err
	}

	workloads := []kubeWorkload{}
	for _, r := range results {
		workloads = append(workloads, r...)
	}

	sort.Slice(workloads, func(i, j int) bool {
		a, b := workloads[i], workloads[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}

		if a.Name != b.Name {
			return a.Name < b.Name
		}

		return a.Kind < b.Kind
	})

	return kubeWorkloadList{Workloads: workloads}, nil
}

func mapWorkload(kind string, obj appsObject) kubeWorkload {
	w := kubeWorkload{Kind: kind, Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Images: []string{}}

	if !obj.Metadata.CreationTimestamp.IsZero() {
		w.Created = obj.Metadata.CreationTimestamp.UnixMilli()
	}

	if at, err := time.Parse(time.RFC3339, obj.Spec.Template.Metadata.Annotations[restartedAtAnnotation]); err == nil {
		w.RestartedAt = at.UnixMilli()
	}

	for _, c := range obj.Spec.Template.Spec.Containers {
		w.Images = append(w.Images, c.Image)
	}

	st := obj.Status
	if kind == "DaemonSet" {
		w.Desired, w.Ready, w.Updated, w.Available = st.DesiredNumberScheduled, st.NumberReady, st.UpdatedNumberScheduled, st.NumberAvailable
	} else {
		w.Desired, w.Ready, w.Updated, w.Available = 1, st.ReadyReplicas, st.UpdatedReplicas, st.AvailableReplicas
		if obj.Spec.Replicas != nil {
			w.Desired = *obj.Spec.Replicas
		}
	}

	w.State = workloadState(w, obj.Spec.Paused, obj.Status.ObservedGeneration < obj.Metadata.Generation)

	return w
}

func workloadState(w kubeWorkload, paused, stale bool) string {
	switch {
	case paused:
		return workloadPaused
	case w.Desired == 0:
		return workloadScaledDown
	case stale || w.Updated < w.Desired:
		return workloadProgressing
	case w.Ready < w.Desired:
		return workloadDegraded
	default:
		return workloadReady
	}
}

// KubeRolloutRestart restarts the pods of a Deployment, StatefulSet or DaemonSet with a
// rolling update, like `kubectl rollout restart KIND/NAME -n NAMESPACE` (os:admin): it
// stamps the pod template with the restart time and the controller replaces the pods. A
// paused Deployment is refused, as kubectl does.
func KubeRolloutRestart(configYAML, contextName, kind, namespace, name string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return err
	}

	if namespace == "" || name == "" {
		return errors.New("no workload namespace or name given")
	}

	if isDemoContext(configYAML, contextName) {
		return demoUnavailable
	}

	_, err = withKube(configYAML, contextName, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, rolloutRestart(ctx, k, wk, namespace, name, time.Now())
	})

	return err
}

func rolloutRestart(ctx context.Context, k *kubeClient, wk workloadKind, namespace, name string, now time.Time) error {
	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/" + wk.resource + "/" + url.PathEscape(name)

	if wk.kind == "Deployment" {
		var obj appsObject
		if err := k.get(ctx, path, &obj); err != nil {
			return err
		}

		if obj.Spec.Paused {
			return errPausedDeployment
		}
	}

	patch := map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
		"annotations": map[string]string{restartedAtAnnotation: now.Format(time.RFC3339)},
	}}}}

	return k.patch(ctx, path, "application/strategic-merge-patch+json", patch, nil)
}

var errPausedDeployment = &kubeAPIError{Code: 409, Reason: "Paused", Message: "the deployment is paused: resume its rollout first"}
