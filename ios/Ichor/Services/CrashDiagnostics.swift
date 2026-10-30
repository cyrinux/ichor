import Foundation
import IchorCore

#if os(iOS)
    import MetricKit
#endif

/// The crash, hang and exception reports iOS keeps for the app (MetricKit, delivered at a later
/// launch; never on the simulator), kept on this device in `<Application Support>/diagnostics`
/// (the newest 10, out of backups) for Settings → Support → Export diagnostics. Nothing is sent:
/// they leave the phone only in a zip the user shares.
enum CrashDiagnostics {
    private static let folderName = "diagnostics"

    /// Subscribes to MetricKit's diagnostic payloads; call once at launch, after the data
    /// directory is set.
    static func start() {
        #if os(iOS)
            MXMetricManager.shared.add(MetricKitSubscriber.shared)
        #endif
    }

    /// The stored payloads' JSON, newest first.
    static func storedPayloads() -> [Data] {
        guard let folder = AppSupport.existingFolder(folderName) else { return [] }
        let names = (try? FileManager.default.contentsOfDirectory(atPath: folder.path)) ?? []
        return DiagnosticsFiles.payloads(in: names).compactMap {
            try? Data(contentsOf: folder.appendingPathComponent($0))
        }
    }

    /// The stored payloads' diagnostics, newest payload first.
    static func records() -> [CrashRecord] {
        storedPayloads().flatMap { CrashRecord.summarize(metricKitJSON: String(decoding: $0, as: UTF8.self)) }
    }

    /// Keeps `payloads` (MetricKit's JSON), drops the oldest beyond 10, and logs one
    /// diagnostics line per diagnostic (its kind and numbers only).
    static func store(_ payloads: [Data], at date: Date = Date()) {
        guard let folder = try? AppSupport.excludedFolder(folderName) else { return }
        for (index, json) in payloads.enumerated() {
            let url = folder.appendingPathComponent(DiagnosticsFiles.payloadName(at: date, index: index))
            try? json.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
            for record in CrashRecord.summarize(metricKitJSON: String(decoding: json, as: UTF8.self)) {
                TalosClient.diagLog("warn", "metrickit \(record.kind.rawValue)", attrs: record.ringAttrs)
            }
        }
        let names = (try? FileManager.default.contentsOfDirectory(atPath: folder.path)) ?? []
        for name in DiagnosticsFiles.pruned(names) {
            try? FileManager.default.removeItem(at: folder.appendingPathComponent(name))
        }
    }
}

#if os(iOS)
    /// MetricKit's subscriber: diagnostic payloads only (metric payloads are ignored).
    private final class MetricKitSubscriber: NSObject, MXMetricManagerSubscriber, Sendable {
        static let shared = MetricKitSubscriber()

        func didReceive(_ payloads: [MXDiagnosticPayload]) {
            CrashDiagnostics.store(payloads.map { $0.jsonRepresentation() })
        }
    }
#endif
