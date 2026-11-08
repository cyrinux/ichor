import Foundation
import IchorCore
import Ichorgo
import UIKit

/// The core's redacted diagnostics log (go/ichorgo/diag*.go) and the zip made from it, on this
/// device only: nothing leaves the phone unless the user shares the zip. Every line is redacted
/// by the core when it is written; still, never hand it a cluster, node, namespace, object name
/// or path. No cluster call, so no client needed.
extension TalosClient {
    /// The newest `limit` entries (0 = all), newest first.
    static func diagEntries(limit: Int) async throws -> [DiagEntry] {
        try await json { IchorgoDiagEntries(limit, $0) }
    }

    /// Adds an entry: `level` debug, info, warn or error; `msg` a constant text; `attrs`
    /// numbers, booleans and short words. Never throws: a log is not worth a failure.
    static func diagLog(_ level: String, _ msg: String, attrs: [String: DiagValue] = [:]) {
        var error: NSError?
        _ = IchorgoDiagLog(level, "ios", msg, DiagnosticsFiles.attrsJSON(attrs), &error)
    }

    /// Forgets every entry, and the kept files.
    static func diagClear() async throws {
        try await run { error -> Void in _ = IchorgoDiagClear(error) }
    }

    /// Keeps the log in the data directory across launches (`on`) or in memory only (its files
    /// deleted). Best effort.
    static func setDiagPersist(_ on: Bool) {
        var error: NSError?
        _ = IchorgoSetDiagPersist(on, &error)
    }

    /// Applies the stored setting; call at launch right after `setDataDirectory`.
    static func applyStoredDiagPersist() {
        setDiagPersist(UserDefaults.standard.bool(forKey: DiagnosticsFiles.persistKey))
    }

    /// Sends the app's startup steps to the diagnostics log, whatever the startup-timing
    /// setting; call first at launch.
    static func startDiagStartupTrace() {
        StartupTrace.setRingSink { step, sinceStart, delta in
            diagLog(
                "info", "startup \(step)",
                attrs: ["since_start_ms": .number(Double(sinceStart)), "delta_ms": .number(Double(delta))])
        }
    }

    /// Writes the zip into `<tmp>/diagnostics/` (emptied first, so one zip at most is kept), with
    /// app.json (this build and device) and platform.json (the stored MetricKit payloads).
    static func diagExport(now: Date = Date()) async throws -> DiagExportSummary {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("diagnostics", isDirectory: true)
        try? FileManager.default.removeItem(at: folder)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let dest = folder.appendingPathComponent(DiagnosticsFiles.zipName(at: now)).path
        let app = await appInfo().json()
        let platform = DiagnosticsFiles.platformJSON(payloads: CrashDiagnostics.storedPayloads())
        return try await json { IchorgoDiagExport(dest, app, platform, $0) }
    }

    /// This build and device, for app.json.
    @MainActor private static func appInfo() -> AppInfo {
        var system = utsname()
        uname(&system)
        let info = Bundle.main.infoDictionary
        return AppInfo(
            version: info?["CFBundleShortVersionString"] as? String, build: info?["CFBundleVersion"] as? String,
            system: UIDevice.current.systemName, systemVersion: UIDevice.current.systemVersion,
            model: AppInfo.machine(system.machine), locale: Locale.current.identifier(.bcp47),
            dataStore: dataStoreStatus())
    }
}
