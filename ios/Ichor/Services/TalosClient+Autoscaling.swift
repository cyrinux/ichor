import Foundation
import IchorCore
import Ichorgo

/// What scales the cluster: HPAs, KEDA, VPA recommendations and Karpenter, through the Kubernetes API (os:admin).
extension TalosClient {
    /// Every autoscaler's rows with their states, and the per-workload index.
    func autoscaling() async throws -> AutoscalingReport {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeAutoscaling(config, context, kubeServer, $0)
        }
    }

    /// Pauses the ScaledObject at `replicas`, holds it at its count (`ScaledObjectPause.hold`)
    /// or resumes it (`ScaledObjectPause.resume`).
    func pauseScaledObject(namespace: String, name: String, replicas: Int) async throws {
        try await Self.run {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubePauseScaledObject(config, context, kubeServer, namespace, name, replicas, error)
        }
    }

    /// Sets the HPA's minReplicas and maxReplicas.
    func setHPABounds(namespace: String, name: String, min: Int, max: Int) async throws {
        try await Self.run {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubeSetHPABounds(config, context, kubeServer, namespace, name, min, max, error)
        }
    }
}
