import Foundation
import Network

/// The device's network changing (Wi-Fi, cellular, a VPN coming or going), so a cluster that
/// did not answer is tried again at once instead of waiting for a pull to refresh.
enum NetworkChanges {
    /// One element per change after the current path; ends when the consuming task is cancelled.
    static func stream() -> AsyncDropFirstSequence<AsyncStream<Void>> {
        // Only the latest change matters: a VPN connecting reports several paths in a burst.
        AsyncStream(bufferingPolicy: .bufferingNewest(1)) { continuation in
            let monitor = NWPathMonitor()
            monitor.pathUpdateHandler = { _ in continuation.yield() }
            continuation.onTermination = { _ in monitor.cancel() }
            monitor.start(queue: DispatchQueue(label: "name.levis.ichor.network-changes"))
        }
        // The monitor reports the current path at start: only later ones are changes.
        .dropFirst()
    }
}
