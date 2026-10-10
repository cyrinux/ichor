import Foundation
import Ichorgo
import IchorCore

enum EtcdRecoverEvent: Sendable {
    case progress(EtcdRecoverProgress)
    /// nil on success.
    case done(error: String?)
}

/// etcd recovery from a snapshot file (StartEtcdRecover, SnapshotInspect).
extension TalosClient {
    /// What the snapshot at `path` needs before a recovery.
    static func snapshotInspect(path: String) async throws -> SnapshotInfo {
        let json = try await run { IchorgoSnapshotInspect(path, $0) }
        return try TalosJSON.decode(SnapshotInfo.self, from: json)
    }

    /// Uploads the snapshot at `path` to `node` and bootstraps etcd from it, decrypting with
    /// `identity` or `passphrase` on the fly. Ending the stream cancels the run (once the
    /// bootstrap is requested it cannot be undone).
    func etcdRecover(node: String, path: String, identity: String, passphrase: String, skipHashCheck: Bool) -> AsyncStream<EtcdRecoverEvent> {
        Self.bridged { [config, context] continuation in
            let bridge = EtcdRecoverBridge(
                progress: { continuation.yield(.progress($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartEtcdRecover(config, context, node, path, identity, passphrase, skipHashCheck, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

private final class EtcdRecoverBridge: NSObject, IchorgoEtcdRecoverListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<EtcdRecoverProgress>

    init(progress: @escaping @Sendable (EtcdRecoverProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}
