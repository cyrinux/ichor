package ichorgo

import (
	"context"
	"errors"

	"github.com/siderolabs/talos/pkg/machinery/client"
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
// Like maskErr it turns a panic into an error: gomobile would abort the app instead.
func maskResult(out *string, err *error) {
	if r := recover(); r != nil {
		*out, *err = "", panicError(r)
	}

	*out = privacy.mask(*out)
	maskErr(err)
}

// maskErr masks a returned error message (use with defer and a named result), and turns a
// panic into an error (see panics.go).
func maskErr(err *error) {
	if r := recover(); r != nil {
		*err = panicError(r)
	}

	// Not a failure, and the apps match its text: a masked word must not change it.
	if *err == nil || errors.Is(*err, errCallsCancelled) {
		return
	}

	diagCallFailed(*err)

	if !privacy.isEnabled() {
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

// maskedOverviewListener masks each streamed node and the complete overview.
type maskedOverviewListener struct{ OverviewListener }

func (l maskedOverviewListener) OnNode(json string) {
	l.OverviewListener.OnNode(privacy.mask(json))
}

func (l maskedOverviewListener) OnDone(overviewJSON, errMessage string) {
	l.OverviewListener.OnDone(privacy.mask(overviewJSON), privacy.maskPlain(errMessage))
}

type maskedHubbleListener struct{ HubbleListener }

func (l maskedHubbleListener) OnUpdate(json string) { l.HubbleListener.OnUpdate(privacy.mask(json)) }
func (l maskedHubbleListener) OnDone(errMessage string) {
	l.HubbleListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedKubeWatchListener struct{ KubeWatchListener }

func (l maskedKubeWatchListener) OnEvent(eventType, json string) {
	l.KubeWatchListener.OnEvent(eventType, privacy.mask(json))
}

func (l maskedKubeWatchListener) OnDone(errMessage string) {
	l.KubeWatchListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedKubeLiveListener struct{ KubeLiveListener }

func (l maskedKubeLiveListener) OnUpdate(json string) {
	l.KubeLiveListener.OnUpdate(privacy.mask(json))
}
func (l maskedKubeLiveListener) OnDone(errMessage string) {
	l.KubeLiveListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedKubeEventsListener struct{ KubeEventsListener }

func (l maskedKubeEventsListener) OnEvents(batchJSON string) {
	l.KubeEventsListener.OnEvents(privacy.mask(batchJSON))
}
func (l maskedKubeEventsListener) OnStatus(stateJSON string) {
	l.KubeEventsListener.OnStatus(privacy.mask(stateJSON))
}
func (l maskedKubeEventsListener) OnDone(errMessage string) {
	l.KubeEventsListener.OnDone(privacy.maskPlain(errMessage))
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

func (l maskedDiagnosisListener) OnTool(name, argsJSON string) {
	l.DiagnosisListener.OnTool(name, privacy.maskPlain(argsJSON))
}

func (l maskedDiagnosisListener) OnToolDone(name string, bytes int, result, errMessage string) {
	l.DiagnosisListener.OnToolDone(name, bytes, privacy.maskPlain(result), privacy.maskPlain(errMessage))
}

func (l maskedDiagnosisListener) OnDone(errMessage string) {
	l.DiagnosisListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedPromChatListener struct{ PromChatListener }

func (l maskedPromChatListener) OnAnswer(text string) {
	l.PromChatListener.OnAnswer(privacy.maskPlain(text))
}

func (l maskedPromChatListener) OnDone(panelJSON, errMessage string) {
	l.PromChatListener.OnDone(privacy.mask(panelJSON), privacy.maskPlain(errMessage))
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
		// The diagnostics mask still learns the overview's own nodes (no extra call).
		diagMask.learnDomains(domains...)
		diagMask.learnHosts(overviewHostEntries(nodes))

		return
	}

	privacy.learnDomains(domains...)
	privacy.learnHosts(clusterHostEntries(ctx, c, nodes))
}

// learnNodeHost teaches the mask one node's hostname and domain before that node is shown
// on its own (a streamed overview node); learnClusterHosts still learns the whole cluster
// once the overview is complete.
func learnNodeHost(n nodeOverview, domain string) {
	if !privacy.isEnabled() {
		diagMask.learnDomains(domain)
		diagMask.learnHosts(overviewHostEntries([]nodeOverview{n}))

		return
	}

	privacy.learnDomains(domain)

	if n.Hostname != n.Node {
		privacy.learnHosts([]hostEntry{{address: n.Node, hostname: n.Hostname, role: n.Role}})
	}
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

// overviewHostEntries is clusterHostEntries without the member call.
func overviewHostEntries(nodes []nodeOverview) []hostEntry {
	entries := make([]hostEntry, 0, len(nodes))

	for _, n := range nodes {
		if n.Hostname != n.Node {
			entries = append(entries, hostEntry{address: n.Node, hostname: n.Hostname, role: n.Role})
		}
	}

	return entries
}

func clusterMembers(ctx context.Context, c *client.Client, node string) []hostEntry {
	members, err := nodeMembers(ctx, c, node)
	if err != nil {
		return nil
	}

	var out []hostEntry

	for _, m := range members {
		e := hostEntry{hostname: m.hostname, role: m.role}

		if len(m.addresses) > 0 {
			e.address = m.addresses[0].String()
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
	entries := make([]hostEntry, 0, len(nodes))
	for _, n := range nodes {
		entries = append(entries, hostEntry{address: n.Address, hostname: n.Hostname, role: n.Role})
	}

	// privacy.learnHosts passes them on to diagMask, which learns them either way.
	if !privacy.isEnabled() {
		diagMask.learnHosts(entries)

		return
	}

	privacy.learnHosts(entries)
}

// The listeners the app passes in, wrapped so every callback is masked like a result.

// maskedDebugListener masks the status and exit messages, which may name the real node.
// The terminal bytes pass through unmasked (see StartDebugShell).
type maskedDebugListener struct{ DebugListener }

func (l maskedDebugListener) OnStatus(message string) {
	l.DebugListener.OnStatus(privacy.maskPlain(message))
}

func (l maskedDebugListener) OnExit(code int, errMessage string) {
	l.DebugListener.OnExit(code, privacy.maskPlain(errMessage))
}

type maskedMaintenanceListener struct{ MaintenanceListener }

func (l maskedMaintenanceListener) OnProgress(json string) {
	l.MaintenanceListener.OnProgress(privacy.mask(json))
}

func (l maskedMaintenanceListener) OnDone(errMessage string) {
	l.MaintenanceListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedCaptureListener masks the live summaries and the error; the file is not masked.
type maskedCaptureListener struct{ CaptureListener }

func (l maskedCaptureListener) OnPacket(summaryJSON string) {
	l.CaptureListener.OnPacket(privacy.mask(summaryJSON))
}

func (l maskedCaptureListener) OnDone(path string, packets int64, bytes int64, errMessage string) {
	l.CaptureListener.OnDone(path, packets, bytes, privacy.maskPlain(errMessage))
}

// maskedSupportListener masks the progress and the error: the path is the app's own file and
// the bundle itself is written unmasked (it is for debugging).
type maskedSupportListener struct{ SupportListener }

func (l maskedSupportListener) OnProgress(json string) {
	l.SupportListener.OnProgress(privacy.mask(json))
}

func (l maskedSupportListener) OnDone(path string, size int64, errMessage string) {
	l.SupportListener.OnDone(path, size, privacy.maskPlain(errMessage))
}

type maskedConfigTryListener struct{ ConfigTryListener }

func (l maskedConfigTryListener) OnProgress(json string) {
	l.ConfigTryListener.OnProgress(privacy.mask(json))
}

func (l maskedConfigTryListener) OnDone(outcome string, errMessage string) {
	l.ConfigTryListener.OnDone(outcome, privacy.maskPlain(errMessage))
}

type maskedUpgradeListener struct{ UpgradeListener }

func (l maskedUpgradeListener) OnProgress(json string) {
	l.UpgradeListener.OnProgress(privacy.mask(json))
}

func (l maskedUpgradeListener) OnDone(newVersion string, errMessage string) {
	l.UpgradeListener.OnDone(newVersion, privacy.maskPlain(errMessage))
}

type maskedImageScanListener struct{ ImageScanListener }

func (l maskedImageScanListener) OnProgress(json string) {
	l.ImageScanListener.OnProgress(privacy.mask(json))
}

func (l maskedImageScanListener) OnDone(reportJSON string, errMessage string) {
	l.ImageScanListener.OnDone(privacy.mask(reportJSON), privacy.maskPlain(errMessage))
}

// maskedKubeSanitizeListener masks a pod sweep's progress, result and error.
type maskedKubeSanitizeListener struct{ KubeSanitizeListener }

func (l maskedKubeSanitizeListener) OnProgress(json string) {
	l.KubeSanitizeListener.OnProgress(privacy.mask(json))
}

func (l maskedKubeSanitizeListener) OnDone(resultJSON string, errMessage string) {
	l.KubeSanitizeListener.OnDone(privacy.mask(resultJSON), privacy.maskPlain(errMessage))
}

// maskedEtcdFixListener masks the NOSPACE fix's progress and error.
type maskedEtcdFixListener struct{ EtcdFixListener }

func (l maskedEtcdFixListener) OnProgress(json string) {
	l.EtcdFixListener.OnProgress(privacy.mask(json))
}

func (l maskedEtcdFixListener) OnDone(errMessage string) {
	l.EtcdFixListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedEtcdRecoverListener masks an etcd recovery's progress and error.
type maskedEtcdRecoverListener struct{ EtcdRecoverListener }

func (l maskedEtcdRecoverListener) OnProgress(json string) {
	l.EtcdRecoverListener.OnProgress(privacy.mask(json))
}

func (l maskedEtcdRecoverListener) OnDone(errMessage string) {
	l.EtcdRecoverListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedConfigApplyListener masks a config apply's progress and error.
type maskedConfigApplyListener struct{ ConfigApplyListener }

func (l maskedConfigApplyListener) OnProgress(json string) {
	l.ConfigApplyListener.OnProgress(privacy.mask(json))
}

func (l maskedConfigApplyListener) OnDone(errMessage string) {
	l.ConfigApplyListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedClusterUpgradeListener masks a cluster upgrade's progress and error.
type maskedClusterUpgradeListener struct{ ClusterUpgradeListener }

func (l maskedClusterUpgradeListener) OnProgress(json string) {
	l.ClusterUpgradeListener.OnProgress(privacy.mask(json))
}

func (l maskedClusterUpgradeListener) OnDone(errMessage string) {
	l.ClusterUpgradeListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedImagePullListener masks an image pull's progress and error.
type maskedImagePullListener struct{ ImagePullListener }

func (l maskedImagePullListener) OnProgress(json string) {
	l.ImagePullListener.OnProgress(privacy.mask(json))
}

func (l maskedImagePullListener) OnDone(errMessage string) {
	l.ImagePullListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedK8sUpgradeListener masks a Kubernetes upgrade's progress and error.
type maskedK8sUpgradeListener struct{ K8sUpgradeListener }

func (l maskedK8sUpgradeListener) OnProgress(json string) {
	l.K8sUpgradeListener.OnProgress(privacy.mask(json))
}

func (l maskedK8sUpgradeListener) OnDone(errMessage string) {
	l.K8sUpgradeListener.OnDone(privacy.maskPlain(errMessage))
}

// maskedNetToolListener masks a network tool's output lines, result and error.
type maskedNetToolListener struct{ NetToolListener }

func (l maskedNetToolListener) OnOutput(line string) {
	l.NetToolListener.OnOutput(privacy.maskPlain(line))
}

func (l maskedNetToolListener) OnDone(resultJSON string, errMessage string) {
	l.NetToolListener.OnDone(privacy.mask(resultJSON), privacy.maskPlain(errMessage))
}

type maskedJoinListener struct{ JoinListener }

func (l maskedJoinListener) OnProgress(json string) {
	l.JoinListener.OnProgress(privacy.mask(json))
}

func (l maskedJoinListener) OnDone(nodeAddress string, errMessage string) {
	// The address goes into the stored talosconfig: it stays real (joins refuse privacy mode).
	l.JoinListener.OnDone(nodeAddress, privacy.maskPlain(errMessage))
}

type maskedCARotationListener struct{ TalosCARotationListener }

func (l maskedCARotationListener) OnProgress(json string) {
	l.TalosCARotationListener.OnProgress(privacy.mask(json))
}

func (l maskedCARotationListener) OnTalosconfig(talosconfigYAML string, isFinal bool) {
	// A credential the app stores: never masked.
	l.TalosCARotationListener.OnTalosconfig(talosconfigYAML, isFinal)
}

func (l maskedCARotationListener) OnDone(errMessage string) {
	l.TalosCARotationListener.OnDone(privacy.maskPlain(errMessage))
}

type maskedArgoSyncListener struct{ ArgoSyncListener }

func (l maskedArgoSyncListener) OnProgress(json string) {
	l.ArgoSyncListener.OnProgress(privacy.mask(json))
}

func (l maskedArgoSyncListener) OnDone(result, errMessage string) {
	l.ArgoSyncListener.OnDone(result, privacy.maskPlain(errMessage))
}

type maskedFluxReconcileListener struct{ FluxReconcileListener }

func (l maskedFluxReconcileListener) OnProgress(json string) {
	l.FluxReconcileListener.OnProgress(privacy.mask(json))
}

func (l maskedFluxReconcileListener) OnDone(result, errMessage string) {
	l.FluxReconcileListener.OnDone(result, privacy.maskPlain(errMessage))
}

type maskedHelmRollbackListener struct{ HelmRollbackListener }

func (l maskedHelmRollbackListener) OnProgress(json string) {
	l.HelmRollbackListener.OnProgress(privacy.mask(json))
}

func (l maskedHelmRollbackListener) OnDone(result, errMessage string) {
	l.HelmRollbackListener.OnDone(result, privacy.maskPlain(errMessage))
}
