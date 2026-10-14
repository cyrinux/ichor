import Foundation
import IchorCore
import Ichorgo

/// What scales the cluster: HPAs, KEDA, VPA recommendations and Karpenter, through the Kubernetes API (os:admin).
extension TalosClient {
    /// Every autoscaler's rows with their states, and the per-workload index.
    func autoscaling() async throws -> AutoscalingReport {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeAutoscaling(config, context, kubeServer, $0) }
    }
}
