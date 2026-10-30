import Foundation
import IchorCore

/// Backs up the talosconfig, the kubeconfig of the clusters added without Talos, and the
/// settings that go with them (pins, Git hosts, AI settings, the pull requests opened from the
/// phone, the imported plugins; their keys and tokens only when
/// the user opts in) into a file sealed with a
/// passphrase (Argon2id + AES-256-GCM, in the Go core) or saved as an age file (a passphrase or
/// age public keys), and restores one, on this device or
/// on another, iOS or Android. The stored config cannot be copied as it is: its key never
/// leaves this device's Secure Enclave.
@MainActor
enum AppBackup {
    /// The backup file of the stored config and settings, sealed with `passphrase`. The AI keys
    /// and Git tokens go in only with `includeSecrets`. The payload is never logged.
    /// As an age file (`format` .age), it is sealed with either `passphrase` or `recipients`
    /// (age1… public keys, one per line), the other one empty.
    static func create(
        model: AppModel, ai: AISettings, gitHosts: GitHostStore, passphrase: String,
        includeSecrets: Bool, format: BackupFileFormat = .ichor,
        recipients: String = ""
    ) async throws -> Data {
        guard model.hasConfig, let summary = model.summary else {
            throw TalosError(message: String(localized: "No talosconfig is stored."))
        }
        let fingerprints = summary.contexts.map(\.fingerprint)
        let aiBackup = await ai.backup(includeKeys: includeSecrets)
        // The pull requests opened from the phone: format 4 only when there are some.
        let proposals = backupProposals(model.git.proposals, fingerprints: fingerprints)
        // The imported plugins: format 5 only when there are some.
        let plugins = backupPlugins(try await TalosClient.exportPlugins())
        // A cluster whose CA was rotated keeps its fingerprint where the backup is restored.
        let lineage = await TalosClient.fingerprintLineage()
        let payload = BackupPayload(
            format: backupPayloadFormat(hasProposals: proposals != nil, hasPlugins: plugins != nil),
            platform: "ios",
            createdAt: Int64(Date().timeIntervalSince1970),
            talosconfig: model.yaml ?? "",
            kubeconfig: model.kubeYAML,
            activeContextIndex: summary.contexts.firstIndex { $0.name == model.activeContext },
            settings: backupSettings(model: model, fingerprints: fingerprints),
            clusters: backupClusters(
                fingerprints: fingerprints,
                names: model.clusterNames,
                colors: model.clusterColors,
                kubeServers: model.kubeServers,
                vpnOnly: model.settings.vpnOnly,
                wakeOnLan: WakeOnLanStore.shared.allTargets,
                kubeAccess: model.kubeAccess,
                repos: model.git.repos,
                auditEvents: model.settings.auditEventClusters
            ),
            kubeAuth: backupKubeAuth(fingerprints: model.signInFingerprints),
            // iOS pins nothing in screenshot mode: the stored pins are all of the real scope.
            pins: backupPins(fingerprints: fingerprints) { PinStore.shared.pins(for: $0) },
            gitHosts: gitHosts.hosts.isEmpty ? nil : gitHosts.hosts,
            ai: aiBackup.settings,
            secrets: backupSecrets(
                include: includeSecrets, aiKeys: aiBackup.keys,
                gitHostTokens: includeSecrets ? gitHosts.tokensForBackup() : [:]),
            proposals: proposals,
            plugins: plugins,
            fingerprintLineage: lineage
        )
        let json = try TalosJSON.encode(payload)
        switch format {
        case .ichor:
            return try await localized { try await TalosClient.encryptBackup(payload: json, passphrase: passphrase) }
        case .age:
            let keys = ageRecipientsArgument(recipients)
            return try await localized {
                try await TalosClient.encryptBackupAge(payload: json, passphrase: passphrase, recipients: keys)
            }
        }
    }

    /// Whether a picked `file` is an age backup sealed for age keys only, which the phone
    /// cannot open: its error shows at once instead of a passphrase prompt. The empty
    /// passphrase try runs off the main thread (in the Go core).
    static func needsIdentity(_ file: Data) async -> Bool {
        guard await TalosClient.isAgeBackup(file) else { return false }
        do {
            _ = try await localized { try await TalosClient.decryptBackup(file, passphrase: "") }
            return false
        } catch {
            return pickedBackupNeedsIdentity(isAge: true, emptyPassphraseError: error)
        }
    }

