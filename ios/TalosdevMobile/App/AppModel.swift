import Foundation
import Observation
import TalosdevMobileCore

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

/// App-wide state: the imported config, the active context, preferences and the lock.
@Observable
@MainActor
final class AppModel {
    private enum Keys {
        static let context = "activeContext"
        /// Position of the active context in the config: unlike the name, it survives the
        /// screenshot mode (Go masks context names but keeps their order).
        static let contextIndex = "activeContextIndex"
        static let theme = "themeMode"
        static let lock = "appLockEnabled"
    }

    private(set) var yaml: String?
    private(set) var summary: ConfigSummary?
    private(set) var loaded = false
    private(set) var lock: AppLockState

    var activeContext = "" {
        didSet {
            if activeContext != oldValue { forgetFeatures() }
            UserDefaults.standard.set(activeContext, forKey: Keys.context)
            if let index = summary?.contexts.firstIndex(where: { $0.name == activeContext }) {
                UserDefaults.standard.set(index, forKey: Keys.contextIndex)
            }
        }
    }

    var theme: ThemeMode {
        didSet { UserDefaults.standard.set(theme.rawValue, forKey: Keys.theme) }
    }

    /// Screenshot mode (masking done in Go, see TalosClient.setPrivacyMask) and its extra words.
    private(set) var privacyMask: Bool
    private(set) var privacyWords: String
    /// Bumped when the screenshot mode changes, so screens reload instead of showing old data.
    private(set) var dataGeneration = 0

    /// What each node's Talos version can do (Go NodeFeatures), by node address; loaded once
    /// per node and Talos version, see loadFeatures.
    private(set) var nodeFeatures: [String: NodeFeatures] = [:]
    @ObservationIgnored private var featuresGeneration = 0

    init() {
        theme = ThemeMode(rawValue: UserDefaults.standard.string(forKey: Keys.theme) ?? "") ?? .auto
        privacyMask = UserDefaults.standard.bool(forKey: PrivacyKeys.enabled)
        privacyWords = UserDefaults.standard.string(forKey: PrivacyKeys.words) ?? ""
        lock = AppLockState(enabled: UserDefaults.standard.bool(forKey: Keys.lock))
    }

    var client: TalosClient? {
        yaml.map { TalosClient(config: $0, context: activeContext) }
    }

    var activeSummary: ContextSummary? { summary?.context(named: activeContext) }

    /// Privileged actions are only shown when the imported config's role allows them.
    func allows(_ feature: Feature) -> Bool { activeSummary?.allows(feature) ?? false }

    /// Whether node's Talos version has `feature`; supported while its features are unknown.
    func support(_ feature: NodeFeature, node: String) -> FeatureSupport {
        featureSupport(nodeFeatures[node], feature)
    }

    /// Support of a cluster-wide feature across the nodes whose features are known.
    func clusterSupport(_ feature: NodeFeature) -> FeatureSupport {
        TalosdevMobileCore.clusterSupport(Array(nodeFeatures.values), feature)
    }

    /// Loads node's features unless they are cached for `version` (the node's Talos version
    /// when the caller knows it: an upgraded node is asked again). Failures are silent: an
    /// unknown node counts as supporting everything and answers for itself.
    func loadFeatures(node: String, version: String = "") async {
        if let cached = nodeFeatures[node], version.isEmpty || cached.version.isEmpty
            || compareTalosVersions(cached.version, version) == 0 { return }
        guard let client else { return }
        let generation = featuresGeneration
        guard let loaded = try? await client.features(node: node), generation == featuresGeneration else { return }
        nodeFeatures[node] = loaded
    }

    /// Drops node's cached features and asks again: its Talos version just changed (upgrade).
    func reloadFeatures(node: String) async {
        nodeFeatures[node] = nil
        await loadFeatures(node: node)
    }

    /// The features of every reachable node of an overview, in parallel.
    func loadFeatures(of nodes: [NodeOverview]) async {
        await withTaskGroup(of: Void.self) { group in
            for node in nodes where node.reachable {
                group.addTask { await self.loadFeatures(node: node.node, version: node.version) }
            }
        }
    }

    /// Other context, other config or other (masked) node names: the cache no longer applies.
    private func forgetFeatures() {
        featuresGeneration += 1
        nodeFeatures = [:]
    }

