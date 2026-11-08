import Foundation
import Observation
import IchorCore

/// The app lock and its security keys, and what the app's lifecycle tells the lock. The rules are
/// AppLockState's; this store keeps its state and the setting, whichever cluster is on screen.
@Observable
@MainActor
final class LockStore {
    private enum Keys {
        static let enabled = "appLockEnabled"
    }

    /// What a lock change has to reach: Spotlight, which indexes nothing with the lock on. Set
    /// once by AppModel.init; weak, the model owns this store.
    @ObservationIgnored weak var model: AppModel?

    private(set) var lock: AppLockState
    /// The enrolled security keys (SecurityKeyStore), nil when none; see IchorCore/SecurityKeys.swift.
    private(set) var securityKeys: SecurityKeyEnrolment? = SecurityKeyStore.current
    /// Whether a tap of a security key is needed to read the stored configs (Face ID alone is refused).
    var requiresKey: Bool { securityKeys?.required == true }

    init() {
        lock = AppLockState(enabled: UserDefaults.standard.bool(forKey: Keys.enabled))
    }

    /// Callers must have authenticated the user first; the lock off drops the enrolled keys too.
    func setLockEnabled(_ enabled: Bool) {
        lock.setEnabled(enabled)
        UserDefaults.standard.set(enabled, forKey: Keys.enabled)
        if !enabled { setSecurityKeys(nil) }
        model?.updateSpotlight()
    }

    /// Callers must have authenticated the user first. No key left forgets the data key too.
    func setSecurityKeys(_ enrolment: SecurityKeyEnrolment?) {
        let value = enrolment.flatMap { $0.keys.isEmpty ? nil : $0 }
        SecurityKeyStore.current = value
        securityKeys = value
        if value == nil { SecurityKeySession.shared.clear() }
    }

    func unlock() { lock.unlock() }
    func didEnterBackground() { lock.onBackground(now: Self.monotonicNow()) }
    func willEnterForeground() { lock.onForeground(now: Self.monotonicNow()) }

    /// Monotonic seconds that keep counting while the device sleeps (systemUptime does not,
    /// so a locked phone left for hours would not relock the app); immune to clock changes.
    private static func monotonicNow() -> TimeInterval {
        TimeInterval(clock_gettime_nsec_np(CLOCK_MONOTONIC)) / 1_000_000_000
    }
}
