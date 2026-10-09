package ichorgo

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// netPerfPoll is how often a pod's status is read while waiting on it.
var netPerfPoll = time.Second

const netPerfContainer = "netperf"

type netPerfNode struct {
	Name         string `json:"name"`
	Address      string `json:"address"`
	ControlPlane bool   `json:"controlPlane"`
	Ready        bool   `json:"ready"`
}

type netPerfNodeList struct {
	Nodes []netPerfNode `json:"nodes"`
}

type nodeObject struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		Addresses []struct {
			Type    string `json:"type"`
			Address string `json:"address"`
		} `json:"addresses"`
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

func listNetPerfNodes(ctx context.Context, k *kubeClient) (netPerfNodeList, error) {
	var list kubeList[nodeObject]

	if err := getList(ctx, k, "/api/v1/nodes", &list); err != nil {
		return netPerfNodeList{}, err
	}

	out := netPerfNodeList{Nodes: make([]netPerfNode, 0, len(list.Items))}
	for _, obj := range list.Items {
		out.Nodes = append(out.Nodes, mapNetPerfNode(obj))
	}

	slices.SortFunc(out.Nodes, func(a, b netPerfNode) int { return strings.Compare(a.Name, b.Name) })

	return out, nil
}

func mapNetPerfNode(obj nodeObject) netPerfNode {
	n := netPerfNode{Name: obj.Metadata.Name}
	_, n.ControlPlane = obj.Metadata.Labels["node-role.kubernetes.io/control-plane"]

	for _, a := range obj.Status.Addresses {
		if a.Type == "InternalIP" && n.Address == "" {
			n.Address = a.Address
		}
	}

	for _, c := range obj.Status.Conditions {
		if c.Type == "Ready" {
			n.Ready = c.Status == "True"
		}
	}

	return n
}

// checkNetPerfNodes refuses a test on a node that is not in the cluster or not ready.
func checkNetPerfNodes(ctx context.Context, k *kubeClient, opts netPerfOptions) error {
	list, err := listNetPerfNodes(ctx, k)
	if err != nil {
		return err
	}

	for _, name := range []string{opts.server, opts.client} {
		i := slices.IndexFunc(list.Nodes, func(n netPerfNode) bool { return n.Name == name })

		switch {
		case i < 0:
			return netPerfRefused("node %s is not in the cluster", name)
		case !list.Nodes[i].Ready:
			return netPerfRefused("node %s is not ready", name)
		}
	}

	return nil
}

func netPerfNamespacePath(ns string) string { return "/api/v1/namespaces/" + url.PathEscape(ns) }

// createNetPerfNamespace creates a namespace with a random name, so a test never meets the
// one before it still terminating. The host network needs the privileged pod security level.
func createNetPerfNamespace(ctx context.Context, k *kubeClient, hostNetwork bool) (string, error) {
	return createRunNamespace(ctx, k, netPerfName, hostNetwork)
}

// createRunNamespace creates a namespace for one run of app's pods, named and labelled after
// app with a random suffix.
func createRunNamespace(ctx context.Context, k *kubeClient, app string, hostNetwork bool) (string, error) {
	suffix := make([]byte, 5)
	alphabet := "abcdefghijklmnopqrstuvwxyz0123456789"

	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}

	for i, b := range suffix {
		suffix[i] = alphabet[int(b)%len(alphabet)]
	}

	ns := app + "-" + string(suffix)
	labels := map[string]string{
		"app.kubernetes.io/name":       app,
		"app.kubernetes.io/managed-by": netPerfManagedBy,
	}

	if hostNetwork {
		labels["pod-security.kubernetes.io/enforce"] = "privileged"
	}

	body := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": ns, "labels": labels},
	}

	return ns, k.post(ctx, "/api/v1/namespaces", body, nil)
}

// deleteNetPerfNamespace deletes ns and the pods in it, also when ctx is already done. A
// DELETE that gets no answer or a server error is tried again, within netPerfCleanupTimeout;
// a namespace already gone is deleted.
func deleteNetPerfNamespace(ctx context.Context, k *kubeClient, ns string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), netPerfCleanupTimeout)
	defer cancel()

	var err error

	for attempt := 1; ; attempt++ {
		err = k.do(ctx, http.MethodDelete, netPerfNamespacePath(ns), "", nil, nil)
		if err == nil || isNotFound(err) {
			return nil
		}

		if attempt == netPerfCleanupAttempts || !retryableCleanup(err) {
			return err
		}

		select {
		case <-ctx.Done():
			return err
		case <-time.After(netPerfCleanupRetry):
		}
	}
}

