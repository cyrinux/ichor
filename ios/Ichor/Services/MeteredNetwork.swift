import Foundation
import Network

/// Whether the device's network is expensive (cellular, a personal hotspot) or constrained
/// (Low Data Mode): large Kubernetes lists then load fewer rows eagerly (eagerCap).
final class MeteredNetwork: @unchecked Sendable {
    static let shared = MeteredNetwork()

    private let monitor = NWPathMonitor()

    private init() {
        monitor.start(queue: DispatchQueue(label: "name.levis.ichor.metered-network"))
    }

    var isMetered: Bool {
        let path = monitor.currentPath
        return path.isExpensive || path.isConstrained
    }
}
