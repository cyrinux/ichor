import Foundation
import Ichorgo

/// The cluster's secrets bundle (see TalosClient for the conventions).
extension TalosClient {
    /// The cluster's secrets.yaml rebuilt from control plane `node`'s config (os:admin), for
    /// `talosctl gen config --with-secrets`. The cluster's root of trust: keep it in memory only.
    func secretsBundle(node: String) async throws -> String {
        try await Self.run { [config, context] in IchorgoClusterSecretsBundle(config, context, node, $0) }
    }
}
