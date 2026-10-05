package ichorgo

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
)

// What a diagnosis report holds. Everything is read with os:reader calls, and nothing in
// it is a credential: no talosconfig, no machine configuration, no kubeconfig.

const (
	diagnosisCollectTimeout = time.Minute
	diagnosisNodeTimeout    = 25 * time.Second
	// The events stream replays the last events, then stays open: stop once it goes quiet.
	diagnosisEventsWindow = 5 * time.Second
	diagnosisEventsQuiet  = 1500 * time.Millisecond

	diagnosisEventTail   = 20 // replayed per node
	diagnosisMaxEvents   = 30
	diagnosisLogLines    = 40
	diagnosisMaxLogs     = 3 // services per node
	diagnosisMaxLineLen  = 300
	diagnosisMaxIdlePods = 15

	kubeletService = "kubelet"
)

type diagnosisData struct {
	At       time.Time
	Roles    []string // of the talosconfig the app uses
	Nodes    []nodeDiagnosis
	Etcd     *etcdOverview
	EtcdNote string // why there is no etcd section
	Events   []nodeEvent
	// EventsNote says why Events may not be the whole story (the stream failed or was slow).
	EventsNote string
	// GitOps: Argo CD and Flux, nil without os:admin or when neither runs.
	GitOps *gitopsState

	// What a mask has to learn to hide the cluster's names in the report.
	hosts   []hostEntry
	domains []string
}

type nodeDiagnosis struct {
	nodeOverview

	Services    []serviceInfo
	ServicesErr string
	Resources   *nodeResources
	Clock       *nodeTime
	Kube        *kubeNodeState
	StaticPods  []staticPodState
	Running     int      // Kubernetes containers running
	IdlePods    []string // pods without a running container
	Logs        []serviceLogTail
}

// kubeNodeState is the node as Kubernetes sees it, from Talos' own NodeStatus resource.
type kubeNodeState struct {
	Name          string
	Ready         bool
	Unschedulable bool
}

type staticPodState struct {
	Name     string
	Phase    string
	Ready    bool
	Restarts int
	Waiting  string // e.g. "CrashLoopBackOff: back-off 5m0s restarting failed container"
}

type serviceLogTail struct {
	Service string
	Lines   []string
	Err     string
}

// collectDiagnosis gathers the report data. A node or a section that fails is reported in
// its place; only an unusable context is an error.
func collectDiagnosis(ctx context.Context, s *session, kube kubeTarget) diagnosisData {
	nodes := targetNodes(s.context)
	data := diagnosisData{At: time.Now(), Nodes: make([]nodeDiagnosis, len(nodes))}

	if summary, err := summarizeContext("", s.context); err == nil {
		data.Roles = summary.Roles
	}

	overviews := make([]nodeOverview, len(nodes))
	domains := make([]string, len(nodes))

	forEachNode(nodes, func(i int, node string) {
		p := probeNode(ctx, s.client, node)
		overviews[i], domains[i] = buildNodeOverview(node, p), p.domain
	})

	data.hosts, data.domains = clusterHostEntries(ctx, s.client, overviews), domains

	var reachable, controlPlanes []string

	for _, o := range overviews {
		if o.Reachable {
			reachable = append(reachable, o.Node)

			if o.Role == "controlplane" {
				controlPlanes = append(controlPlanes, o.Node)
			}
		}
	}

	var wg sync.WaitGroup

	wg.Go(func() {
		forEachNode(overviews, func(i int, o nodeOverview) { data.Nodes[i] = diagnoseNode(ctx, s.client, o) })
	})

	wg.Go(func() {
		if len(controlPlanes) == 0 {
			data.EtcdNote = "no reachable control-plane node"

			return
		}

		etcd := fetchEtcd(ctx, s.client, controlPlanes)
		data.Etcd = &etcd
	})

	wg.Go(func() { data.Events, data.EventsNote = recentProblemEvents(ctx, s.client, reachable) })
	wg.Go(func() { data.GitOps = collectGitOps(ctx, kube, data.Roles) })

	wg.Wait()

	return data
}

