import Foundation
import Ichorgo
import IchorCore

enum K8sUpgradeEvent: Sendable {
    case progress(K8sUpgradeProgress)
    /// nil when every component was upgraded (or, in a dry run, accepted).
    case done(error: String?)
}

/// A started Kubernetes upgrade: its events, and Cancel (after the node being changed).
final class K8sUpgradeHandle: @unchecked Sendable {
    let events: AsyncStream<K8sUpgradeEvent>
    private let run: IchorgoK8sUpgradeRun?

    fileprivate init(events: AsyncStream<K8sUpgradeEvent>, run: IchorgoK8sUpgradeRun?) {
        self.events = events
        self.run = run
    }

    func cancel() { run?.cancel() }
}

/// The Kubernetes upgrade (see TalosClient for the conventions).
extension TalosClient {
    /// The versions offered: one minor up at most, inside the range the cluster's Talos supports.
    func k8sUpgradeVersions() async throws -> K8sVersionChoice {
        try await Self.json { [config, context] in IchorgoK8sUpgradeVersions(config, context, $0) }
    }

    /// What upgrading Kubernetes to `version` would change, read-only (os:admin).
    func k8sUpgradePlan(version: String) async throws -> K8sUpgradePlan {
        try await Self.json { [config, context, kubeServer] in IchorgoK8sUpgradePlan(config, context, kubeServer, version, $0) }
    }

    /// StartK8sUpgrade: with `dryRun` each node only checks its change.
    func startK8sUpgrade(version: String, dryRun: Bool) -> K8sUpgradeHandle {
        let (stream, continuation) = AsyncStream.makeStream(of: K8sUpgradeEvent.self)
        let bridge = K8sUpgradeBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done(error: $0))
                continuation.finish()
            }
        )
        let run = IchorgoStartK8sUpgrade(config, context, kubeServer, version, dryRun, bridge)
        continuation.onTermination = { _ in
            _ = bridge // keep the listener alive for the whole run
        }
        if run == nil {
            continuation.yield(.done(error: String(localized: "The Kubernetes upgrade could not start.")))
            continuation.finish()
        }
        return K8sUpgradeHandle(events: stream, run: run)
    }
}

private final class K8sUpgradeBridge: NSObject, IchorgoK8sUpgradeListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<K8sUpgradeProgress>

    init(progress: @escaping @Sendable (K8sUpgradeProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}
