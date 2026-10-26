import Foundation
import IchorCore
import Ichorgo

enum CARotationEvent: Sendable {
    case progress(CARotationProgress)
    /// A talosconfig to store for the context: the transition one, then the `isFinal` one.
    case talosconfig(String, isFinal: Bool)
    /// `error` nil once the CA was rotated.
    case done(error: String?)
}

/// Rotating the cluster's Talos API CA (see TalosClient for the conventions).
extension TalosClient {
    /// What rotating the context's Talos API CA would do (os:admin): the CA, the nodes and
    /// what stands in the way. Never cached.
    func caRotationPlan() async throws -> CARotationPlan {
        try await Self.json { [config, context] in IchorgoTalosCARotationPlan(config, context, $0) }
    }

    /// Starts rotating the context's Talos API CA (StartTalosCARotation). The talosconfigs it
    /// hands over are credentials: they only go through the stream, never logged. `proceed`
    /// tells Go the transition one is stored; `stop` ends the run before its next step.
    func startCARotation() -> (events: AsyncStream<CARotationEvent>, proceed: @Sendable () -> Void, stop: @Sendable () -> Void) {
        let (stream, continuation) = AsyncStream.makeStream(of: CARotationEvent.self)
        let bridge = CARotationBridge(
            progress: { continuation.yield(.progress($0)) },
            talosconfig: { continuation.yield(.talosconfig($0, isFinal: $1)) },
            done: {
                continuation.yield(.done(error: $0))
                continuation.finish()
            }
        )
        let run = IchorgoStartTalosCARotation(config, context, bridge)
        continuation.onTermination = { _ in
            _ = bridge // keep the listener alive for the whole run
        }
        return (stream, { run?.proceed() }, { run?.cancel() })
    }

    /// This device's fingerprint lineage for a backup, nil when no CA was rotated.
    static func fingerprintLineage() async -> [String: String]? {
        let json = try? await run { IchorgoFingerprintLineageExport($0) }
        guard let json, let lineage = try? JSONDecoder().decode([String: String].self, from: Data(json.utf8)),
              !lineage.isEmpty else { return nil }
        return lineage
    }

    /// Merges a backup's lineage into this device's, before its talosconfig is parsed. Best
    /// effort: at worst a rotated cluster's per-cluster settings are not matched.
    static func restoreFingerprintLineage(_ lineage: [String: String]?) async {
        guard let lineage, !lineage.isEmpty, let data = try? JSONEncoder().encode(lineage) else { return }
        let json = String(decoding: data, as: UTF8.self)
        try? await run { error -> Void in _ = IchorgoFingerprintLineageImport(json, error) }
    }
}

/// Receives Go's rotation callbacks (on Go's threads) and hands them over decoded.
private final class CARotationBridge: NSObject, IchorgoTalosCARotationListenerProtocol, @unchecked Sendable {
    private let progress: @Sendable (CARotationProgress) -> Void
    private let talosconfig: @Sendable (String, Bool) -> Void
    private let done: @Sendable (String?) -> Void

    init(progress: @escaping @Sendable (CARotationProgress) -> Void,
         talosconfig: @escaping @Sendable (String, Bool) -> Void,
         done: @escaping @Sendable (String?) -> Void) {
        self.progress = progress
        self.talosconfig = talosconfig
        self.done = done
    }

    func onProgress(_ json: String?) {
        guard let json, let event = try? TalosJSON.decode(CARotationProgress.self, from: json) else { return }
        progress(event)
    }

    func onTalosconfig(_ talosconfigYAML: String?, isFinal: Bool) {
        guard let talosconfigYAML, !talosconfigYAML.isEmpty else { return }
        talosconfig(talosconfigYAML, isFinal)
    }

    func onDone(_ errMessage: String?) {
        done(errMessage.nonEmpty)
    }
}
