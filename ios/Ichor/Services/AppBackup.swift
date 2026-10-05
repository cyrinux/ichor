import Foundation
import IchorCore

/// Backs up the talosconfig and the settings that go with it into a file sealed with a
/// passphrase (Argon2id + AES-256-GCM, in the Go core), and restores one, on this device or
/// on another, iOS or Android. The stored config cannot be copied as it is: its key never
/// leaves this device's Secure Enclave.
@MainActor
enum AppBackup {
    /// The backup file of the stored config and settings, sealed with `passphrase`.
    static func create(model: AppModel, passphrase: String) async throws -> Data {
        guard let yaml = model.yaml, let summary = model.summary else {
            throw TalosError(message: String(localized: "No talosconfig is stored."))
        }
        let payload = BackupPayload(
            platform: "ios",
            createdAt: Int64(Date().timeIntervalSince1970),
            talosconfig: yaml,
            activeContextIndex: summary.contexts.firstIndex { $0.name == model.activeContext },
            settings: BackupSettings(
                themeMode: model.theme.rawValue,
                privacyMask: model.privacyMask,
                privacyMaskWords: model.privacyWords,
                monitorAlerts: BackgroundMonitor.alertsEnabled,
                remoteAppIcons: AppIconSettings.remoteEnabled
            ),
            clusters: backupClusters(
                fingerprints: summary.contexts.map(\.fingerprint),
                names: model.clusterNames,
                colors: model.clusterColors,
                kubeServers: model.kubeServers,
                vpnOnly: model.vpnOnly,
                wakeOnLan: WakeOnLanStore.shared.allTargets
            )
        )
        let json = String(decoding: try JSONEncoder().encode(payload), as: UTF8.self)
        return try await localized { try await TalosClient.encryptBackup(payload: json, passphrase: passphrase) }
    }

    /// Replaces the stored config and settings with those of `file`. Throws
    /// `BackupError.wrongPassphrase` for a wrong passphrase, so the user can try again.
    static func restore(_ file: Data, passphrase: String, model: AppModel) async throws {
        let json = try await localized { try await TalosClient.decryptBackup(file, passphrase: passphrase) }
        guard let payload = try? JSONDecoder().decode(BackupPayload.self, from: Data(json.utf8)) else {
            throw BackupError.invalidContent
        }
        let settings = payload.settings
        // The config first: on failure nothing else changed. The screenshot mode re-parses it after.
        try await model.replace(yaml: payload.talosconfig, activeIndex: payload.activeContextIndex)
        let restored = restoredClusters(payload.clusters, fingerprints: model.summary?.contexts.map(\.fingerprint) ?? [])
        // Checked like a typed one: an address the Go core refuses is dropped.
        var kubeServers: [String: String] = [:]
        for (fp, server) in restored.kubeServers {
            if let normalized = try? await TalosClient.normalizeKubeServer(server), !normalized.isEmpty {
                kubeServers[fp] = normalized
            }
        }
        model.restoreClusterSettings(names: restored.names, colors: restored.colors, kubeServers: kubeServers,
                                     vpnOnly: restored.vpnOnly)
        WakeOnLanStore.shared.restore(restored.wakeOnLan)
        if let mask = settings?.privacyMask {
            await model.setPrivacyMask(mask, words: settings?.privacyMaskWords ?? "")
        }
        if let theme = settings?.themeMode.flatMap(ThemeMode.init(rawValue:)) { model.theme = theme }
        if let icons = settings?.remoteAppIcons { AppIconSettings.remoteEnabled = icons }
        if let alerts = settings?.monitorAlerts {
            // Notifications need this device's permission; without it alerts stay off.
            let on = alerts ? await BackgroundMonitor.requestPermission() : false
            BackgroundMonitor.alertsEnabled = on
            if on { BackgroundMonitor.schedule() }
        }
    }

    private static func localized<T>(_ call: () async throws -> T) async throws -> T {
        do {
            return try await call()
        } catch let error as TalosError {
            if let code = BackupError(coreMessage: error.message) { throw code }
            throw error
        }
    }
}

extension BackupError: @retroactive LocalizedError {
    public var errorDescription: String? {
        switch self {
        case .passphraseShort: String(localized: "Use at least \(backupMinPassphrase) characters.")
        case .wrongPassphrase: String(localized: "Wrong passphrase, or the file is damaged.")
        case .notBackup: String(localized: "This file is not an Ichor backup.")
        case .unsupported: String(localized: "This backup was made by a newer version of Ichor. Update the app to restore it.")
        case .invalidContent: String(localized: "The backup opened but its content is not valid.")
        }
    }
}
