import Foundation
import Ichorgo
import IchorCore

enum ClusterUpgradeEvent: Sendable {
    case progress(ClusterUpgradeProgress)
    /// nil when every node was upgraded.
    case done(error: String?)
}

/// A started roll: its events, and what the run screen can ask of it.
final class ClusterUpgradeHandle: @unchecked Sendable {
    let events: AsyncStream<ClusterUpgradeEvent>
    private let run: IchorgoClusterUpgradeRun?

    fileprivate init(events: AsyncStream<ClusterUpgradeEvent>, run: IchorgoClusterUpgradeRun?) {
        self.events = events
        self.run = run
    }

    /// Stops before the next node (a node being upgraded finishes).
    func pause() { run?.pause() }

    /// Goes on after a pause, retries a failed gate, or skips the rest of the settle time.
    func resume() { run?.resume() }

    /// Ends the roll before its next node; the nodes upgraded stay upgraded.
    func abort() { run?.abort() }
}

/// The rolling cluster upgrade (see TalosClient for the conventions).
extension TalosClient {
    /// Every node to `version`, in the order the roll follows, with each node's checks. Nodes
    /// already on the version are done: a rerun continues with the others.
    func clusterUpgradePlan(version: String) async throws -> ClusterUpgradePlan {
        try await Self.json { [config, context, kubeServer] in IchorgoClusterUpgradePlan(config, context, kubeServer, version, $0) }
    }

    /// Upgrades every node to `version`, one after the other (StartClusterUpgrade). Ending the
    /// stream stops following: the node being upgraded goes on, and the lock expires.
    func startClusterUpgrade(version: String, drain: Bool, acknowledged: Bool) -> ClusterUpgradeHandle {
        let (stream, continuation) = AsyncStream.makeStream(of: ClusterUpgradeEvent.self)
        let bridge = ClusterUpgradeBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done(error: $0))
                continuation.finish()
            }
        )
        let run = IchorgoStartClusterUpgrade(config, context, kubeServer, version, drain, acknowledged, bridge)
        continuation.onTermination = { _ in
            run?.cancel()
            _ = bridge // keep the listener alive for the whole roll
        }
        if run == nil {
            continuation.yield(.done(error: String(localized: "The cluster upgrade could not start.")))
            continuation.finish()
        }
        return ClusterUpgradeHandle(events: stream, run: run)
    }
}

private final class ClusterUpgradeBridge: NSObject, IchorgoClusterUpgradeListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<ClusterUpgradeProgress>

    init(progress: @escaping @Sendable (ClusterUpgradeProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}
