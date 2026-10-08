import Foundation
import IchorCore
import Ichorgo

/// The cluster checkup and an object's events through the Kubernetes API (os:admin), and the
/// Talos rollback.
extension TalosClient {
    /// What no other screen shows, section by section. It lists the cluster's pods and asks every
    /// kubelet for its volumes: on demand, never cached.
    func checkup() async throws -> CheckupReport {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeCheckup(config, context, kubeServer, $0) }
    }

    /// The events of the `kind` named `name`, newest first; with an empty `kind`, those of `name`
    /// and of what it owns by name (a Deployment's ReplicaSets and pods).
    func kubeEvents(namespace: String, kind: String, name: String) async throws -> [KubeEvent] {
        let list: KubeEventList = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeEvents(config, context, kubeServer, namespace, kind, name, $0)
        }
        return list.events
    }

    /// The events of every namespace and of the nodes, newest first (`kubectl get events -A`),
    /// the Warning ones only with `warningsOnly`; `forbidden` when the credentials may not list
    /// them cluster-wide. Kubernetes keeps events for an hour.
    func kubeClusterEvents(warningsOnly: Bool) async throws -> KubeEventList {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeClusterEvents(config, context, kubeServer, warningsOnly, 0, $0)
        }
    }

    /// `talosctl rollback`: the node reboots into the Talos it ran before its last upgrade (os:admin).
    func rollback(node: String) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = IchorgoRollback(config, context, node, error)
        }
    }
}
