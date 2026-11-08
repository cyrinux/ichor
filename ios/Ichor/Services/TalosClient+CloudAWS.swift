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

    /// Finds or makes the guided network in the default VPC: a public subnet and the ichor-nodes
    /// group, opened to nothing but the group itself and (plain Talos) this phone on port 50000.
    /// Writes at AWS; refused in the demo and without a default VPC.
    func awsNetworkPrepare(credential: String, spec: AWSNetworkSpec) async throws -> AWSNetwork {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return try await Self.json { [config, context] in
            IchorgoAWSNetworkPrepare(config, context, credential, specJSON, $0)
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

    /// Starts an IAM Identity Center sign-in: the device code to approve.
    static func awsSignInStart(request: String) async throws -> AWSSignInStart {
        try await json { IchorgoAWSSignInStart(request, $0) }
    }

    /// Waits for the code to be approved (minutes; ended by `awsSignInCancel`), then lists the roles.
    static func awsSignInFinish(credential: String) async throws -> AWSSignInFinish {
        try await json { IchorgoAWSSignInFinish(credential, $0) }
    }

    static func awsSignInCancel() {
        IchorgoAWSSignInCancel()
    }

    /// The credentials of one account and role of the list.
    static func awsSignInChoose(credential: String, role: KubeSsoRoleChoice) async throws -> AWSSignInChoice {
        try await json { IchorgoAWSSignInChoose(credential, role.accountId, role.roleName, $0) }
    }

    /// Role credentials that last a run; "" when the stored credential is good as it is.
    static func awsRenew(credential: String) async throws -> AWSRenewed {
        try await json { IchorgoAWSRenew(credential, $0) }
    }

    /// The cost line of an AMI run. Creates nothing.
    static func awsImageEstimate(credential: String, arch: String, region: String) async throws -> AWSImageCost {
        try await json { IchorgoAWSImageEstimate(credential, arch, region, $0) }
    }

    /// Makes a Talos AMI with a throwaway instance (StartAWSImage): `done`'s address is the result
    /// JSON. `stop` terminates the helper and deletes an unfinished snapshot or AMI, and so only
    /// `stop` stops it, as above.
    func startAWSImage(
        _ spec: AWSImageSpec, credential: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return hetznerRun { [config, context] bridge in
            IchorgoStartAWSImage(config, context, credential, specJSON, bridge)
        }
    }
}
