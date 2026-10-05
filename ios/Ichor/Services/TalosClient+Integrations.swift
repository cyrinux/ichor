import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// The projects the app integrates with, without asking any cluster.
    static func integrations() -> Integrations {
        var error: NSError?
        let json = IchorgoIntegrations(&error)
        return (try? TalosJSON.decode(Integrations.self, from: json)) ?? Integrations()
    }

    /// The same list with whether the cluster runs each one, from one API discovery (os:admin).
    /// hints: see integrationHints.
    func integrations(hints: String) async throws -> Integrations {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeIntegrations(config, context, kubeServer, hints, $0) }
    }
}
