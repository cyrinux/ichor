package ichorgo

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// `kubectl debug node/NAME`, for clusters without Talos: a privileged pod pinned to the node
// with the host's PID, network and IPC namespaces, then `nsenter -t 1` into the host's mount,
// UTS, IPC, network and PID namespaces for a root shell on the node. The pod is deleted when
// the shell ends; its labels let the next run sweep one an app kill left behind, and its
// active deadline ends it anyway.

const (
	nodeDebugApp       = "ichor-node-debug"
	nodeDebugContainer = "debug"
	// nodeDebugLifetime bounds the pod whatever happens to the app: a backstop, not the plan.
	nodeDebugLifetime = 4 * time.Hour
	// nodeDebugStaleAge is when a debug pod left behind is swept by the next run: past its
	// deadline, so never a shell still in use (on another phone, by a colleague).
	nodeDebugStaleAge = nodeDebugLifetime + 10*time.Minute
	// nodeDebugCleanupTimeout bounds the pod deletion, which runs after the session's end.
	nodeDebugCleanupTimeout = 15 * time.Second
)

// nodeDebugShell enters every namespace of the host's PID 1, then runs its bash, or its sh
// where there is no bash.
var nodeDebugShell = []string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "sh", "-c", "exec bash || exec sh"}

// StartNodeDebug opens a root shell on a Kubernetes node through a privileged pod, like
// `kubectl debug node/NODE -it --image=IMAGE --profile=sysadmin` followed by nsenter. The pod
// runs image ("" for busybox, which has nsenter) in namespace ("" for default), which must
// admit privileged pods: a Pod Security Admission refusal is reported as the API server words
// it. Recorded in the audit log; the pod is deleted when the shell ends. Output and exit go
// to listener, as for StartPodShell.
func StartNodeDebug(configYAML, contextName, kubeServer, nodeName, namespace, image string, cols, rows int, listener DebugListener) *DebugSession {
	contextName = unmaskContext(configYAML, contextName)
	nodeName = privacy.reveal(strings.TrimSpace(nodeName))
	namespace = cmpOr(privacy.revealNamespace(strings.TrimSpace(namespace)), "default")
	image = cmpOr(strings.TrimSpace(image), podDebugDefaultImage)

	d, ctx := newDebugSession(listener)
	target := kubeTarget{configYAML, contextName, kubeServer}

	// A refused input is not an action: nothing to record.
	if err := validateNodeDebug(nodeName, namespace, image); err != nil {
		d.exit(-1, err.Error())
		d.cancel()

		return d
	}

	go func() {
		defer d.cancel()
		defer onPanic(func(msg string) { d.exit(-1, msg) })

		var k *kubeClient

		name := nodeDebugPodName()

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{
				Server: kubeServer, Action: "debug-node", Namespace: namespace, Object: "Node/" + nodeName,
				Params: fmt.Sprintf("image=%s,pod=%s", image, name),
			}
		}, func() (err error) {
			if isDemoContext(configYAML, contextName) {
				return errDemoUnavailable
			}

			if k, _, err = kubeClients.get(target); err != nil {
				return kubeError(err)
			}

			return startNodeDebugPod(ctx, k, namespace, name, nodeName, image, d.listener.OnStatus)
		})
		if err != nil {
			d.exitDialFailed(ctx, err)

			return
		}

		// Deleted before OnExit: the app (or the probe) may end the process right after it.
		d.beforeExit = func() { deleteNodeDebugPod(ctx, k, namespace, name) }

		d.listener.OnStatus("Root shell on " + privacy.maskPlain(nodeName) + " (pod " + name + "). If no prompt shows, press Enter.")

		ws, err := k.dialExec(ctx, namespace, name, nodeDebugContainer, nodeDebugShell, true)
		if err != nil {
			d.exitDialFailed(ctx, err)

			return
		}

		d.serveTTY(ctx, ws, cols, rows, false)
	}()

	return d
}

func validateNodeDebug(nodeName, namespace, image string) error {
	if !kubeNamePattern.MatchString(nodeName) || strings.Contains(nodeName, "..") {
		return fmt.Errorf("invalid Kubernetes node name %q", nodeName)
	}

	if err := validateNamespace(namespace); err != nil {
		return err
	}

	return validateDebugImage(image)
}

