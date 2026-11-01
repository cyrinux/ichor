import Foundation
import Ichorgo
import IchorCore

/// Creating an instance at OVHcloud for a new node (see TalosClient for the conventions).
extension TalosClient {
    /// What a service account sees and may do. Creates nothing.
    static func ovhTest(credential: String) async throws -> OVHCheck {
        try await json { IchorgoOVHTest(credential, $0) }
    }

    /// Regions, flavors and images (`credential` may be empty on the demo). Never cached.
    func ovhOptions(credential: String) async throws -> OVHOptions {
        try await Self.json { [config, context] in
            IchorgoOVHOptions(config, context, credential, $0)
        }
    }

    /// The IAM policy to give the service account, on `project` when known.
    static func ovhPolicy(project: String) -> String {
        IchorgoOVHServiceAccountPolicy(project)
    }

    /// Creates the instance with the new node's user data and follows it (StartOVHCreate). As for
    /// Hetzner, only `stop` stops it: it deletes a billable instance.
    func startOVHCreate(
        _ spec: OVHCreateSpec, credential: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return hetznerRun { [config, context] bridge in
            IchorgoStartOVHCreate(config, context, credential, specJSON, bridge)
        }
    }
}
