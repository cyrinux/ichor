import Foundation
import Ichorgo
import IchorCore

enum NetPerfEvent: Sendable {
    case progress(NetPerfProgress)
    /// `error` is nil when the test completed.
    case done(NetPerfReport, error: String?)
}

/// Network tests between two nodes, through the Kubernetes API (os:admin): netperf pods in a
/// temporary namespace, like `cilium connectivity perf` (see TalosClient for the conventions).
extension TalosClient {
    /// The Kubernetes nodes a test can run between, by name.
    func netPerfNodes() async throws -> [NetPerfNode] {
        let list: NetPerfNodeList = try await Self.json { [config, context, kubeServer] in
            IchorgoNetPerfNodes(config, context, kubeServer, $0)
        }
        return list.nodes
    }

    /// Starts a test. The stream finishes after `done`; `stop` ends the test early, and `done`
    /// follows once Go deleted the test namespace, so stop rather than cancel the consuming
    /// task (which also stops the test, but drops that last event).
    func netPerf(_ setup: NetPerfSetup) -> (events: AsyncStream<NetPerfEvent>, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: NetPerfEvent.self)
        let bridge = NetPerfBridge(
            progress: { continuation.yield(.progress($0)) },
            done: {
                continuation.yield(.done($0, error: $1))
                continuation.finish()
            }
        )
        let run = IchorgoStartNetPerf(config, context, kubeServer, setup.server, setup.client,
                                          setup.hostNetwork, setup.seconds, bridge)
        continuation.onTermination = { _ in
            run?.cancel()
            _ = bridge // keep the listener alive for the whole test
        }
        return (stream, { run?.cancel() })
    }
}

private final class NetPerfBridge: NSObject, IchorgoNetPerfListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (NetPerfProgress) -> Void
    private let done: @Sendable (NetPerfReport, String?) -> Void

    init(progress: @escaping @Sendable (NetPerfProgress) -> Void, done: @escaping @Sendable (NetPerfReport, String?) -> Void) {
        self.progress = progress
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let decoded = try? TalosJSON.decode(NetPerfProgress.self, from: json) else { return }
        progress(decoded)
    }

    func onDone(_ reportJSON: String?, errMessage: String?) {
        let report = reportJSON.flatMap { try? TalosJSON.decode(NetPerfReport.self, from: $0) } ?? NetPerfReport()
        done(report, errMessage.nonEmpty)
    }
}
