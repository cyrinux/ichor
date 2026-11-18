import Foundation
import Ichorgo
import IchorCore

/// The SSH bastion of a cluster added from a kubeconfig (see TalosClient for the conventions).
/// `kube` is the stored kubeconfig, `context` a context of it. The core keeps the setting, key
/// included, in the sealed auth store; the app only ever sees the fingerprint.
extension TalosClient {
    /// The bastion of `context`, or why it cannot have one.
    static func bastionInfo(kube: String, context: String) async throws -> KubeBastionInfo {
        let json = try await run { IchorgoKubeBastionInfo(kube, context, $0) }
        return try KubeBastionInfo.decode(json)
    }

    /// The host key the bastion at `host` (host or host:port) presents, without signing in.
    static func bastionHostKey(host: String) async throws -> KubeBastionHostKey {
        let host = host.trimmingCharacters(in: .whitespacesAndNewlines)
        let json = try await run { IchorgoKubeBastionHostKey(host, $0) }
        return try KubeBastionHostKey.decode(json)
    }

    /// Sets `context`'s bastion after the core checked the pin, the sign-in and a connection to
    /// the API server; throws with a kube-bastion-* code otherwise.
    static func setBastion(kube: String, context: String, fields: BastionFields) async throws -> KubeBastionInfo {
        let input = try fields.json()
        let json = try await run { IchorgoKubeSetBastion(kube, context, input, $0) }
        return try KubeBastionInfo.decode(json)
    }

    /// Forgets `context`'s bastion: it connects directly again.
    static func removeBastion(kube: String, context: String) async throws {
        try await run { error -> Void in
            _ = IchorgoKubeRemoveBastion(kube, context, error)
        }
    }
}