// startNodeDebugPod sweeps the debug pods earlier runs left, creates this one and waits until
// it runs. A pod that cannot start is deleted before the error is returned.
func startNodeDebugPod(ctx context.Context, k *kubeClient, namespace, name, node, image string, status func(string)) error {
	sweepNodeDebugPods(ctx, k, namespace, time.Now())

	// Masked here: the scanner leaves a name glued to the ellipsis alone.
	status("Creating privileged pod " + name + " on " + privacy.maskPlain(node) + "…")

	if err := k.post(ctx, netPerfNamespacePath(namespace)+"/pods", nodeDebugPodSpec(name, node, image), nil); err != nil {
		// No answer: the pod may exist anyway.
		deleteNodeDebugPod(ctx, k, namespace, name)

		return kubeMutationError(err)
	}

	_, err := waitRunPod(ctx, k, "node debug", namespace, name, node, podDebugStartTimeout,
		func(reason string) {
			if reason != "" {
				status("Pod " + name + ": " + reason + "…")
			}
		},
		func(p netPerfPod) bool { return p.Status.Phase == "Running" })
	if err != nil {
		deleteNodeDebugPod(ctx, k, namespace, name)

		// A pod that cannot start says why in its own words; anything else is the API's.
		if refusal := (*netPerfRefusal)(nil); errors.As(err, &refusal) {
			return refusal
		}

		return kubeError(err)
	}

	return nil
}

// nodeDebugPodSpec is the pod `kubectl debug node/` creates with the sysadmin profile: on
// node, in the host's namespaces, privileged, tolerating every taint, ended after
// nodeDebugLifetime whatever happens to the app.
func nodeDebugPodSpec(name, node, image string) map[string]any {
	container := map[string]any{
		"name":                     nodeDebugContainer,
		"image":                    image,
		"imagePullPolicy":          "IfNotPresent",
		"command":                  []string{"sleep", fmt.Sprint(int(nodeDebugLifetime.Seconds()))},
		"stdin":                    true,
		"tty":                      true,
		"terminationMessagePolicy": "File",
		"securityContext":          map[string]any{"privileged": true},
	}

	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":        name,
			"labels":      map[string]string{"app.kubernetes.io/name": nodeDebugApp, "app.kubernetes.io/managed-by": netPerfManagedBy},
			"annotations": map[string]string{"ichor.levis.name/node": node},
		},
		"spec": map[string]any{
			"nodeName":                      node,
			"hostPID":                       true,
			"hostNetwork":                   true,
			"hostIPC":                       true,
			"tolerations":                   []map[string]string{{"operator": "Exists"}},
			"restartPolicy":                 "Never",
			"activeDeadlineSeconds":         int(nodeDebugLifetime.Seconds()),
			"terminationGracePeriodSeconds": 1,
			"automountServiceAccountToken":  false,
			"enableServiceLinks":            false,
			"containers":                    []map[string]any{container},
		},
	}
}

// nodeDebugPodName is "ichor-node-debug-xxxxx": no node in it (its annotation has it), so a
// long node name is never cut, and the name never shows a node the privacy mask hides.
func nodeDebugPodName() string {
	suffix := make([]byte, podDebugNameLength)
	_, _ = rand.Read(suffix)

	for i, b := range suffix {
		suffix[i] = podDebugNameAlphabet[int(b)%len(podDebugNameAlphabet)]
	}

	return nodeDebugApp + "-" + string(suffix)
}

// deleteNodeDebugPod deletes the debug pod at once, also when ctx is already done.
func deleteNodeDebugPod(ctx context.Context, k *kubeClient, namespace, name string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), nodeDebugCleanupTimeout)
	defer cancel()

	_ = k.do(ctx, http.MethodDelete, podPath(namespace, name)+"?gracePeriodSeconds=0", "", nil, nil) //nolint:errcheck // the deadline and the next sweep remain
}

// nodeDebugPodObject is what the sweep reads of a debug pod.
type nodeDebugPodObject struct {
	Metadata struct {
		Name              string     `json:"name"`
		CreationTimestamp time.Time  `json:"creationTimestamp"`
		DeletionTimestamp *time.Time `json:"deletionTimestamp"`
	} `json:"metadata"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// sweepNodeDebugPods deletes the debug pods of namespace a run could not delete: finished,
// or past their deadline (nodeDebugStaleAge). A running one may be someone's shell: kept. Best effort.
func sweepNodeDebugPods(ctx context.Context, k *kubeClient, namespace string, now time.Time) {
	var list kubeList[nodeDebugPodObject]

	selector := url.QueryEscape("app.kubernetes.io/name=" + nodeDebugApp + ",app.kubernetes.io/managed-by=" + netPerfManagedBy)
	if getList(ctx, k, netPerfNamespacePath(namespace)+"/pods?labelSelector="+selector, &list) != nil {
		return
	}

	for _, p := range list.Items {
		m := p.Metadata
		finished := p.Status.Phase == "Succeeded" || p.Status.Phase == "Failed"

		if m.DeletionTimestamp == nil && strings.HasPrefix(m.Name, nodeDebugApp+"-") && (finished || now.Sub(m.CreationTimestamp) > nodeDebugStaleAge) {
			deleteNodeDebugPod(ctx, k, namespace, m.Name)
		}
	}
}
