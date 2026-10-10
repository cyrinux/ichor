import Foundation
import Ichorgo
import IchorCore

enum MultiConfigEvent: Sendable {
    case progress(MultiConfigProgress)
    /// nil when every node took the change (or was skipped).
    case done(error: String?)
}

/// The same machine config change on several nodes (see TalosClient for the conventions).
extension TalosClient {
    /// `edits` replayed on each node's own config, previewed per node with a dry run (os:admin).
    /// A node the edits do not fit carries its error.
    func previewMachineConfigMulti(nodes: [String], edits: [ConfigEdit]) async throws -> MultiConfigPreview {
        let csv = nodes.joined(separator: ",")
        let editsJSON = Self.editsJSON(edits)
        return try await Self.json { [config, context] in IchorgoMachineConfigMultiPreview(config, context, csv, editsJSON, $0) }
    }

    /// Applies `edits` to `nodes` one after the other in `mode` (workers first, control planes
    /// last). Ending the stream stops following; what was sent stays.
    func applyMachineConfigMulti(nodes: [String], edits: [ConfigEdit], mode: ConfigApplyMode) -> AsyncStream<MultiConfigEvent> {
        let csv = nodes.joined(separator: ",")
        let editsJSON = Self.editsJSON(edits)
        return Self.bridged { [config, context] continuation in
            let bridge = MultiConfigApplyBridge(
                progress: { continuation.yield(.progress($0)) },
                done: {
                    continuation.yield(.done(error: $0))
                    continuation.finish()
                }
            )
            let run = IchorgoStartConfigApplyMulti(config, context, csv, editsJSON, mode.rawValue, bridge)
            return BridgedRun(bridge) { run?.cancel() }
        }
    }

    private static func editsJSON(_ edits: [ConfigEdit]) -> String {
        "[" + edits.map { $0.json() }.joined(separator: ",") + "]"
    }
}

private final class MultiConfigApplyBridge: NSObject, IchorgoConfigApplyListenerProtocol, @unchecked Sendable {
    private let sink: JSONSink<MultiConfigProgress>

    init(progress: @escaping @Sendable (MultiConfigProgress) -> Void, done: @escaping @Sendable (String?) -> Void) {
        sink = JSONSink(item: progress, done: done)
    }

    func onProgress(_ json: String?) { sink.emit(json) }

    func onDone(_ errMessage: String?) { sink.finish(errMessage) }
}
