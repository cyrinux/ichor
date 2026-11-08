import Foundation

/// A value behind a lock, shared across tasks and threads (Go calls back on its own threads).
///
/// The one `@unchecked Sendable` of IchorCore, and why it is sound: `value` is private and
/// every read and write of it holds `lock`, so two threads never touch it at once. It stands in
/// for `Synchronization.Mutex`, which needs iOS 18 (the package targets iOS 17). Do not let
/// `body` keep the `inout` reference past its return. New shared mutable state goes through
/// this type rather than a new `@unchecked Sendable` (CONTRIBUTING.md).
public final class Locked<Value>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: Value

    public init(_ value: Value) {
        self.value = value
    }

    /// Runs `body` with the value, the lock held for the whole call.
    public func withLock<Result>(_ body: (inout Value) throws -> Result) rethrows -> Result {
        lock.lock()
        defer { lock.unlock() }
        return try body(&value)
    }

    /// A copy of the value.
    public func get() -> Value {
        withLock { $0 }
    }

    /// Replaces the value.
    public func set(_ newValue: Value) {
        withLock { $0 = newValue }
    }
}