// learnDiagnosisHosts teaches mask the cluster's host names and domains, including those
// that only show up in the details (Kubernetes node names, etcd members).
func learnDiagnosisHosts(mask *privacyMask, data diagnosisData) {
	mask.learnDomains(data.domains...)
	mask.learnHosts(data.hosts)

	for _, n := range data.Nodes {
		if n.Kube != nil {
			mask.learnHost(n.Kube.Name, n.Role)
		}
	}

	if data.Etcd != nil {
		for _, m := range data.Etcd.Members {
			mask.learnHost(m.Hostname, "controlplane")
		}
	}
}

func diagnoseNode(ctx context.Context, c *client.Client, o nodeOverview) nodeDiagnosis {
	n := nodeDiagnosis{nodeOverview: o}
	if !o.Reachable {
		return n
	}

	ctx, cancel := context.WithTimeout(ctx, diagnosisNodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, o.Node)

	var wg sync.WaitGroup

	wg.Go(func() {
		resp, err := c.ServiceList(nodeCtx)
		if err != nil {
			n.ServicesErr = friendlyError(err)

			return
		}

		if msg := first(resp.GetMessages()); msg != nil {
			n.Services = mapServices(msg.GetServices())
		}
	})

	wg.Go(func() {
		if r, err := fetchNodeResources(ctx, c, o.Node); err == nil {
			n.Resources = &r
		}
	})

	wg.Go(func() {
		t := probeTime(ctx, c, o.Node)
		n.Clock = &t
	})

	wg.Go(func() { n.Kube = fetchKubeNodeState(nodeCtx, c) })

	if o.Role == "controlplane" {
		wg.Go(func() { n.StaticPods = fetchStaticPods(nodeCtx, c) })
	}

	wg.Go(func() {
		list, err := c.Containers(nodeCtx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI)
		if err == nil {
			n.Running, n.IdlePods = summarizePods(mergeContainers(first(list.GetMessages()).GetContainers(), nil))
		}
	})

	wg.Wait()

	for _, service := range logTargets(n) {
		n.Logs = append(n.Logs, fetchLogTail(nodeCtx, c, service))
	}

	return n
}

// serviceTroubled tells whether a Talos service deserves a look: unhealthy, or neither
// running nor done.
func serviceTroubled(s serviceInfo) bool {
	if s.Health == "unhealthy" {
		return true
	}

	return !slices.Contains([]string{"Running", "Finished", "Skipped"}, s.State)
}

// logTargets picks the services whose log tail goes in the report: the troubled ones, or
// the kubelet when the node is not ready and no service says why.
func logTargets(n nodeDiagnosis) []string {
	var out []string

	for _, s := range n.Services {
		if serviceTroubled(s) && len(out) < diagnosisMaxLogs {
			out = append(out, s.ID)
		}
	}

	if len(out) == 0 && !n.Ready && slices.ContainsFunc(n.Services, func(s serviceInfo) bool { return s.ID == kubeletService }) {
		out = append(out, kubeletService)
	}

	return out
}

func fetchLogTail(nodeCtx context.Context, c *client.Client, service string) serviceLogTail {
	out := serviceLogTail{Service: service}

	stream, err := c.Logs(nodeCtx, constants.SystemContainerdNamespace, common.ContainerDriver_CONTAINERD, service, false, diagnosisLogLines)
	if err != nil {
		out.Err = friendlyError(err)

		return out
	}

	tail, err := drainStream(stream.Recv, diagnosisLogLines)
	if err != nil {
		out.Err = err.Error()

		return out
	}

	for _, line := range tail.Lines {
		out.Lines = append(out.Lines, clipText(line, diagnosisMaxLineLen))
	}

	return out
}

func fetchKubeNodeState(nodeCtx context.Context, c *client.Client) *kubeNodeState {
	list, err := safe.StateListAll[*k8s.NodeStatus](nodeCtx, c.COSI)
	if err != nil {
		return nil
	}

	for status := range list.All() {
		spec := status.TypedSpec()

		return &kubeNodeState{Name: spec.Nodename, Ready: spec.NodeReady, Unschedulable: spec.Unschedulable}
	}

	return nil
}

