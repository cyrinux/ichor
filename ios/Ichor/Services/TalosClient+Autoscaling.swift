import Foundation
import IchorCore
import Ichorgo

/// What scales the cluster: HPAs, KEDA, VPA recommendations and Karpenter, through the Kubernetes API (os:admin).
extension TalosClient {
    /// Every autoscaler's rows with their states, the per-workload index, and the cloud's node groups.
    func autoscaling() async throws -> AutoscalingReport {
        async let groups = nodeGroups()
        let report: AutoscalingReport = try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeAutoscaling(config, context, kubeServer, $0)
        }
        return report.withNodeGroups(await groups)
    }

    /// The cluster's EKS managed node groups, read with its own AWS sign-in. A failed read is
    /// an empty report: the rest of the Autoscaling screen does not depend on AWS.
    func nodeGroups() async -> NodeGroupsReport {
        let report: NodeGroupsReport? = try? await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeNodeGroups(config, context, kubeServer, $0)
        }
        return report ?? NodeGroupsReport()
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

    /// Changes the EKS managed node group's size; the update AWS starts for it.
    func scaleNodeGroup(group: String, desired: Int, min: Int, max: Int) async throws -> NodeGroupUpdate {
        try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeNodeGroupScale(config, context, kubeServer, group, desired, min, max, $0)
        }
    }

    /// Upgrades the EKS managed node group to the control plane's `version`, or to the latest
    /// node image of its own when `version` is empty; `force` ignores pod disruption budgets.
    /// The update AWS starts for it.
    func nodeGroupUpgrade(group: String, version: String, force: Bool) async throws -> NodeGroupUpdate {
        try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeNodeGroupUpgrade(config, context, kubeServer, group, version, force, $0)
        }
    }

    /// The update `id` of `group`, as AWS reports it now.
    func nodeGroupUpdate(group: String, id: String) async throws -> NodeGroupUpdate {
        try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeNodeGroupUpdate(config, context, kubeServer, group, id, $0)
        }
    }
}
