package ichorgo

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// The diagnosis of a cluster added from a kubeconfig: no Talos API, so the report is what
// the Kubernetes API says: the nodes as it sees them, the pods that are not healthy, the
// Warning events of the last hour and the GitOps apps in trouble. Nothing about the node
// operating system, etcd or system services; the prompt tells the model so.

const kubeDiagnosisMaxPods = 30

type kubeDiagnosis struct {
	ServerVersion string
	Nodes         []kubeNodeInfo
	NodesNote     string // why there is no node list
	// Pods are the first kubeDiagnosisMaxPods of the PodsTotal pods that are not healthy.
	Pods      []kubePod
	PodsTotal int
	PodsNote  string
	// Events are the Warning events of the last checkupEventWindow, newest first.
	Events     []kubeEvent
	EventsNote string
}

// collectKubeDiagnosis reads the report of a cluster added from a kubeconfig. Only an API
// server that answers nothing is an error; a list the credentials may not read is noted.
func collectKubeDiagnosis(kube kubeTarget) (diagnosisData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diagnosisCollectTimeout)
	defer cancel()

	data := diagnosisData{At: time.Now()}

	kd, err := withKubeContext(ctx, kube, readKubeDiagnosis)
	if err != nil {
		return data, err
	}

	data.Kube = &kd
	data.GitOps = gitopsStateOf(ctx, kube)

	return data, nil
}

func readKubeDiagnosis(ctx context.Context, k *kubeClient) (kubeDiagnosis, error) {
	var (
		kd                           kubeDiagnosis
		nodes                        []kubeNodeObject
		pods                         []kubePod
		events                       kubeEventList
		nodesErr, podsErr, eventsErr error
		wg                           sync.WaitGroup
	)

	wg.Go(func() { kd.ServerVersion = readAPIVersion(ctx, k) })
	wg.Go(func() { nodes, nodesErr = listKubeNodeObjects(ctx, k) })
	wg.Go(func() { pods, podsErr = readUnhealthyPods(ctx, k, "") })
	wg.Go(func() { events, eventsErr = readClusterEvents(ctx, k, true, kubeEventsMax) })
	wg.Wait()

	switch {
	case isForbidden(nodesErr):
		kd.NodesNote = "the credentials may not list nodes"
	case nodesErr != nil:
		return kd, nodesErr
	default:
		kd.Nodes = make([]kubeNodeInfo, 0, len(nodes))
		for _, obj := range nodes {
			kd.Nodes = append(kd.Nodes, mapKubeNode(obj))
		}

		sortKubeNodes(kd.Nodes)
	}

	switch {
	case isForbidden(podsErr):
		kd.PodsNote = "the credentials may not list pods cluster-wide"
	case podsErr != nil:
		kd.PodsNote = kubeError(podsErr).Error()
	default:
		kd.PodsTotal = len(pods)
		kd.Pods = pods[:min(len(pods), kubeDiagnosisMaxPods)]
	}

	switch {
	case eventsErr != nil:
		kd.EventsNote = kubeError(eventsErr).Error()
	case events.Forbidden:
		kd.EventsNote = "the credentials may not list events cluster-wide"
	default:
		since := time.Now().Add(-checkupEventWindow).UnixMilli()
		recent := eventsSince(events.Events, since)
		kd.Events = recent[:min(len(recent), diagnosisMaxEvents)]
	}

	return kd, nil
}

// kubeDiagnosisHosts are the nodes as the mask learns hosts: a placeholder per node name and
// address, cp-N for the control planes.
func kubeDiagnosisHosts(nodes []kubeNodeInfo) []hostEntry {
	entries := make([]hostEntry, 0, len(nodes))

	for _, n := range nodes {
		role := "worker"
		if slices.Contains(n.Roles, "control-plane") {
			role = "controlplane"
		}

		entries = append(entries, hostEntry{address: n.InternalIP, hostname: n.Name, role: role})
	}

	return entries
}

func renderKubeDiagnosis(d diagnosisData) string {
	var b strings.Builder

	kd := d.Kube

	fmt.Fprintf(&b, "Kubernetes cluster report, collected %s through the Kubernetes API", d.At.UTC().Format("2006-01-02 15:04 MST"))
	b.WriteString("\n(the cluster was added from a kubeconfig: no Talos API, so nothing about the node operating system, etcd or system services)")

	if kd.ServerVersion != "" {
		fmt.Fprintf(&b, "\nAPI server %s", kd.ServerVersion)
	}

	b.WriteString("\n\n")
	renderKubeNodes(&b, kd)
	renderUnhealthyPods(&b, kd)
	renderGitOps(&b, d.GitOps)
	renderKubeEvents(&b, kd)

	return b.String()
}

