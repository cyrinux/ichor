import Foundation
import IchorCore
import Ichorgo

/// Kyverno's admission policies and their PolicyReport findings, through the Kubernetes API (os:admin).
extension TalosClient {
    /// Every policy with its readiness and findings, and the engine's health.
    func policies() async throws -> PolicyReport {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubePolicies(config, context, kubeServer, $0) }
    }
}
