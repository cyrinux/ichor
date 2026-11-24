import Foundation
import Ichorgo
import IchorCore

/// Creating an Instance at Scaleway for a new node (see TalosClient for the conventions).
extension TalosClient {
    /// Whose API key this is and whether it may create Instances. Creates nothing.
    static func scalewayTest(credential: String) async throws -> ScalewayCheck {
        try await json { IchorgoScalewayTest(credential, $0) }
    }

    /// Zones, types, images and security groups of the key's zone (`credential` may be empty on
    /// the demo). Never cached.
    func scalewayOptions(credential: String) async throws -> ScalewayOptions {
        try await Self.json { [config, context] in
            IchorgoScalewayOptions(config, context, credential, $0)
        }
    }

    /// Finds or makes the guided security group ichor-nodes in the spec's zone, opened to nothing
    /// but the cluster's members and (plain Talos) this phone on port 50000. Writes at Scaleway;
    /// refused in the demo.
    func scalewayNetworkPrepare(credential: String, spec: ScalewayNetworkSpec) async throws -> ScalewayNetwork {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return try await Self.json { [config, context] in
            IchorgoScalewayNetworkPrepare(config, context, credential, specJSON, $0)
        }
    }

    /// The IAM policy to attach to the key's application, on `project` when known.
    static func scalewayPolicy(project: String) -> String {
        IchorgoScalewayPolicy(project)
    }

    /// Creates the Instance with the new node's user data and follows it (StartScalewayCreate). As
    /// for Hetzner, only `stop` stops it: it deletes a billable Instance and its volumes.
    func startScalewayCreate(
        _ spec: ScalewayCreateSpec, credential: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return hetznerRun { [config, context] bridge in
            IchorgoStartScalewayCreate(config, context, credential, specJSON, bridge)
        }
    }
}
