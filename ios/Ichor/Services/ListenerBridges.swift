import Foundation

/// What a stream fed by a Go listener holds until it ends: the listener (Go only keeps a weak
/// hold on it) and how to cancel the run.
final class BridgedRun: @unchecked Sendable {
    private let bridge: AnyObject
    private let cancelRun: () -> Void

    init(_ bridge: AnyObject, cancel: @escaping () -> Void) {
        self.bridge = bridge
        cancelRun = cancel
    }

    func cancel() {
        cancelRun()
    }
}

extension TalosClient {
    /// A stream of what a Go run reports through its listener. `start` creates the listener,
    /// starts the run and returns both; ending the stream (its consumer is cancelled, or the
    /// listener finished it) cancels the run, and the listener lives as long as the stream.
    static func bridged<Event>(
        buffering: AsyncStream<Event>.Continuation.BufferingPolicy = .unbounded,
        _ start: (AsyncStream<Event>.Continuation) -> BridgedRun
    ) -> AsyncStream<Event> {
        AsyncStream(bufferingPolicy: buffering) { continuation in
            let run = start(continuation)
            continuation.onTermination = { _ in run.cancel() }
        }
    }
}
