import Foundation
import Observation
import IchorCore

// The app lock and its security keys, and what the app's lifecycle tells the lock.
extension AppModel {
    /// Callers must have authenticated the user first; the lock off drops the enrolled keys too.
    func setLockEnabled(_ enabled: Bool) {
        lock.setEnabled(enabled)
        UserDefaults.standard.set(enabled, forKey: Keys.lock)
        if !enabled { setSecurityKeys(nil) }
        updateSpotlight()
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
