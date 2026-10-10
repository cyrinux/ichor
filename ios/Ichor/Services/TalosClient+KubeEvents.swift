import Foundation
import Ichorgo
import IchorCore

/// What the cluster's events stream reports: batches of rows, how the stream is doing, then
/// how it ended (`error` nil when cancelled).
enum KubeEventsStreamItem: Sendable {
    case batch(LiveKubeEventsBatch)
    case status(LiveKubeEventsStatus)
    case done(error: String?)
}

extension TalosClient {
    /// The cluster's Kubernetes events kept live (os:admin, like the other watches): a reset
    /// batch of the newest rows first, then the rows that changed, at most every 250 ms.
    /// `namespace` nil for every one; `warningsOnly` keeps the Warning events alone.
    func liveKubeEvents(namespace: String?, warningsOnly: Bool) -> AsyncStream<KubeEventsStreamItem> {
        TalosClient.bridged { continuation in
            let bridge = KubeEventsBridge(
                batch: { continuation.yield(.batch($0)) },
                status: { continuation.yield(.status($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartKubeEvents(kubeConfig, kubeContext, kubeAPIServer, namespace ?? "", warningsOnly, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

/// Bridges the Go listener (called from Go goroutines) to the stream: each JSON decoded, one
/// that does not decode dropped.
private final class KubeEventsBridge: NSObject, IchorgoKubeEventsListenerProtocol, @unchecked Sendable {
    private let batch: @Sendable (LiveKubeEventsBatch) -> Void
    private let status: @Sendable (LiveKubeEventsStatus) -> Void
    private let done: @Sendable (String?) -> Void

    init(batch: @escaping @Sendable (LiveKubeEventsBatch) -> Void,
         status: @escaping @Sendable (LiveKubeEventsStatus) -> Void,
         done: @escaping @Sendable (String?) -> Void) {
        self.batch = batch
        self.status = status
        self.done = done
    }

    func onEvents(_ batchJSON: String?) {
        guard let batchJSON, let decoded = try? TalosJSON.decode(LiveKubeEventsBatch.self, from: batchJSON) else { return }
        batch(decoded)
    }

    func onStatus(_ stateJSON: String?) {
        guard let stateJSON, let decoded = try? TalosJSON.decode(LiveKubeEventsStatus.self, from: stateJSON) else { return }
        status(decoded)
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
