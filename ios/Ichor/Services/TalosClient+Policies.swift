import Foundation
import IchorCore
import Ichorgo

/// Kyverno's policies and Gatekeeper's constraints with their findings, through the Kubernetes API (os:admin).
extension TalosClient {
    /// Every policy with its readiness and findings, and the engine's health.
    func policies() async throws -> PolicyReport {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubePolicies(config, context, kubeServer, $0) }
    }

    /// Switches the Kyverno policy `name` (`namespace` "": a ClusterPolicy) to `action`, Audit or
    /// Enforce, its rules' failureAction with it. Refused on the demo and without patch access.
    @discardableResult
    func setPolicyAction(namespace: String, name: String, action: PolicyAction) async throws -> PolicyActionResult {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubePolicySetAction(config, context, kubeServer, namespace, name, action.rawValue, $0)
        }
    }
}
