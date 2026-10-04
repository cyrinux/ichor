import Foundation
import Ichorgo
import IchorCore

enum SupportBundleEvent: Sendable {
    case progress(SupportProgress)
    case done(path: String, size: Int64, error: String?)
}

/// Container logs, storage, etcd member actions, resources, support bundle and version
/// compatibility calls (see TalosClient for the conventions).
extension TalosClient {
    /// Last `lines` lines of a Kubernetes container's log (os:reader); `containerID` is the
    /// id from `containers`.
    func containerLogs(node: String, containerID: String, lines: Int = 500) async throws -> LogTail {
        try await Self.json { [config, context] in IchorgoContainerLogs(config, context, node, containerID, lines, $0) }
    }

    /// `talosctl logs -k -f` of one container, starting with the last `tailLines`.
    func followContainerLogs(node: String, containerID: String, tailLines: Int = 200) -> AsyncStream<LogFollowItem> {
        AsyncStream { continuation in
            let bridge = LogBridge(
                line: { continuation.yield(.line($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartContainerLogFollow(config, context, node, containerID, tailLines, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive for the whole stream
            }
        }
    }

    /// Mounted filesystems of node with their usage, like `talosctl mounts` (os:reader).
    func mounts(node: String) async throws -> NodeMounts {
        try await Self.json { [config, context] in IchorgoNodeMounts(config, context, node, $0) }
    }

    /// Talos volumes of node (os:reader); `supported` is false before Talos had them.
    func volumes(node: String) async throws -> NodeVolumes {
        try await Self.json { [config, context] in IchorgoNodeVolumes(config, context, node, $0) }
    }

    /// `talosctl usage PATH -d DEPTH` on node (os:reader).
    func diskUsage(node: String, path: String, depth: Int = 1) async throws -> DiskUsage {
        try await Self.json { [config, context] in IchorgoNodeDiskUsage(config, context, node, path, depth, $0) }
    }

    /// SMART / NVMe health of node's disks (os:reader); `supported` is false with a reason
    /// when the node cannot tell.
    func diskHealth(node: String) async throws -> NodeDiskHealth {
        try await Self.json { [config, context] in IchorgoNodeDiskHealth(config, context, node, $0) }
    }

    /// `talosctl -n NODE etcd forfeit-leadership` (os:admin); node must be the leader.
    func etcdForfeitLeadership(node: String) async throws -> EtcdForfeitResult {
        try await Self.json { [config, context] in IchorgoEtcdForfeitLeadership(config, context, node, $0) }
    }

    /// `talosctl -n NODE etcd remove-member MEMBER_ID` (os:admin), asked to another member.
    func etcdRemoveMember(node: String, memberID: String) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = IchorgoEtcdRemoveMember(config, context, node, memberID, error)
        }
    }

    /// What removing memberID would leave behind, and what forbids it (os:admin).
    func etcdMemberPlan(memberID: String) async throws -> EtcdMemberPlan {
        try await Self.json { [config, context] in IchorgoEtcdMemberPlan(config, context, memberID, $0) }
    }

    /// Resource types node serves, like `talosctl get rd` (os:reader).
    func resourceTypes(node: String) async throws -> [ResourceType] {
        try await Self.json { [config, context] in IchorgoResourceTypes(config, context, node, $0) }
    }

    /// `talosctl get TYPE --namespace NAMESPACE` on node.
    func resourceList(node: String, namespace: String, type: String) async throws -> ResourceItems {
        try await Self.json { [config, context] in IchorgoResourceList(config, context, node, namespace, type, $0) }
    }

    /// `talosctl get TYPE ID -o yaml` on node.
    func resource(node: String, namespace: String, type: String, id: String) async throws -> ResourceDocument {
        try await Self.json { [config, context] in IchorgoResourceGet(config, context, node, namespace, type, id, $0) }
    }

    /// `talosctl support` for nodesCSV into destPath (os:admin). Cancelling the consuming
    /// task cancels the collection (Go then removes the partial file).
    func supportBundle(nodesCSV: String, destPath: String) -> AsyncStream<SupportBundleEvent> {
        AsyncStream { continuation in
            let bridge = SupportBridge(
                progress: { continuation.yield(.progress($0)) },
                done: {
                    continuation.yield($0)
                    continuation.finish()
                }
            )
            let run = IchorgoStartSupportBundle(config, context, nodesCSV, destPath, bridge)
            continuation.onTermination = { _ in
                run?.cancel()
                _ = bridge // keep the listener alive for the whole collection
            }
        }
    }

    /// What node's Talos version can do (os:reader), see AppModel.loadFeatures for the cache.
    func features(node: String) async throws -> NodeFeatures {
        try await Self.json { [config, context] in IchorgoNodeFeatures(config, context, node, $0) }
    }
}

private final class SupportBridge: NSObject, IchorgoSupportListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (SupportProgress) -> Void
    private let done: @Sendable (SupportBundleEvent) -> Void

    init(progress: @escaping @Sendable (SupportProgress) -> Void, done: @escaping @Sendable (SupportBundleEvent) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(SupportProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ path: String?, size: Int64, errMessage: String?) {
        done(.done(path: path ?? "", size: size, error: errMessage.nonEmpty))
    }
}
