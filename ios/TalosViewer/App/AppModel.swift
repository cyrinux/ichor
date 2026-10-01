import Foundation
import Observation
import TalosViewerCore

enum ThemeMode: String, CaseIterable, Identifiable {
    case auto, light, dark, black

    var id: String { rawValue }
    var label: String { rawValue.capitalized }
}

/// App-wide state: the imported config, the active context, preferences and the lock.
@Observable
@MainActor
final class AppModel {
    private enum Keys {
        static let context = "activeContext"
        static let theme = "themeMode"
        static let lock = "appLockEnabled"
    }

    private(set) var yaml: String?
    private(set) var summary: ConfigSummary?
    private(set) var loaded = false
    private(set) var lock: AppLockState

    var activeContext = "" {
        didSet { UserDefaults.standard.set(activeContext, forKey: Keys.context) }
    }

    var theme: ThemeMode {
        didSet { UserDefaults.standard.set(theme.rawValue, forKey: Keys.theme) }
    }

    init() {
        theme = ThemeMode(rawValue: UserDefaults.standard.string(forKey: Keys.theme) ?? "") ?? .auto
        lock = AppLockState(enabled: UserDefaults.standard.bool(forKey: Keys.lock))
    }

    var client: TalosClient? {
        yaml.map { TalosClient(config: $0, context: activeContext) }
    }

    var activeSummary: ContextSummary? { summary?.context(named: activeContext) }

    /// Privileged actions are only shown when the imported config's role allows them.
    func allows(_ feature: Feature) -> Bool { activeSummary?.allows(feature) ?? false }

    /// Loads the stored config (Keychain); nothing is read before the first unlock.
    func load() async {
        defer { loaded = true }
        guard let data = SecureConfigStore.load(), let stored = String(data: data, encoding: .utf8),
              let parsed = try? await TalosClient.parse(stored) else { return }
        apply(yaml: stored, summary: parsed, preferred: UserDefaults.standard.string(forKey: Keys.context))
    }

    func save(yaml newYAML: String) async throws {
        let parsed = try await TalosClient.parse(newYAML)
        try SecureConfigStore.save(Data(newYAML.utf8))
        apply(yaml: newYAML, summary: parsed, preferred: parsed.current)
    }

    func clear() {
        SecureConfigStore.delete()
        yaml = nil
        summary = nil
    }

    /// Callers must have authenticated the user first.
    func setLockEnabled(_ enabled: Bool) {
        lock.setEnabled(enabled)
        UserDefaults.standard.set(enabled, forKey: Keys.lock)
    }

    func unlock() { lock.unlock() }
    func didEnterBackground() { lock.onBackground(now: Self.monotonicNow()) }
    func willEnterForeground() { lock.onForeground(now: Self.monotonicNow()) }

    /// Monotonic seconds that keep counting while the device sleeps (systemUptime does not,
    /// so a locked phone left for hours would not relock the app); immune to clock changes.
    private static func monotonicNow() -> TimeInterval {
        TimeInterval(clock_gettime_nsec_np(CLOCK_MONOTONIC)) / 1_000_000_000
    }

    private func apply(yaml newYAML: String, summary newSummary: ConfigSummary, preferred: String?) {
        yaml = newYAML
        summary = newSummary
        activeContext = preferred.flatMap { newSummary.context(named: $0)?.name } ?? newSummary.current
    }
}
