import Foundation
import Ichorgo
import IchorCore

/// Creating an instance at AWS EC2 for a new node (see TalosClient for the conventions).
extension TalosClient {
    /// Whose keys these are and whether they may run instances (a dry run). Creates nothing.
    static func awsTest(credential: String) async throws -> AWSCheck {
        try await json { IchorgoAWSTest(credential, $0) }
    }

    /// Regions, instance types, subnets, security groups and images (`credential` may be empty on
    /// the demo). Never cached.
    func awsOptions(credential: String) async throws -> AWSOptions {
        try await Self.json { [config, context] in
            IchorgoAWSOptions(config, context, credential, $0)
        }
    }

    /// The IAM policy to give the keys' user: only the calls Ichor makes.
    static func awsPolicy() -> String {
        IchorgoAWSIAMPolicy()
    }

    /// The hourly price of `instanceType` in the keys' region; an empty `hourly` when unknown.
    static func awsPrice(credential: String, instanceType: String) async throws -> AWSPrice {
        try await json { IchorgoAWSPrice(credential, instanceType, $0) }
    }

    /// Creates the instance with the new node's user data and follows it (StartAWSCreate). As for
    /// Hetzner, only `stop` stops it: it terminates a billable instance.
    func startAWSCreate(
        _ spec: AWSCreateSpec, credential: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return hetznerRun { [config, context] bridge in
            IchorgoStartAWSCreate(config, context, credential, specJSON, bridge)
        }
    }
}
