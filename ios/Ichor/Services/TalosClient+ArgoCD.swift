import Foundation
import IchorCore
import Ichorgo

extension TalosClient {
    /// The Argo CD Applications of every namespace, with their ApplicationSets and projects
    /// (os:admin), read through their custom resources: no Argo CD token needed.
    func argoCD() async throws -> ArgoStatus {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeArgoCD(config, context, kubeServer, $0) }
    }

    /// How traffic reaches the Application namespace/name (os:admin): hosts, Gateways, routes,
    /// Services, pods and the nodes they run on, with the likely root cause.
    func argoNetwork(namespace: String, name: String) async throws -> ArgoNetwork {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeArgoNetwork(config, context, kubeServer, namespace, name, $0) }
    }

    /// What syncing the Application namespace/name now would change, object by object (os:admin,
    /// read only): what the application controller compared last, read from Argo CD's Redis
    /// through a port-forward. Not cached on the phone: always fresh.
    func argoDiff(namespace: String, name: String) async throws -> FluxDiff {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeArgoDiff(config, context, kubeServer, namespace, name, $0)
        }
    }

    /// Runs action on the Application namespace/name (os:admin): one merge patch the
    /// application controller picks up. options: the sync sheet's choices, or the rollback target.
    func argoAction(namespace: String, name: String, action: ArgoAction, options: ArgoSyncOptions? = nil) async throws {
        let json = options?.json ?? ""
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeArgoAction(config, context, kubeServer, namespace, name, action.rawValue, json, error)
        }
    }

    /// Changes the sync windows of the AppProject namespace/project (os:admin): freezes apps for a
    /// while, extends or ends a freeze, removes a window, clears ended freezes.
    func argoFreeze(namespace: String, project: String, action: ArgoFreezeAction, options: ArgoFreezeOptions = ArgoFreezeOptions()) async throws {
        let json = options.json
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeArgoFreeze(config, context, kubeServer, namespace, project, action.rawValue, json, error)
        }
    }
}
