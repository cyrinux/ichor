import Foundation
import Ichorgo
import IchorCore

/// Creating a VM at Azure for a new node (see TalosClient for the conventions).
extension TalosClient {
    /// Whose service principal this is and whether it may create VMs (its permissions, read
    /// only). Creates nothing.
    static func azureTest(credential: String) async throws -> AzureCheck {
        try await json { IchorgoAzureTest(credential, $0) }
    }

    /// Resource groups, locations, sizes, networks, security groups and images (`credential` may
    /// be empty on the demo). Never cached.
    func azureOptions(credential: String) async throws -> AzureOptions {
        try await Self.json { [config, context] in
            IchorgoAzureOptions(config, context, credential, $0)
        }
    }

    /// The custom role to give the principal on `resourceGroup`: only the calls Ichor makes.
    static func azureRole(subscription: String, resourceGroup: String) -> String {
        IchorgoAzureRoleDefinition(subscription, resourceGroup)
    }

    /// The hourly price of `size` in `location`; an empty `hourly` when unknown.
    static func azurePrice(credential: String, location: String, size: String) async throws -> AzurePrice {
        try await json { IchorgoAzurePrice(credential, location, size, $0) }
    }

    /// Creates the VM with the new node's custom data and follows it (StartAzureCreate). As for
    /// Hetzner, only `stop` stops it: it deletes a billable VM.
    func startAzureCreate(
        _ spec: AzureCreateSpec, credential: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return hetznerRun { [config, context] bridge in
            IchorgoStartAzureCreate(config, context, credential, specJSON, bridge)
        }
    }

    /// The cost line of an image run. Creates nothing.
    static func azureImageEstimate(credential: String, arch: String, location: String) async throws
        -> AzureImageEstimate
    {
        try await json { IchorgoAzureImageEstimate(credential, arch, location, $0) }
    }

    /// Makes a Talos managed image with a throwaway VM (StartAzureImage): `done`'s address is the
    /// result JSON. `stop` deletes the helper, its data disk and an unfinished image, and so only
    /// `stop` stops it, as above.
    func startAzureImage(
        _ spec: AzureImageSpec, credential: String
    ) -> (events: AsyncStream<JoinEvent>, stop: @Sendable () -> Void) {
        let specJSON = (try? JSONEncoder().encode(spec)).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        return hetznerRun { [config, context] bridge in
            IchorgoStartAzureImage(config, context, credential, specJSON, bridge)
        }
    }
}
