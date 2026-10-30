import Foundation

/// Runs short synchronous work (parsing, encoding, a Go call that returns at once) away from the
/// caller's actor: a nonisolated async function does not run on the main actor (SE-0338). It
/// replaces `Task.detached`, so the caller's priority and cancellation carry over. Go calls that
/// wait on the network go through `TalosClient.run` instead, which does not hold a cooperative
/// thread.
func offMainActor<T: Sendable>(_ work: @Sendable () throws -> T) async rethrows -> T {
    try work()
}