    /// Replaces the stored config and settings with those of `file`. Throws
    /// `BackupError.wrongPassphrase` for a wrong passphrase, so the user can try again. The
    /// answer says what became of the backup's plugins, nil when it has none.
    @discardableResult
    static func restore(
        _ file: Data, passphrase: String, model: AppModel, ai: AISettings,
        gitHosts: GitHostStore
    ) async throws -> String? {
        let json = try await localized { try await TalosClient.decryptBackup(file, passphrase: passphrase) }
        guard let payload = try? JSONDecoder().decode(BackupPayload.self, from: Data(json.utf8)) else {
            throw BackupError.invalidContent
        }
        let settings = payload.settings
        // Before the configs are parsed: a cluster whose CA was rotated keeps its fingerprint.
        await TalosClient.restoreFingerprintLineage(payload.fingerprintLineage)
        // Both configs first: on failure nothing else changed. The screenshot mode re-parses them after.
        try await model.replace(
            talos: restoredTalosconfig(payload), kube: restoredKubeconfig(payload),
            activeIndex: payload.activeContextIndex)
        let restored = restoredClusters(
            payload.clusters, fingerprints: model.summary?.contexts.map(\.fingerprint) ?? [])
        // Checked like a typed one: an address the Go core refuses is dropped.
        var kubeServers: [String: String] = [:]
        for (fp, server) in restored.kubeServers {
            if let normalized = try? await TalosClient.normalizeKubeServer(server), !normalized.isEmpty {
                kubeServers[fp] = normalized
            }
        }
        model.restoreClusterSettings(
            names: restored.names, colors: restored.colors, kubeServers: kubeServers,
            vpnOnly: restored.vpnOnly, kubeAccess: restored.kubeAccess, repos: restored.repos,
            auditEvents: restored.auditEvents)
        // After the configs: the sign-ins of the kubeconfig and Omni clusters restored.
        KubeAuthStore.shared.restore(restoredKubeAuth(payload, fingerprints: model.signInFingerprints))
        WakeOnLanStore.shared.restore(restored.wakeOnLan)
        PinStore.shared.restore(restoredPins(payload, fingerprints: model.summary?.contexts.map(\.fingerprint) ?? []))
        // A format 4 backup's pull requests; an older one leaves the stored ones as they are.
        if let proposals = restoredProposals(payload, fingerprints: model.summary?.contexts.map(\.fingerprint) ?? []) {
            model.git.store(proposals: proposals)
        }
        gitHosts.restore(restoredGitHosts(payload))
        let pluginsNote = await restorePlugins(payload)
        if payload.ai != nil || payload.secrets?.aiKeys != nil {
            await ai.restore(payload.ai, keys: payload.secrets?.aiKeys ?? [:])
        }
        restoreStorageThresholds(settings)
        // The alert rules, overrides only of the clusters restored; an older backup has none.
        if let rules = settings?.alertRules {
            AlertRulesStore.save(rules.keeping(model.summary?.contexts.map(\.fingerprint) ?? []))
        }
        // settings.appLockMode is not applied: a lock's secret is bound to the device it was set
        // on. With a real cluster restored and no lock here, RootView shows LockOnboardingView
        // by itself (the lock is required), so the lock is offered again, never enabled.
        // settings.allowScreenshots is Android only: iOS cannot block screenshots.
        if let mask = settings?.privacyMask {
            await model.settings.setPrivacyMask(mask, words: settings?.privacyMaskWords ?? "")
        }
        if let theme = settings?.themeMode.flatMap(ThemeMode.init(rawValue:)) { model.settings.theme = theme }
        if let icons = settings?.remoteAppIcons { AppIconSettings.remoteEnabled = icons }
        if let live = settings?.liveClusterStats { LiveStatsSettings.enabled = live }
        if let alerts = settings?.monitorAlerts {
            // Notifications need this device's permission; without it alerts stay off.
            let on = alerts ? await BackgroundMonitor.requestPermission() : false
            BackgroundMonitor.alertsEnabled = on
            if on { BackgroundMonitor.schedule() }
        }
        return pluginsNote
    }

    /// Stores a format 5 backup's plugins, each checked again here; the line the restore shows
    /// about them, nil when the backup has none. A failure is that line: the clusters are restored.
    private static func restorePlugins(_ payload: BackupPayload) async -> String? {
        guard let plugins = restoredPlugins(payload) else { return nil }
        do {
            let result = try await TalosClient.restorePlugins(plugins)
            var lines = [String(localized: "Plugins restored: \(result.restored.count).")]
            if !result.errors.isEmpty {
                lines.append(String(localized: "Plugins not restored: \(result.skippedLine)"))
            }
            return lines.joined(separator: " ")
        } catch {
            return String(localized: "Plugins not restored: \(error.localizedDescription)")
        }
    }

    private static func restoreStorageThresholds(_ settings: BackupSettings?) {
        guard settings?.storageWarnPercent != nil || settings?.storageCriticalPercent != nil else { return }
        let current = BackgroundMonitor.storageThresholds
        // StorageThresholds clamps them like the Settings sliders.
        BackgroundMonitor.storageThresholds = StorageThresholds(
            warn: settings?.storageWarnPercent ?? current.warn,
            crit: settings?.storageCriticalPercent ?? current.crit)
    }

    /// The sign-ins of the kubeconfig and Omni clusters as a backup keeps them (what the user entered,
    /// no session; Go KubeAuthForBackup), nil when there is none.
    private static func backupKubeAuth(fingerprints: [String]) -> [String: String]? {
        let stored = KubeAuthMap.keeping(KubeAuthStore.shared.all() ?? [:], fingerprints: fingerprints)
        var kept: [String: String] = [:]
        for (fp, state) in stored {
            let backup = TalosClient.authForBackup(state)
            if !backup.isEmpty { kept[fp] = backup }
        }
        return kept.isEmpty ? nil : kept
    }

    private static func backupSettings(model: AppModel, fingerprints: [String]) -> BackupSettings {
        var settings = BackupSettings(
            themeMode: model.settings.theme.rawValue,
            privacyMask: model.settings.privacyMask,
            privacyMaskWords: model.settings.privacyWords,
            monitorAlerts: BackgroundMonitor.alertsEnabled,
            remoteAppIcons: AppIconSettings.remoteEnabled
        )
        settings.liveClusterStats = LiveStatsSettings.enabled
        let securityKey = model.lockStore.securityKeys?.keys.isEmpty == false
        settings.appLockMode = BackupLockMode(enabled: model.lockStore.lock.enabled, securityKey: securityKey).rawValue
        let thresholds = BackgroundMonitor.storageThresholds
        settings.storageWarnPercent = thresholds.warn
        settings.storageCriticalPercent = thresholds.crit
        settings.alertRules = AlertRulesStore.load().forBackup(fingerprints: fingerprints)
        return settings
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
        case .unsupported:
            String(localized: "This backup was made by a newer version of Ichor. Update the app to restore it.")
        case .invalidContent: String(localized: "The backup opened but its content is not valid.")
        case .needsIdentity: String(localized: "This backup opens with your age key on a computer.")
        }
    }
}
