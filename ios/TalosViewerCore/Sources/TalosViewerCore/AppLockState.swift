import Foundation

/// App-lock state machine (same rules as Android): locked at cold start and after
/// `grace` seconds in the background, so short trips (file picker, camera) don't relock.
public struct AppLockState: Equatable, Sendable {
    public private(set) var enabled: Bool
    public private(set) var locked: Bool
    public let grace: TimeInterval
    private var backgroundedAt: TimeInterval?

    public init(enabled: Bool, grace: TimeInterval = 30) {
        self.enabled = enabled
        self.locked = enabled
        self.grace = grace
    }

    /// Callers must have authenticated the user before changing this.
    public mutating func setEnabled(_ value: Bool) {
        enabled = value
        if !value { locked = false }
    }

    public mutating func unlock() { locked = false }

    public mutating func onBackground(now: TimeInterval) { backgroundedAt = now }

    public mutating func onForeground(now: TimeInterval) {
        guard let since = backgroundedAt else { return }
        backgroundedAt = nil
        if enabled && now - since >= grace { locked = true }
    }
}
