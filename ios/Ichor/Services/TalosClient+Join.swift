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

    /// A new node's config from `template`'s (os:admin: read with its secrets), redacted. The
    /// role must be the template's; `staticAddress` is a prefix or "" for DHCP.
    func joinNodeConfigPreview(template: String, role: String, hostname: String, installDisk: String,
                               staticAddress: String) async throws -> JoinConfigPreview {
        try await Self.json { [config, context] in
            IchorgoJoinNodeConfigPreview(config, context, template, role, hostname, installDisk, staticAddress, "", $0)
        }
    }
}
