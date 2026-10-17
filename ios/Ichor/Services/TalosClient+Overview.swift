import Foundation
import Ichorgo
import IchorCore

/// The overview's extras: the cluster map that groups its nodes by site, and live CPU and
/// memory for its summary (see TalosClient for the conventions).
extension TalosClient {
    /// `overview()` node by node (os:reader): each node as it answers or fails, then the
    /// complete overview, byte for byte the one `overview()` returns; a failure throws. Ending
    /// the stream (its consumer cancelled) stops the run, and nothing follows a stop.
    func overviewStream() -> AsyncThrowingStream<OverviewPart, Error> {
        TalosClient.bridgedThrowing { continuation in
            let bridge = OverviewBridge(
                node: { continuation.yield(.node($0)) },
                done: { overview, error in
                    if let overview {
                        continuation.yield(.done(overview))
                        continuation.finish()
                    } else {
                        continuation.finish(throwing: TalosError(message: error ?? "The overview could not be read"))
                    }
                }
            )
            let run = IchorgoStartClusterOverview(config, context, bridge)
            return BridgedRun(bridge) { run?.stop() }
        }
    }

    /// Members, KubeSpan links and sites (os:reader); every part is best-effort in Go.
    func topology() async throws -> ClusterTopology {
        try await Self.json { [config, context] in IchorgoClusterTopology(config, context, $0) }
    }

    /// Cumulative CPU times and current memory of every node that answered in time.
    func clusterStats() async throws -> ClusterStatsSample {
        try await Self.json { [config, context] in IchorgoClusterStats(config, context, $0) }
    }
}

/// The Go overview listener (gomobile calls it on Go threads, one call at a time).
private final class OverviewBridge: NSObject, IchorgoOverviewListenerProtocol, @unchecked Sendable {
    private let node: @Sendable (NodeOverview) -> Void
    private let done: @Sendable (ClusterOverview?, String?) -> Void

    init(node: @escaping @Sendable (NodeOverview) -> Void, done: @escaping @Sendable (ClusterOverview?, String?) -> Void) {
        self.node = node
        self.done = done
    }

    func onNode(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(NodeOverview.self, from: json) else { return }
        node(decoded)
    }

    func onDone(_ overviewJSON: String?, errMessage: String?) {
        if let message = errMessage.nonEmpty {
            done(nil, message)
            return
        }
        do {
            done(try TalosJSON.decode(ClusterOverview.self, from: overviewJSON ?? ""), nil)
        } catch {
            done(nil, error.localizedDescription)
        }
    }
}
