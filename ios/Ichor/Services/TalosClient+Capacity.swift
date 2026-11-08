import Foundation
import IchorCore
import Ichorgo

/// The cluster's capacity and the cost of losing a node, through the Kubernetes API (os:admin).
extension TalosClient {
    /// Free room per node, each workload's priority, QoS and OOM risk, and the N+1 verdict. It
    /// lists every pod: on demand, never cached.
    func capacity() async throws -> KubeCapacity {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeCapacity(config, context, kubeServer, $0)
        }
    }

    /// What losing the Kubernetes nodes `nodes` together would cost; empty: the worst node, with the others as candidates.
    func nodeLoss(nodes: [String]) async throws -> NodeLossPlan {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeCapacityNodeLoss(config, context, kubeServer, nodes.joined(separator: ","), $0)
        }
    }
}