// NetPerfDeleteNamespace deletes a namespace a network test left behind (the app was closed
// mid-run), as the checkup lists it. Only an ichor-netperf-* namespace Ichor labelled as its
// own is deleted, whatever the caller passes. kubeServer: see KubePods.
func NetPerfDeleteNamespace(configYAML, contextName, kubeServer, namespace string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.reveal(strings.TrimSpace(namespace))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "delete-netperf-namespace", Object: "Namespace/" + namespace})

	if !kubeNamePattern.MatchString(namespace) || strings.Contains(namespace, "..") {
		return fmt.Errorf("invalid Kubernetes name %q", namespace)
	}

	if !strings.HasPrefix(namespace, netPerfName+"-") {
		return fmt.Errorf("%s is not a network test namespace", namespace)
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		var obj struct {
			Metadata checkMeta `json:"metadata"`
		}

		if err := k.get(ctx, netPerfNamespacePath(namespace), &obj); err != nil {
			if isNotFound(err) {
				return nil
			}

			return err
		}

		if obj.Metadata.Labels["app.kubernetes.io/managed-by"] != netPerfManagedBy {
			return fmt.Errorf("%s was not created by Ichor: not deleted", namespace)
		}

		return deleteNetPerfNamespace(ctx, k, namespace)
	})
}

// retryableCleanup tells a DELETE worth trying again (no answer, a server error, throttled)
// from one the API server refused for good (permission denied).
func retryableCleanup(err error) bool {
	code := kubeCode(err)

	return code == 0 || code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// sweepNetPerfNamespaces deletes the test namespaces of runs the app could not finish.
func sweepNetPerfNamespaces(ctx context.Context, k *kubeClient, now time.Time) {
	sweepRunNamespaces(ctx, k, netPerfName, now)
}

// runNamespace is what sweepRunNamespaces needs of a namespace: when it was made and
// whether it is already going.
type runNamespace struct {
	Metadata struct {
		Name              string     `json:"name"`
		CreationTimestamp time.Time  `json:"creationTimestamp"`
		DeletionTimestamp *time.Time `json:"deletionTimestamp"`
	} `json:"metadata"`
}

// sweepRunNamespaces deletes the namespaces of app's runs the app could not finish.
func sweepRunNamespaces(ctx context.Context, k *kubeClient, app string, now time.Time) {
	var list kubeList[runNamespace]

	// Only namespaces this app created: a name prefix and an app label alone may match others.
	selector := url.QueryEscape("app.kubernetes.io/name=" + app + ",app.kubernetes.io/managed-by=" + netPerfManagedBy)
	if getList(ctx, k, "/api/v1/namespaces?labelSelector="+selector, &list) != nil {
		return
	}

	for _, ns := range list.Items {
		m := ns.Metadata
		if m.DeletionTimestamp == nil && strings.HasPrefix(m.Name, app+"-") && now.Sub(m.CreationTimestamp) > netPerfStaleAge {
			_ = deleteNetPerfNamespace(ctx, k, m.Name) //nolint:errcheck // the next run sweeps again
		}
	}
}

// netPerfPodSpec is a pod pinned to node running command once. It meets the "restricted"
// pod security level (netperf needs no privilege); hostNetwork alone needs "privileged".
// It tolerates every taint, so control plane nodes can be tested too.
func netPerfPodSpec(name, node string, hostNetwork bool, deadline time.Duration, command ...string) map[string]any {
	return runPodSpec(runPodImage{app: netPerfName, image: netPerfImage, container: netPerfContainer},
		name, node, hostNetwork, deadline, command...)
}

// runPodImage is what a run's pods are: their app label, image and container name, and
// what a run needs beyond the restricted defaults (the image scan's credentials and space).
type runPodImage struct {
	app, image, container string

	env       []map[string]string
	mounts    []map[string]any // besides /tmp
	volumes   []map[string]any
	resources map[string]any
}

// runPodSpec is netPerfPodSpec for any run's pods.
func runPodSpec(img runPodImage, name, node string, hostNetwork bool, deadline time.Duration, command ...string) map[string]any {
	container := map[string]any{
		"name":            img.container,
		"image":           img.image,
		"imagePullPolicy": "IfNotPresent",
		"command":         command,
		"securityContext": map[string]any{
			"allowPrivilegeEscalation": false,
			"readOnlyRootFilesystem":   true,
			"capabilities":             map[string]any{"drop": []string{"ALL"}},
		},
		// netserver's child for each test opens a debug file in /tmp, and exits
		// (the client sees a connection reset) when it cannot.
		"volumeMounts": append([]map[string]any{{"name": "tmp", "mountPath": "/tmp"}}, img.mounts...),
	}

	if len(img.env) > 0 {
		container["env"] = img.env
	}

	if img.resources != nil {
		container["resources"] = img.resources
	}

	spec := map[string]any{
		"hostNetwork":                   hostNetwork,
		"restartPolicy":                 "Never",
		"activeDeadlineSeconds":         int(deadline.Seconds()),
		"terminationGracePeriodSeconds": 1,
		"automountServiceAccountToken":  false,
		"enableServiceLinks":            false,
		"securityContext": map[string]any{
			"runAsNonRoot":   true,
			"runAsUser":      65534,
			"runAsGroup":     65534,
			"fsGroup":        65534,
			"seccompProfile": map[string]string{"type": "RuntimeDefault"},
		},
		"containers": []map[string]any{container},
		"volumes":    append([]map[string]any{{"name": "tmp", "emptyDir": map[string]string{"sizeLimit": "16Mi"}}}, img.volumes...),
	}

	// A pod pinned to a node tolerates its taints (a control plane, a dedicated node); one
	// left to the scheduler does not, or it lands on cordoned and unreachable nodes too.
	if node != "" {
		spec["nodeName"] = node
		spec["tolerations"] = []map[string]string{{"operator": "Exists"}}
	}

	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":   name,
			"labels": map[string]string{"app.kubernetes.io/name": img.app, "app.kubernetes.io/managed-by": "ichor"},
		},
		"spec": spec,
	}
}

