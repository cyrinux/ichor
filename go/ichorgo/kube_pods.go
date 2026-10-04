package ichorgo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type kubePod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Status is what `kubectl get pods` shows: Running, Pending, CrashLoopBackOff,
	// Init:Error, Terminating, Completed...
	Status     string   `json:"status"`
	Healthy    bool     `json:"healthy"` // running with every container ready, or completed
	Ready      int      `json:"ready"`
	Containers int      `json:"containers"`
	Restarts   int      `json:"restarts"`
	Node       string   `json:"node"`
	Owner      string   `json:"owner"` // "ReplicaSet/web-5d8f", "" when none
	Created    int64    `json:"created"`
	Images     []string `json:"images"`
	// ContainerNames are the pod's containers, to pick one for its logs.
	ContainerNames []string `json:"containerNames"`
	// LastTermination is why a restarted container last stopped ("OOMKilled (exit 137)"),
	// "" when none did: what the previous run's log explains.
	LastTermination string `json:"lastTermination"`
}

type kubePodList struct {
	Pods []kubePod `json:"pods"`
}

type containerState struct {
	Waiting *struct {
		Reason string `json:"reason"`
	} `json:"waiting"`
	Terminated *struct {
		Reason   string `json:"reason"`
		ExitCode int    `json:"exitCode"`
		Signal   int    `json:"signal"`
	} `json:"terminated"`
}

type containerStatus struct {
	Ready        bool           `json:"ready"`
	RestartCount int            `json:"restartCount"`
	State        containerState `json:"state"`
	LastState    containerState `json:"lastState"`
}

type podObject struct {
	Metadata struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
		DeletionTimestamp *time.Time        `json:"deletionTimestamp"`
		Labels            map[string]string `json:"labels"`
		OwnerReferences   []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		NodeName       string `json:"nodeName"`
		InitContainers []struct {
			Image string `json:"image"`
		} `json:"initContainers"`
		Containers []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase                 string            `json:"phase"`
		Reason                string            `json:"reason"`
		InitContainerStatuses []containerStatus `json:"initContainerStatuses"`
		ContainerStatuses     []containerStatus `json:"containerStatuses"`
	} `json:"status"`
}

// KubePods lists the pods of every namespace with the status `kubectl get pods` shows,
// through the Kubernetes API with the admin kubeconfig Talos issues (os:admin):
// {"pods":[{namespace,name,status,healthy,ready,containers,restarts,node,owner,created,images}]}.
// kubeServer is the API server address the user set for the cluster (see
// NormalizeKubeServer), "" for the kubeconfig's own.
func KubePods(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() kubePodList { return kubePodList{Pods: demoPods()} }, listPods)
}

func listPods(ctx context.Context, k *kubeClient) (kubePodList, error) {
	var list kubeList[podObject]

	if err := k.get(ctx, "/api/v1/pods", &list); err != nil {
		return kubePodList{}, err
	}

	pods := make([]kubePod, 0, len(list.Items))
	for _, obj := range list.Items {
		pods = append(pods, mapPod(obj))
	}

	sort.Slice(pods, func(i, j int) bool {
		if pods[i].Namespace != pods[j].Namespace {
			return pods[i].Namespace < pods[j].Namespace
		}

		return pods[i].Name < pods[j].Name
	})

	return kubePodList{Pods: pods}, nil
}

func mapPod(obj podObject) kubePod {
	p := kubePod{
		Namespace:      obj.Metadata.Namespace,
		Name:           obj.Metadata.Name,
		Node:           obj.Spec.NodeName,
		Containers:     len(obj.Spec.Containers),
		Images:         []string{},
		ContainerNames: []string{},
	}

	if !obj.Metadata.CreationTimestamp.IsZero() {
		p.Created = obj.Metadata.CreationTimestamp.UnixMilli()
	}

	if owners := obj.Metadata.OwnerReferences; len(owners) > 0 {
		p.Owner = owners[0].Kind + "/" + owners[0].Name
	}

	for _, c := range obj.Spec.Containers {
		p.Images = append(p.Images, c.Image)
		p.ContainerNames = append(p.ContainerNames, c.Name)
	}

	mostRestarts := 0

	for _, cs := range obj.Status.ContainerStatuses {
		p.Restarts += cs.RestartCount
		if cs.Ready {
			p.Ready++
		}

		if t := cs.LastState.Terminated; t != nil && cs.RestartCount > mostRestarts {
			mostRestarts = cs.RestartCount
			p.LastTermination = fmt.Sprintf("%s (exit %d)", terminatedReason(t.Reason, t.Signal, t.ExitCode), t.ExitCode)
		}
	}

	p.Status = podStatus(obj)
	p.Healthy = (p.Status == "Running" && p.Ready == p.Containers) || p.Status == "Completed"

	return p
}

// podStatus follows the STATUS column of `kubectl get pods` (printPod in kubectl).
func podStatus(obj podObject) string {
	st := obj.Status

	reason := st.Phase
	if st.Reason != "" {
		reason = st.Reason
	}

	initializing := false

	for i, cs := range st.InitContainerStatuses {
		switch {
		case cs.State.Terminated != nil && cs.State.Terminated.ExitCode == 0:
			continue
		case cs.State.Terminated != nil:
			reason = "Init:" + terminatedReason(cs.State.Terminated.Reason, cs.State.Terminated.Signal, cs.State.Terminated.ExitCode)
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + cs.State.Waiting.Reason
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(obj.Spec.InitContainers))
		}

		initializing = true

		break
	}

	if !initializing {
		hasRunning := false

		for i := len(st.ContainerStatuses) - 1; i >= 0; i-- {
			cs := st.ContainerStatuses[i]

			switch {
			case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
				reason = cs.State.Waiting.Reason
			case cs.State.Terminated != nil:
				reason = terminatedReason(cs.State.Terminated.Reason, cs.State.Terminated.Signal, cs.State.Terminated.ExitCode)
			case cs.Ready && cs.State.Waiting == nil && cs.State.Terminated == nil:
				hasRunning = true
			}
		}

		// A pod with a running container that completed others still shows Running.
		if reason == "Completed" && hasRunning {
			reason = "Running"
		}
	}

	if obj.Metadata.DeletionTimestamp != nil {
		if st.Reason == "NodeLost" {
			return "Unknown"
		}

		return "Terminating"
	}

	return reason
}

func terminatedReason(reason string, signal, exitCode int) string {
	switch {
	case reason != "":
		return reason
	case signal != 0:
		return fmt.Sprintf("Signal:%d", signal)
	default:
		return fmt.Sprintf("ExitCode:%d", exitCode)
	}
}

// KubeDeletePod deletes a pod like `kubectl delete pod NAME -n NAMESPACE` (os:admin): its
// controller, if any, starts a new one. The pod gets its usual grace period. kubeServer: see KubePods.
func KubeDeletePod(configYAML, contextName, kubeServer, namespace, name string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("pod", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return k.do(ctx, http.MethodDelete, podPath(namespace, name), "", nil, nil)
	})
}

func podPath(namespace, name string) string {
	return "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(name)
}
