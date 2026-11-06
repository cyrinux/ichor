import Foundation
import Ichorgo
import IchorCore

/// Creating a Compute Engine VM at Google Cloud for a new node (see TalosClient for the conventions).
extension TalosClient {
    /// Whose service account key this is and which of the printed role's permissions it lacks.
    /// Creates nothing.
    static func gcpTest(credential: String) async throws -> GCPCheck {
        try await json { IchorgoGCPTest(credential, $0) }
    }

    /// Projects, zones, and the zone's machine types, networks and images (`credential` may be
    /// empty on the demo). Never cached.
    func gcpOptions(credential: String) async throws -> GCPOptions {
        try await Self.json { [config, context] in
            IchorgoGCPOptions(config, context, credential, $0)
        }
    }

    /// The custom role to create in `project` and grant the service account: only the calls Ichor makes.
    static func gcpRole(project: String) -> String {
        IchorgoGCPRoleDefinition(project)
    }

    /// The hourly price of `machineType` in `zone`; an empty `hourly` when unknown.
    static func gcpPrice(credential: String, zone: String, machineType: String) async throws -> GCPPrice {
        try await json { IchorgoGCPPrice(credential, zone, machineType, $0) }
    }

    /// Creates the VM with the new node's user data and follows it (StartGCPCreate). As for
    /// Hetzner, only `stop` stops it: it deletes a billable VM.
    func startGCPCreate(
        _ spec: GCPCreateSpec, credential: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return hetznerRun { [config, context] bridge in
            IchorgoStartGCPCreate(config, context, credential, specJSON, bridge)
        }
    }
}
