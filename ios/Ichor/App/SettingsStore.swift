import Foundation
import Observation
import IchorCore

enum ThemeMode: String, CaseIterable, Identifiable {
    case auto, light, dark, black

    var id: String { rawValue }
    var label: String {
        switch self {
        case .auto: String(localized: "Auto")
        case .light: String(localized: "Light")
        case .dark: String(localized: "Dark")
        case .black: String(localized: "Black")
        }
    }
}

/// The preferences: what the Settings screens set, kept in UserDefaults, whichever cluster is on
/// screen. Cluster state lives in AppModel; the rules that need the clusters (a whole cluster's
/// contexts, a reload of the one on screen) stay there too and store their result here.
@Observable
@MainActor
final class SettingsStore {
    private enum Keys {
        static let theme = "themeMode"
        static let keepLastKnown = "keepLastKnownState"
        static let vpnOnly = "vpnOnlyClusters"
    }

    /// What a preference change has to reach: the features cache, the config, the screens' reload
    /// and Spotlight. Set once by AppModel.init; weak, the model owns this store.
    @ObservationIgnored weak var model: AppModel?

    var theme: ThemeMode {
        didSet { UserDefaults.standard.set(theme.rawValue, forKey: Keys.theme) }
    }

    /// Screenshot mode (masking done in Go, see TalosClient.setPrivacyMask) and its extra words.
    private(set) var privacyMask: Bool
    private(set) var privacyWords: String
    /// True while the screenshot mode is being turned on or off: the app shows a placeholder
    /// instead of its screens, so no name shows under the wrong setting (see setPrivacyMask).
    private(set) var maskTransition = false
    /// The switch of the screenshot mode in progress: the next one waits for it.
    @ObservationIgnored private var maskSwitch: Task<Void, Never>?

    /// Keep the last data fetched from each cluster on the phone (LastKnownStore), to show it
    /// when the cluster cannot be reached. On unless the user turned it off.
    private(set) var keepLastKnown: Bool

    /// The clusters reached over a VPN only, by context fingerprint: without a VPN up the app
    /// does not try them (screens say to connect it, background checks wait). Only on this device.
    private(set) var vpnOnly: Set<String>

    /// The clusters turned off with "Watch in the background", by context fingerprint: the
    /// background checks skip them (on by default). Only on this device.
    private(set) var monitorUnwatched: Set<String>

    /// The clusters that record actions ("Record actions in the cluster": a Kubernetes Event on
    /// each object the app changes there), by context fingerprint; off by default.
    private(set) var auditEventClusters: Set<String>

    init() {
        let defaults = UserDefaults.standard
        theme = ThemeMode(rawValue: defaults.string(forKey: Keys.theme) ?? "") ?? .auto
        privacyMask = defaults.bool(forKey: PrivacyKeys.enabled)
        privacyWords = defaults.string(forKey: PrivacyKeys.words) ?? ""
        // On unless the user turned it off: the home opens on it at once.
        keepLastKnown = defaults.object(forKey: Keys.keepLastKnown) as? Bool ?? true
        vpnOnly = Set(defaults.stringArray(forKey: Keys.vpnOnly) ?? [])
        monitorUnwatched = Set(defaults.stringArray(forKey: BackgroundMonitor.unwatchedKey) ?? [])
        auditEventClusters = Set(defaults.stringArray(forKey: AuditEventKeys.clusters) ?? [])
    }

    func storeVpnOnly(_ fingerprints: Set<String>) {
        guard fingerprints != vpnOnly else { return }
        vpnOnly = fingerprints
        UserDefaults.standard.set(fingerprints.sorted(), forKey: Keys.vpnOnly)
    }

    func storeMonitorUnwatched(_ fingerprints: Set<String>) {
        guard fingerprints != monitorUnwatched else { return }
        monitorUnwatched = fingerprints
        UserDefaults.standard.set(fingerprints.sorted(), forKey: BackgroundMonitor.unwatchedKey)
    }

    /// Saves the clusters that record actions and hands them to Go.
    func storeAuditEventClusters(_ fingerprints: Set<String>) {
        guard fingerprints != auditEventClusters else { return }
        auditEventClusters = fingerprints
        UserDefaults.standard.set(fingerprints.sorted(), forKey: AuditEventKeys.clusters)
        TalosClient.setAuditEventClusters(fingerprints)
    }

    /// Off: everything kept so far is deleted, with its key.
    func setKeepLastKnown(_ enabled: Bool) {
        keepLastKnown = enabled
        UserDefaults.standard.set(enabled, forKey: Keys.keepLastKnown)
        if !enabled { LastKnownStore.wipe() }
        model?.updateSpotlight()
    }

    /// Turns the screenshot mode on or off, keeping its words (the Privacy toggle).
    func setPrivacyMask(_ on: Bool) async {
        await setPrivacyMask(on, words: privacyWords)
    }

    /// Turns the screenshot mode on or off (or changes its words). One switch at a time: a
    /// second call waits for the first, so the two never interleave.
    func setPrivacyMask(_ enabled: Bool, words: String) async {
        let previous = maskSwitch
        let current = Task {
            await previous?.value
            await switchPrivacyMask(enabled, words: words)
        }
        maskSwitch = current
        await current.value
    }

    /// With the placeholder up (maskTransition), drops the data held with the previous names
    /// (widget snapshot, last known state, live samples, search history), then changes Go's
    /// mask and re-reads the config, whose context names are masked too (MaskTransition).
    private func switchPrivacyMask(_ enabled: Bool, words: String) async {
        guard enabled != privacyMask || words != privacyWords else { return }
        // Words changed while the mode is off: nothing shown changes.
        guard enabled || privacyMask else {
            storePrivacyMask(enabled, words: words)
            return
        }
        await MaskTransition.run(
            begin: { maskTransition = true },
            dropData: { dropMaskedData() },
            applyMask: { storePrivacyMask(enabled, words: words) },
            reread: {
                await model?.reparse()
                model?.dataGeneration += 1
                // Lets the screens under the placeholder drop what they hold (their onChange and
                // .task(id:) on dataGeneration) before it goes, so not one frame shows old names.
                try? await Task.sleep(for: .milliseconds(150))
            },
            end: { maskTransition = false }
        )
        // Not awaited under the placeholder: the notification center may answer slowly.
        if enabled { Task { await BackgroundMonitor.removeDeliveredAlerts() } }
    }

    /// What was kept under the previous names, dropped before Go's mask changes.
    private func dropMaskedData() {
        // Also resets the alert diff, which would otherwise see every node renamed.
        SharedStore.clear()
        LastKnownStore.wipe()
        // Its poll stops recording: the overview starts a new one with the new generation.
        ClusterLiveStore.shared.forget()
        SearchRecents.clear()
        RecentTargetStore.shared.clear()
        model?.nodes.forget()
    }

    private func storePrivacyMask(_ enabled: Bool, words: String) {
        privacyMask = enabled
        privacyWords = words
        UserDefaults.standard.set(enabled, forKey: PrivacyKeys.enabled)
        UserDefaults.standard.set(words, forKey: PrivacyKeys.words)
        TalosClient.setPrivacyMask(enabled: enabled, extraWords: words)
        model?.updateSpotlight()
    }
}
