import Foundation

/// A gomobile object (a run, a session), which Swift imports as a non-Sendable class.
///
/// `@unchecked Sendable`, and why it is sound: the Swift side holds only a reference number and
/// has no state of its own, and every method is a call into Go, whose objects guard their own
/// state (runs cancel through a context, sessions hold a mutex). Go's own goroutines already call
/// them concurrently. This is the one place that says so, rather than one class per Go type.
struct GoObject<Object: AnyObject>: @unchecked Sendable {
    let value: Object

    init(_ value: Object) {
        self.value = value
    }
}

/// What a stream fed by a Go listener holds until it ends: the listener (Go only keeps a weak
/// hold on it) and how to cancel the run.
///
/// `@unchecked Sendable`, and why it is sound: both properties are immutable, and `cancelRun` is
/// only ever a gomobile `cancel()` or `stop()` (see GoObject), which any thread may call.
/// Requiring `@Sendable` of it would make every caller box its run for nothing.
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

    /// `bridged` for a run that ends with an error: the listener finishes the stream throwing it.
    static func bridgedThrowing<Event>(
        _ start: (AsyncThrowingStream<Event, Error>.Continuation) -> BridgedRun
    ) -> AsyncThrowingStream<Event, Error> {
        AsyncThrowingStream { continuation in
            let run = start(continuation)
            continuation.onTermination = { _ in run.cancel() }
        }
    }
}
