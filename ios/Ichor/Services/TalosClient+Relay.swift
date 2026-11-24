import Foundation
import Ichorgo
import IchorCore

/// The in-cluster ichor-relay, reached through the Kubernetes API service proxy (see TalosClient
/// for the conventions): Go reads the pairing token from the relay's Secret with this config's
/// own rights and never hands it over. Pairing is refused in the demo and recorded in the audit log.
extension TalosClient {
    /// The relays among the cluster's Services, and whether this config may pair with them.
    func relayDiscover() async throws -> RelayDiscovery {
        try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoRelayDiscover(config, context, kubeServer, $0)
        }
    }

    /// Registers `device` with the relay at `source`; the device's id, the same when pairing again.
    func relayRegister(_ source: PromSource, device: RelayDevice) async throws -> String {
        let sourceJSON = try TalosJSON.encode(source)
        let deviceJSON = try TalosJSON.encode(device)
        let paired: RelayRegistered = try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoRelayRegister(config, context, kubeServer, sourceJSON, deviceJSON, $0)
        }
        return paired.id
    }

    /// Removes the device `id` from the relay at `source`.
    func relayUnregister(_ source: PromSource, id: String) async throws {
        let sourceJSON = try TalosJSON.encode(source)
        try await Self.run {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoRelayUnregister(config, context, kubeServer, sourceJSON, id, error)
        }
    }

    /// Tells the relay at `source` the quiet hours of the device `id` (`.off` clears them); false
    /// when the relay is too old to hold any.
    func relaySetQuiet(_ source: PromSource, id: String, quiet: QuietHours) async throws -> Bool {
        let sourceJSON = try TalosJSON.encode(source)
        let quietJSON = try TalosJSON.encode(quiet)
        let answer: RelayQuietAnswer = try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoRelaySetQuiet(config, context, kubeServer, sourceJSON, id, quietJSON, $0)
        }
        return answer.applied
    }
}

/// RelaySetQuiet's answer.
private struct RelayQuietAnswer: Decodable {
    let applied: Bool
}

/// RelayRegister's answer.
private struct RelayRegistered: Decodable {
    let id: String
}
