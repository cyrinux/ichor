import Foundation
import Ichorgo
import IchorCore

/// What boots a new machine of the cluster (see TalosClient for the conventions).
extension TalosClient {
    /// `template`'s (or the first member's, "") version and schematic, the Image Factory links
    /// and the booter command, for `arch` ("" for the template's). Never cached: on Omni it
    /// carries the join token.
    func nodeBootMedia(template: String, arch: String, secureBoot: Bool) async throws -> BootMedia {
        try await Self.json { [config, context] in
            IchorgoNodeBootMedia(config, context, template, arch, secureBoot, $0)
        }
    }

    /// The user data of a new node on a cloud host and the Talos cloud images to create it from:
    /// Omni's join config on Omni (the other arguments are not used), else a new member's full
    /// config made from `template`'s. Never cached: it carries a credential.
    func nodeCloudUserData(template: String, role: String, hostname: String, installDisk: String) async throws
        -> CloudUserData
    {
        try await Self.json { [config, context] in
            IchorgoNodeCloudUserData(config, context, template, role, hostname, installDisk, "", $0)
        }
    }
}