func renderKubeNodes(b *strings.Builder, kd *kubeDiagnosis) {
	if kd.NodesNote != "" {
		fmt.Fprintf(b, "NODES\n  not read: %s\n\n", kd.NodesNote)

		return
	}

	var ready, notReady, cordoned int

	for _, n := range kd.Nodes {
		if n.Ready {
			ready++
		} else {
			notReady++
		}

		if n.Cordoned {
			cordoned++
		}
	}

	fmt.Fprintf(b, "NODES (%d: %d ready, %d not ready, %d cordoned)\n", len(kd.Nodes), ready, notReady, cordoned)

	for _, n := range kd.Nodes {
		state := "Ready"
		if !n.Ready {
			state = "NOT READY"
		}

		roles := strings.Join(n.Roles, ", ")
		if roles == "" {
			roles = "worker"
		}

		fmt.Fprintf(b, "- %s", n.Name)

		if n.InternalIP != "" {
			fmt.Fprintf(b, " [%s]", n.InternalIP)
		}

		fmt.Fprintf(b, ": %s, %s", roles, state)

		if n.Cordoned {
			b.WriteString(", cordoned (unschedulable)")
		}

		if len(n.Pressure) > 0 {
			fmt.Fprintf(b, ", pressure: %s", strings.Join(n.Pressure, ", "))
		}

		b.WriteString("\n")

		var details []string

		for _, part := range []struct{ label, value string }{{"kubelet", n.Kubelet}, {"os", n.OSImage}, {"kernel", n.Kernel}, {"runtime", n.Runtime}, {"arch", n.Arch}} {
			if part.value != "" {
				details = append(details, part.label+" "+part.value)
			}
		}

		if n.CPU > 0 || n.Memory > 0 {
			details = append(details, fmt.Sprintf("%g cpu, %s memory, %d pods max", n.CPU, formatSize(uint64(n.Memory)), n.PodLimit))
		}

		if len(details) > 0 {
			fmt.Fprintf(b, "    %s\n", strings.Join(details, "; "))
		}
	}

	b.WriteString("\n")
}

func renderUnhealthyPods(b *strings.Builder, kd *kubeDiagnosis) {
	switch {
	case kd.PodsNote != "":
		fmt.Fprintf(b, "PODS NOT HEALTHY\n  not read: %s\n\n", kd.PodsNote)

		return
	case kd.PodsTotal == 0:
		b.WriteString("PODS NOT HEALTHY (0)\n  none\n\n")

		return
	}

	fmt.Fprintf(b, "PODS NOT HEALTHY (%d", kd.PodsTotal)

	if kd.PodsTotal > len(kd.Pods) {
		fmt.Fprintf(b, ", the first %d shown", len(kd.Pods))
	}

	b.WriteString(")\n")

	for _, p := range kd.Pods {
		fmt.Fprintf(b, "  %s/%s: %s, ready %d/%d, %d restarts", p.Namespace, p.Name, p.Status, p.Ready, p.Containers, p.Restarts)

		if p.Node != "" {
			fmt.Fprintf(b, ", on %s", p.Node)
		}

		if p.Owner != "" {
			fmt.Fprintf(b, ", owned by %s", p.Owner)
		}

		if p.LastTermination != "" {
			fmt.Fprintf(b, ", last stop: %s", clipUTF8(p.LastTermination, diagnosisMaxLineLen))
		}

		b.WriteString("\n")
	}

	b.WriteString("\n")
}

func renderKubeEvents(b *strings.Builder, kd *kubeDiagnosis) {
	if kd.EventsNote != "" {
		fmt.Fprintf(b, "WARNING EVENTS, LAST HOUR\n  not read: %s\n", kd.EventsNote)

		return
	}

	fmt.Fprintf(b, "WARNING EVENTS, LAST HOUR (%d, newest first)\n", len(kd.Events))

	if len(kd.Events) == 0 {
		b.WriteString("  none\n")

		return
	}

	for _, e := range kd.Events {
		object := e.Kind + " " + e.Name
		if e.Namespace != "" {
			object = e.Kind + " " + e.Namespace + "/" + e.Name
		}

		line := fmt.Sprintf("  %s %s %s (x%d): %s", time.UnixMilli(e.Last).UTC().Format("15:04:05"), object, e.Reason, e.Count, e.Message)
		b.WriteString(clipUTF8(line, diagnosisMaxLineLen) + "\n")
	}
}