func fetchStaticPods(nodeCtx context.Context, c *client.Client) []staticPodState {
	list, err := safe.StateListAll[*k8s.StaticPodStatus](nodeCtx, c.COSI)
	if err != nil {
		return nil
	}

	var out []staticPodState

	for status := range list.All() {
		out = append(out, describeStaticPod(status.Metadata().ID(), status.TypedSpec().PodStatus))
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}

// describeStaticPod reads the fields that matter out of a raw Kubernetes pod status.
func describeStaticPod(id string, status map[string]any) staticPodState {
	out := staticPodState{Name: id}
	out.Phase, _ = status["phase"].(string) //nolint:errcheck

	for _, cond := range anyMaps(status["conditions"]) {
		if cond["type"] == "Ready" {
			out.Ready = cond["status"] == "True"
		}
	}

	for _, container := range anyMaps(status["containerStatuses"]) {
		if restarts, ok := container["restartCount"].(float64); ok {
			out.Restarts += int(restarts)
		}

		state, _ := container["state"].(map[string]any) //nolint:errcheck
		waiting, _ := state["waiting"].(map[string]any) //nolint:errcheck
		reason, _ := waiting["reason"].(string)         //nolint:errcheck
		message, _ := waiting["message"].(string)       //nolint:errcheck
		if reason != "" && out.Waiting == "" {
			out.Waiting = strings.TrimSuffix(reason+": "+message, ": ")
		}
	}

	return out
}

func anyMaps(v any) []map[string]any {
	items, _ := v.([]any) //nolint:errcheck
	out := make([]map[string]any, 0, len(items))

	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}

	return out
}

// summarizePods counts the running containers and lists the pods that have none: crash
// loops and pods stuck starting look like that, and so do finished jobs.
func summarizePods(containers []containerInfo) (int, []string) {
	type pod struct {
		running bool
		states  []string
	}

	pods := map[string]*pod{}

	var (
		order   []string
		running int
	)

	for _, c := range containers {
		key := strings.TrimPrefix(c.PodNamespace+"/"+c.Pod, "/")

		p, ok := pods[key]
		if !ok {
			p = &pod{}
			pods[key] = p
			order = append(order, key)
		}

		if c.Status == "CONTAINER_RUNNING" {
			running++
			p.running = true
		}

		p.states = append(p.states, c.Name+" "+strings.ToLower(strings.TrimPrefix(c.Status, "CONTAINER_")))
	}

	var idle []string

	for _, key := range order {
		if p := pods[key]; !p.running {
			idle = append(idle, fmt.Sprintf("%s (%s)", key, strings.Join(p.states, ", ")))
		}
	}

	if len(idle) > diagnosisMaxIdlePods {
		idle = append(idle[:diagnosisMaxIdlePods], fmt.Sprintf("and %d more", len(idle)-diagnosisMaxIdlePods))
	}

	return running, idle
}

// recentProblemEvents returns the latest warning and error events of nodes, oldest first,
// and a note when they could not be read properly: "none" must mean none.
func recentProblemEvents(ctx context.Context, c *client.Client, nodes []string) ([]nodeEvent, string) {
	if len(nodes) == 0 {
		return nil, "no reachable node"
	}

	ctx, cancel := context.WithTimeout(ctx, diagnosisEventsWindow)
	defer cancel()

	ch := make(chan eventItem)
	done := make(chan error, 1)

	go func() {
		done <- safeCall(func() error { return watchEvents(client.WithNodes(ctx, nodes...), c, diagnosisEventTail, ch) })
	}()

	var (
		events   []nodeEvent
		received int
		note     string
	)

	// The replay starts within the window; once it has, a quiet moment means it is over.
	quiet := time.NewTimer(diagnosisEventsWindow)
	defer quiet.Stop()

loop:
	for {
		select {
		case item := <-ch:
			received++

			if item.event.Severity != "info" {
				events = append(events, item.event)
			}

			quiet.Reset(diagnosisEventsQuiet)
		case err := <-done:
			if err != nil && ctx.Err() == nil {
				note = "could not read the events: " + friendlyError(err)
			}

			break loop
		case <-quiet.C:
			break loop
		case <-ctx.Done():
			break loop
		}
	}

	// Stops the watcher, which may be blocked sending the next event.
	cancel()

	if received == 0 && note == "" {
		note = "no event received in " + diagnosisEventsWindow.String() + ", the nodes may be slow to answer"
	}

	return latestEvents(events, diagnosisMaxEvents), note
}

func latestEvents(events []nodeEvent, limit int) []nodeEvent {
	sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })

	if len(events) > limit {
		events = events[len(events)-limit:]
	}

	return events
}
