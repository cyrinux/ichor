package ichorgo

import (
	"context"
	"errors"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// Every exported function goes through these hooks exactly once (privacy_api_test.go checks
// it): arguments are unmasked on entry, results and errors are masked on exit. Unmasking
// twice could map a real value that looks like a fake (a cluster in 10.0.0.0/24), so the
// internals never unmask again.

// unmaskContext learns configYAML (contexts, addresses) and returns the real context name.
func unmaskContext(configYAML, contextName string) string {
	privacy.learnConfig(configYAML)

	return privacy.unmaskContext(contextName)
}

// unmaskTarget is unmaskContext plus the real node (address or hostname).
func unmaskTarget(configYAML, contextName, node string) (string, string) {
	return unmaskContext(configYAML, contextName), privacy.unmaskNode(node)
}

// unmaskTargets is unmaskTarget for a comma-separated node list.
func unmaskTargets(configYAML, contextName, nodes string) (string, string) {
	return unmaskContext(configYAML, contextName), privacy.unmaskNodes(nodes)
}

// maskResult masks a returned display value and error (use with defer and named results).
func maskResult(out *string, err *error) {
	*out = privacy.mask(*out)
	maskErr(err)
}

// maskErr masks a returned error message (use with defer and a named result).
func maskErr(err *error) {
	if *err == nil || !privacy.isEnabled() {
		return
	}

	*err = errors.New(privacy.maskPlain((*err).Error()))
}

type maskedNetPerfListener struct{ NetPerfListener }

func (l maskedNetPerfListener) OnProgress(json string) {
	l.NetPerfListener.OnProgress(privacy.mask(json))
}

func (l maskedNetPerfListener) OnDone(reportJSON string, errMessage string) {
	l.NetPerfListener.OnDone(privacy.mask(reportJSON), privacy.maskPlain(errMessage))
}

type maskedEventListener struct{ EventListener }

func (l maskedEventListener) OnEvent(json string) { l.EventListener.OnEvent(privacy.mask(json)) }
func (l maskedEventListener) OnDone(errMessage string) {
	l.EventListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedLogListener struct{ LogListener }

func (l maskedLogListener) OnLine(line string) { l.LogListener.OnLine(privacy.maskPlain(line)) }
func (l maskedLogListener) OnDone(errMessage string) {
	l.LogListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedHealthListener struct{ HealthListener }

func (l maskedHealthListener) OnProgress(node, message string) {
	l.HealthListener.OnProgress(privacy.maskPlain(node), privacy.maskPlain(message))
}

func (l maskedHealthListener) OnDone(errMessage string) {
	l.HealthListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedDiagnosisListener struct{ DiagnosisListener }

func (l maskedDiagnosisListener) OnAnswer(text string) {
	l.DiagnosisListener.OnAnswer(privacy.maskPlain(text))
}

func (l maskedDiagnosisListener) OnDone(errMessage string) {
	l.DiagnosisListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedSnapshotListener masks only the error: the path is the app's own file and the
// snapshot bytes are written unmasked (it is a backup).
type maskedSnapshotListener struct{ SnapshotListener }

func (l maskedSnapshotListener) OnDone(path string, size int64, sha256 string, errMessage string) {
	l.SnapshotListener.OnDone(path, size, sha256, privacy.maskPlain(errMessage))
}

// learnClusterHosts teaches the mask the cluster's hostnames (overview nodes plus the
// discovered members, which include workers missing from the talosconfig) and domains.
// Best effort, and only while the mask is on: it costs one extra call per overview.
func learnClusterHosts(ctx context.Context, c *client.Client, nodes []nodeOverview, domains []string) {
	if !privacy.isEnabled() {
		return
	}

	privacy.learnDomains(domains...)
	privacy.learnHosts(clusterHostEntries(ctx, c, nodes))
}

// clusterHostEntries lists the hosts of the cluster: the overview nodes plus the members
// discovered through the first reachable one.
func clusterHostEntries(ctx context.Context, c *client.Client, nodes []nodeOverview) []hostEntry {
	entries := make([]hostEntry, 0, len(nodes))
	via := ""

	for _, n := range nodes {
		if n.Hostname != n.Node {
			entries = append(entries, hostEntry{address: n.Node, hostname: n.Hostname, role: n.Role})
		}

		if via == "" && n.Reachable {
			via = n.Node
		}
	}

	if via != "" {
		entries = append(entries, clusterMembers(ctx, c, via)...)
	}

	return entries
}

func clusterMembers(ctx context.Context, c *client.Client, node string) []hostEntry {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	members, err := safe.StateListAll[*cluster.Member](client.WithNode(ctx, node), c.COSI)
	if err != nil {
		return nil
	}

	var out []hostEntry

	for m := range members.All() {
		spec := m.TypedSpec()
		e := hostEntry{hostname: spec.Hostname, role: roleName(spec.MachineType, nil)}

		if len(spec.Addresses) > 0 {
			e.address = spec.Addresses[0].String()
		}

		out = append(out, e)
	}

	return out
}

// learnSearchDomains teaches the mask the resolvers' DNS search domains.
func learnSearchDomains(resolvers []*network.ResolverStatus) {
	for _, r := range resolvers {
		privacy.learnDomains(r.TypedSpec().SearchDomains...)
	}
}

// learnDiscoveredHosts names the discovered members like the overview's, so the masked
// addresses the app gets back map to real ones when it adds them to the talosconfig.
func learnDiscoveredHosts(nodes []discoveredNode) {
	if !privacy.isEnabled() {
		return
	}

	entries := make([]hostEntry, 0, len(nodes))
	for _, n := range nodes {
		entries = append(entries, hostEntry{address: n.Address, hostname: n.Hostname, role: n.Role})
	}

	privacy.learnHosts(entries)
}
