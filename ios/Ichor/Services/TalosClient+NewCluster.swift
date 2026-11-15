import Foundation
import Ichorgo
import IchorCore

/// A new cluster from a machine in maintenance mode (see TalosClient for the conventions).
/// Static: no cluster exists yet, so no stored talosconfig is involved until the app stores the
/// draft's.
extension TalosClient {
    /// The cluster's secrets and configs, held by the Go core behind the draft's id; nothing is sent.
    static func newClusterPrepare(_ spec: NewClusterSpec) async throws -> NewClusterDraft {
        let specJSON = String(decoding: try JSONEncoder().encode(spec), as: UTF8.self)
        return try await json { IchorgoNewClusterPrepare(specJSON, $0) }
    }

    /// Sends the draft's config to the machine at `request.address` and follows it until it is
    /// bootstrapped (StartNewCluster). The draft's talosconfig must be stored already. After `stop`
    /// Go still reports `done`: once the config is sent the install goes on.
    static func startNewCluster(
        _ request: JoinRequest
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: JoinEvent.self)
        let bridge = JoinBridge(
            progress: { continuation.yield(.progress($0)) },
            done: { address, error in
                continuation.yield(.done(address: address, error: error))
                continuation.finish()
            }
        )
        let start = request.newCluster
        let started = IchorgoStartNewCluster(
            start?.draftId ?? "", request.address, start?.talosconfig ?? "", bridge)
        let run = started.map(GoObject.init)
        continuation.onTermination = { _ in
            run?.value.cancel()
            _ = bridge  // keep the listener alive for the whole run
        }
        return (stream, { run?.value.cancel() })
    }
}
