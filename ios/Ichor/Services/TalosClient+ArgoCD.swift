import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// The Argo CD Applications of every namespace, with their ApplicationSets and projects
    /// (os:admin), read through their custom resources: no Argo CD token needed.
    func argoCD() async throws -> ArgoStatus {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeArgoCD(config, context, kubeServer, $0) }
    }

    /// How traffic reaches the Application namespace/name (os:admin): hosts, Gateways, routes,
    /// Services, pods and the nodes they run on, with the likely root cause.
    func argoNetwork(namespace: String, name: String) async throws -> ArgoNetwork {
        try await Self.json { [config, context, kubeServer] in IchorgoKubeArgoNetwork(config, context, kubeServer, namespace, name, $0) }
    }

    /// Runs action on the Application namespace/name (os:admin): one merge patch the
    /// application controller picks up. options: the sync sheet's choices, or the rollback target.
    func argoAction(namespace: String, name: String, action: ArgoAction, options: ArgoSyncOptions? = nil) async throws {
        let json = options?.json ?? ""
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoKubeArgoAction(config, context, kubeServer, namespace, name, action.rawValue, json, error)
        }
    }
}