    /// Loads the stored config (Keychain); nothing is read before the first unlock.
    func load() async {
        defer { loaded = true }
        guard let data = SecureConfigStore.load(), let stored = String(data: data, encoding: .utf8),
              let parsed = try? await TalosClient.parse(stored) else { return }
        apply(yaml: stored, summary: parsed, preferred: parsed.selectedContext(index: Self.savedContextIndex, name: UserDefaults.standard.string(forKey: Keys.context)))
    }

    func save(yaml newYAML: String) async throws {
        let parsed = try await TalosClient.parse(newYAML)
        try SecureConfigStore.save(Data(newYAML.utf8))
        apply(yaml: newYAML, summary: parsed, preferred: parsed.current)
    }

    /// Replaces the active context's ca/crt/key with those of `generated` (a renewed
    /// single-context talosconfig), keeping the other contexts and the active context. The
    /// result must parse, keep the same contexts and carry exactly the new certificate.
    func renewCredentials(with generated: String) async throws {
        guard let current = yaml, let before = summary else { throw TalosError(message: String(localized: "No talosconfig is stored.")) }
        let context = activeContext
        let patched = try await TalosClient.replaceContextCredentials(stored: current, generated: generated, context: context)
        let parsed = try await TalosClient.parse(patched)
        let issued = try await TalosClient.parse(generated)
        guard let renewed = parsed.context(named: context), let fresh = issued.context(named: context),
              renewed.certNotAfter == fresh.certNotAfter, Set(renewed.roles) == Set(fresh.roles),
              parsed.contexts.map(\.name) == before.contexts.map(\.name) else {
            throw TalosError(message: String(localized: "The renewed talosconfig did not validate; the stored one is unchanged."))
        }
        try SecureConfigStore.save(Data(patched.utf8))
        apply(yaml: patched, summary: parsed, preferred: context)
    }

    func clear() {
        SecureConfigStore.delete()
        SharedStore.save(nil) // the widget stops showing the old cluster
        forgetFeatures()
        yaml = nil
        summary = nil
    }

    /// Callers must have authenticated the user first.
    func setLockEnabled(_ enabled: Bool) {
        lock.setEnabled(enabled)
        UserDefaults.standard.set(enabled, forKey: Keys.lock)
    }

    /// Turns the screenshot mode on or off (or changes its words): drops the data held with
    /// the previous names (widget snapshot, loaded screens) and re-reads the config, whose
    /// context names are masked too.
    func setPrivacyMask(_ enabled: Bool, words: String) async {
        guard enabled != privacyMask || words != privacyWords else { return }
        let affectsData = enabled || privacyMask
        privacyMask = enabled
        privacyWords = words
        UserDefaults.standard.set(enabled, forKey: PrivacyKeys.enabled)
        UserDefaults.standard.set(words, forKey: PrivacyKeys.words)
        TalosClient.setPrivacyMask(enabled: enabled, extraWords: words)
        guard affectsData else { return }
        // Also resets the alert diff, which would otherwise see every node renamed.
        SharedStore.save(nil)
        forgetFeatures()
        await reparse()
        dataGeneration += 1
    }

    func unlock() { lock.unlock() }
    func didEnterBackground() { lock.onBackground(now: Self.monotonicNow()) }
    func willEnterForeground() { lock.onForeground(now: Self.monotonicNow()) }

    /// Monotonic seconds that keep counting while the device sleeps (systemUptime does not,
    /// so a locked phone left for hours would not relock the app); immune to clock changes.
    private static func monotonicNow() -> TimeInterval {
        TimeInterval(clock_gettime_nsec_np(CLOCK_MONOTONIC)) / 1_000_000_000
    }

    /// The saved context position; nil for installs from before it was saved.
    nonisolated static var savedContextIndex: Int? {
        UserDefaults.standard.object(forKey: Keys.contextIndex) as? Int
    }

    /// Parses the stored config again, keeping the active context by position (its name
    /// changes when masking is toggled).
    private func reparse() async {
        guard let current = yaml, let before = summary, let parsed = try? await TalosClient.parse(current) else { return }
        let index = before.contexts.firstIndex { $0.name == activeContext }
        apply(yaml: current, summary: parsed, preferred: parsed.selectedContext(index: index, name: nil))
    }

    private func apply(yaml newYAML: String, summary newSummary: ConfigSummary, preferred: String?) {
        forgetFeatures()
        yaml = newYAML
        summary = newSummary
        activeContext = preferred.flatMap { newSummary.context(named: $0)?.name } ?? newSummary.current
    }
}
