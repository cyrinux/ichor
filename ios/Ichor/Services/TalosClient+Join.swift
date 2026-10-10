import Foundation
import Ichorgo
import IchorCore

/// Adding a node to the cluster (see TalosClient for the conventions).
extension TalosClient {
    /// A node in maintenance mode at `address` (IP or host name on the LAN or VPN): version, disks
    /// and links over the unauthenticated maintenance API. Static: the node belongs to no cluster
    /// yet, so no talosconfig is involved.
    static func maintenanceNodeInspect(address: String) async throws -> MaintenanceInspection {
        try await json { IchorgoMaintenanceNodeInspect(address, 0, $0) }
    }
}
