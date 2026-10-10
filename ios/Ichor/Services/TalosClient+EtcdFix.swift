import Foundation
import Ichorgo
import IchorCore

enum EtcdFixEvent: Sendable {
    case progress(EtcdFixProgress)
    /// nil on success.
    case done(error: String?)
}

/// The one-tap etcd NOSPACE fix (StartEtcdNospaceFix).
extension TalosClient {
    /// Snapshot into `destPath` (none when empty) through `snapshotNode`, defragment every
    /// member, disarm the alarm, read etcd again. Ending the stream cancels the run.
    func etcdNospaceFix(snapshotNode: String, destPath: String, encryption: SnapshotEncryption) -> AsyncStream<EtcdFixEvent> {
        let (recipients, passphrase): (String, String) = switch encryption {
        case .keys(let keys): (keys, "")
        case .passphrase(let passphrase): ("", passphrase)
        case .none: ("", "")
        }
        return Self.bridged { [config, context] continuation in
            let bridge = EtcdFixBridge(
                progress: { continuation.yield(.progress($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartEtcdNospaceFix(config, context, snapshotNode, destPath, recipients, passphrase, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }
}

private final class EtcdFixBridge: NSObject, IchorgoEtcdFixListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<EtcdFixProgress>

    init(progress: @escaping @Sendable (EtcdFixProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}