type netPerfPod struct {
	Status struct {
		Phase             string `json:"phase"`
		Reason            string `json:"reason"`
		Message           string `json:"message"`
		PodIP             string `json:"podIP"`
		ContainerStatuses []struct {
			State struct {
				Waiting *struct {
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"waiting"`
				Terminated *struct {
					ExitCode int    `json:"exitCode"`
					Reason   string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

// waiting is why the container is not started yet ("" when it is, or not known yet).
func (p netPerfPod) waiting() (reason, message string) {
	for _, c := range p.Status.ContainerStatuses {
		if w := c.State.Waiting; w != nil {
			return w.Reason, w.Message
		}
	}

	return "", ""
}

// netPerfPullFailures are waiting reasons the pod does not recover from by itself
// (the kubelet retries ErrImagePull; ImagePullBackOff follows when it keeps failing).
var netPerfPullFailures = []string{"ImagePullBackOff", "InvalidImageName", "CreateContainerConfigError", "CreateContainerError"}

// waitNetPerfPod reads the pod until done says so, it cannot start, or timeout. onWait gets
// the container's waiting reason when it changes (ContainerCreating while the image is pulled).
func waitNetPerfPod(ctx context.Context, k *kubeClient, ns, name, node string, timeout time.Duration,
	onWait func(string), done func(netPerfPod) bool,
) (netPerfPod, error) {
	return waitRunPod(ctx, k, "netperf", ns, name, node, timeout, onWait, done)
}

// waitRunPod is waitNetPerfPod for any run's pods; what names them in the errors.
func waitRunPod(ctx context.Context, k *kubeClient, what, ns, name, node string, timeout time.Duration,
	onWait func(string), done func(netPerfPod) bool,
) (netPerfPod, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	path := netPerfNamespacePath(ns) + "/pods/" + url.PathEscape(name)
	last := ""

	for {
		var pod netPerfPod
		if err := k.get(ctx, path, &pod); err != nil {
			return pod, err
		}

		if done(pod) {
			return pod, nil
		}

		reason, message := pod.waiting()
		switch {
		case slices.Contains(netPerfPullFailures, reason):
			return pod, netPerfRefused("the %s pod cannot start on %s: %s %s", what, node, reason, message)
		case pod.Status.Phase == "Failed":
			return pod, netPerfRefused("the %s pod failed on %s: %s %s", what, node, pod.Status.Reason, pod.Status.Message)
		case reason != last && onWait != nil:
			last = reason
			onWait(reason)
		}

		select {
		case <-ctx.Done():
			return pod, netPerfRefused("the %s pod did not start on %s in time (%s)", what, node, strings.TrimSpace(reason+" "+message))
		case <-time.After(netPerfPoll):
		}
	}
}

type netPerfServer struct {
	address string
	err     string // why this path cannot be measured
}

// startNetPerfServers starts netserver on the server node for each path, and pulls the
// image on the client node meanwhile so the measurements do not wait for it. A path whose
// server cannot start is reported in its entry; an error ends the test.
func startNetPerfServers(ctx context.Context, k *kubeClient, ns string, opts netPerfOptions, onWait func(string)) (map[string]netPerfServer, error) {
	paths := []string{netPerfPathPod}
	if opts.hostNetwork {
		paths = append(paths, netPerfPathHost)
	}

	podsPath := netPerfNamespacePath(ns) + "/pods"
	lifetime := netPerfTimeout
	servers := map[string]netPerfServer{}

	for _, p := range paths {
		spec := netPerfPodSpec("server-"+p, opts.server, p == netPerfPathHost, lifetime,
			"netserver", "-D", "-p", strconv.Itoa(netPerfControlPort))
		if err := k.post(ctx, podsPath, spec, nil); err != nil {
			servers[p] = netPerfServer{err: "netserver could not be created: " + kubeError(err).Error()}
		}
	}

	if opts.client != opts.server {
		spec := netPerfPodSpec("pull", opts.client, false, netPerfStartTimeout, "netperf", "-V")
		if err := k.post(ctx, podsPath, spec, nil); err != nil {
			return servers, kubeError(err)
		}
	}

	for _, p := range paths {
		if servers[p].err != "" {
			continue
		}

		pod, err := waitNetPerfPod(ctx, k, ns, "server-"+p, opts.server, netPerfStartTimeout, onWaitOn(opts.server, onWait),
			func(pod netPerfPod) bool { return pod.Status.Phase == "Running" && pod.Status.PodIP != "" })
		if ctx.Err() != nil {
			return servers, ctx.Err()
		}

		servers[p] = netPerfServer{address: pod.Status.PodIP}
		if err != nil {
			servers[p] = netPerfServer{err: err.Error()}
		}
	}

	if opts.client != opts.server {
		_, err := waitNetPerfPod(ctx, k, ns, "pull", opts.client, netPerfStartTimeout, onWaitOn(opts.client, onWait),
			func(pod netPerfPod) bool { return pod.Status.Phase == "Succeeded" })
		if err != nil {
			return servers, err
		}
	}

	return servers, nil
}

// onWaitOn reports a pod waiting on node as "node: reason".
func onWaitOn(node string, onWait func(string)) func(string) {
	return func(reason string) {
		if reason != "" {
			onWait(node + ": " + reason)
		}
	}
}

// runNetPerfCase runs one measurement from the client node against the server at address.
func runNetPerfCase(ctx context.Context, k *kubeClient, ns string, opts netPerfOptions, c netPerfCase, address string, step int) netPerfResult {
	name := fmt.Sprintf("client-%d-%s-%s", step, c.path, c.test)
	seconds := time.Duration(opts.seconds) * time.Second
	spec := netPerfPodSpec(name, opts.client, c.path == netPerfPathHost, seconds+time.Minute, netPerfCommand(address, c.test, opts.seconds)...)
	result := netPerfResult{Path: c.path, Test: c.test}

	if err := k.post(ctx, netPerfNamespacePath(ns)+"/pods", spec, nil); err != nil {
		result.Error = kubeError(err).Error()

		return result
	}

	pod, err := waitNetPerfPod(ctx, k, ns, name, opts.client, seconds+netPerfStartTimeout, nil, func(pod netPerfPod) bool {
		return pod.Status.Phase == "Succeeded" || pod.Status.Phase == "Failed"
	})
	if err != nil {
		result.Error = err.Error()

		return result
	}

	log, err := k.getText(ctx, netPerfNamespacePath(ns)+"/pods/"+url.PathEscape(name)+"/log?container="+netPerfContainer)
	if err != nil {
		result.Error = "read the result: " + kubeError(err).Error()

		return result
	}

	if pod.Status.Phase == "Failed" {
		result.Error = netPerfFailure(log, pod, address)

		return result
	}

	parsed, err := parseNetPerf(c.test, log)
	if err != nil {
		result.Error = err.Error()

		return result
	}

	parsed.Path = c.path

	return parsed
}

// netPerfCommand measures throughput (TCP_STREAM) or round trips (TCP_RR) to address, with
// the data connection on netPerfDataPort, as `cilium connectivity perf` runs netperf.
func netPerfCommand(address, test string, seconds int) []string {
	kind := "TCP_STREAM"
	if test == netPerfLatencyTest {
		kind = "TCP_RR"
	}

	cmd := []string{"netperf"}
	if ip := net.ParseIP(address); ip != nil && ip.To4() == nil {
		cmd = append(cmd, "-6")
	}

	return append(cmd, "-H", address, "-p", strconv.Itoa(netPerfControlPort), "-l", strconv.Itoa(seconds), "-t", kind,
		"--", "-R", "1", "-P", ","+strconv.Itoa(netPerfDataPort), "-o", netPerfFields)
}
