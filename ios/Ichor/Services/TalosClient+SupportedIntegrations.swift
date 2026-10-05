import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// The projects the app integrates with, without asking any cluster.
    static func supportedIntegrations() -> SupportedIntegrations {
        var error: NSError?
        let json = IchorgoSupportedIntegrations(&error)
        return (try? TalosJSON.decode(SupportedIntegrations.self, from: json)) ?? SupportedIntegrations()
    }

    /// The same list with whether the cluster runs each one, from one API discovery (os:admin).
    /// hints: see supportedIntegrationHints.
    func supportedIntegrations(hints: String) async throws -> SupportedIntegrations {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeSupportedIntegrations(config, context, kubeServer, hints, $0) }
    }
}
