package ichorgo

import (
	"context"
	"strconv"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// talosContainersHealth is the background monitor's view of the Talos containers of a
// cluster. Issues are keyed "<node>|<name>" and valued
// "<severity>|<state>|<restartCount>|<exitCode>|<hostname>|<name>" (hostname and name without
// the separator). Never the error text nor the last log: they are workload output, and the
// value ends up in a notification.
type talosContainersHealth struct {
	Context string `json:"context"`
	// Checked: at least one node answered.
	Checked bool `json:"checked"`
	// Unread are the nodes that could not be read: the apps keep the issues they knew of them.
	Unread []string          `json:"unread"`
	Issues map[string]string `json:"issues"`
}

// talosContainersEntry is what one node said: its containers, or why it could not be read.
type talosContainersEntry struct {
	Node       string
	Hostname   string
	Containers []containerInfo
	Error      string
}

// TalosContainersHealth reads, for every node of the context in parallel, its Talos containers
// (Talos 1.15) and returns the ones Go judges in trouble (see talosContainerIssue): what the
// Talos containers alert of the background monitor needs (os:reader). A node older than 1.15
// answers none; a node that does not answer is named in unread and never fails the call.
func TalosContainersHealth(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return demoTalosContainersHealth(configYAML, contextName)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		entries := make([]talosContainersEntry, len(nodes))

		forEachNode(nodes, func(i int, node string) { entries[i] = readTalosContainersEntry(ctx, s, node) })

		return toJSON(talosContainersHealthOf(name, entries))
	})
}

func readTalosContainersEntry(ctx context.Context, s *session, node string) talosContainersEntry {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)
	out := talosContainersEntry{Node: node, Hostname: node}

	list, err := listTalosContainers(nodeCtx, s, node)
	if err != nil {
		out.Error = s.friendly(node, err)

		return out
	}

	out.Containers = list

	if len(list) > 0 {
		if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil && hs.TypedSpec().Hostname != "" {
			out.Hostname = hs.TypedSpec().Hostname
			privacy.learnHost(out.Hostname, "node")
		}
	}

	return out
}

// talosContainersHealthOf keeps the containers in trouble of the nodes that answered.
func talosContainersHealthOf(contextName string, entries []talosContainersEntry) talosContainersHealth {
	out := talosContainersHealth{Context: contextName, Unread: []string{}, Issues: map[string]string{}}

	for _, e := range entries {
		if e.Error != "" {
			out.Unread = append(out.Unread, e.Node)

			continue
		}

		out.Checked = true

		for _, c := range e.Containers {
			severity, ok := talosContainerIssue(c)
			if !ok {
				continue
			}

			out.Issues[e.Node+"|"+c.Name] = strings.Join([]string{
				severity, c.Status, strconv.FormatUint(c.RestartCount, 10), strconv.FormatInt(int64(c.ExitCode), 10),
				issueField(e.Hostname), issueField(c.Name),
			}, "|")
		}
	}

	return out
}

// talosContainerIssue judges one Talos container: critical once Talos gave up on it (failed:
// its restart policy does not start it again), a warning while it keeps crashing (degraded
// after talosContainerRestartsAlert restarts or more; the monitor alerts a warning on its
// second check in a row).
func talosContainerIssue(c containerInfo) (string, bool) {
	switch {
	case c.Status == "failed":
		return fleetCritical, true
	case c.Health == "degraded" && c.RestartCount >= talosContainerRestartsAlert:
		return fleetWarning, true
	}

	return "", false
}

// issueField is s for an issue value, without the separator.
func issueField(s string) string { return strings.ReplaceAll(s, "|", "/") }

// demoTalosContainersHealth is TalosContainersHealth for the demo cluster.
func demoTalosContainersHealth(configYAML, contextName string) (string, error) {
	name, _, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	nodes := demoNodes()
	entries := make([]talosContainersEntry, 0, len(nodes))

	for _, n := range nodes {
		entries = append(entries, talosContainersEntry{Node: n.Node, Hostname: n.Hostname, Containers: demoTalosContainers(n)})
	}

	return toJSON(talosContainersHealthOf(name, entries))
}
