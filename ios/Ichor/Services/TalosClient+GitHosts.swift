import Foundation
import Ichorgo
import IchorCore

/// Git host calls (see TalosClient for the conventions). Static: no cluster involved.
extension TalosClient {
    /// Whether the token in `hostJSON` (gitHostJSON, or gitHostDemoJSON in the demo) works,
    /// and who it signs in as. The token is only inside that string, never logged.
    static func gitHostCheck(_ hostJSON: String) async throws -> GitHostCheck {
        try await json { IchorgoGitForgeCheck(hostJSON, $0) }
    }
}
