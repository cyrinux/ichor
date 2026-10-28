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
}
