import Foundation
import IchorCore
import Ichorgo

/// Argo Rollouts and the `kubectl argo rollouts` plugin's actions, through the Kubernetes API (os:admin).
extension TalosClient {
    /// The Rollouts of every namespace, worst first; installed false when the cluster serves none.
    func argoRollouts() async throws -> ArgoRolloutsStatus {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeArgoRollouts(config, context, kubeServer, $0)
        }
    }

    /// Runs `action` on `rollout` with the plugin's patches; the Go core refuses what the state forbids.
    func argoRolloutAction(_ action: ArgoRolloutAction, on rollout: ArgoRollout) async throws {
        let namespace = rollout.namespace, name = rollout.name
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeArgoRolloutAction(config, context, kubeServer, namespace, name, action.rawValue, error)
        }
    }
}
