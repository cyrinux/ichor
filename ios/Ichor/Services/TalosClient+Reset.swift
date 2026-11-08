import Foundation
import IchorCore
import Ichorgo

/// `talosctl reset` and the plan that says what it would wipe and leave.
extension TalosClient {
    /// Role, etcd member, user disks and what forbids resetting node (the last control plane,
    /// a loss of etcd quorum); also reads the cluster upgrade lock (Kubernetes API). Read-only.
    func resetPlan(node: String) async throws -> NodeResetPlan {
        try await Self.json { [config, context, kubeServer] in
            IchorgoNodeResetPlan(config, context, kubeServer, node, $0)
        }
    }

    /// `talosctl reset --wipe-mode --graceful --reboot` (os:admin); Go refuses it on a plan blocker.
    func reset(node: String, wipe: ResetWipe, graceful: Bool, reboot: Bool) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = IchorgoNodeReset(config, context, node, wipe.rawValue, graceful, reboot, error)
        }
    }

    /// Whether node can leave this context, and whether it is also an endpoint. No network.
    func removalPlan(node: String) async throws -> ContextNodeRemoval {
        try await Self.json { [config, context] in
            IchorgoContextNodeRemovalPlan(config, context, node, $0)
        }
    }

    /// Whether node does not answer and the cluster no longer lists it. Read-only.
    func forgettable(node: String) async throws -> Bool {
        let check: NodeForgetCheck = try await Self.json { [config, context] in
            IchorgoNodeForgettable(config, context, node, $0)
        }
        return check.forgettable
    }

    /// Deletes the Node of a node being reset once its kubelet has stopped; waits up to 5 minutes.
    func deleteDepartedNode(hostname: String, node: String) async throws {
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoKubeDeleteDepartedNode(config, context, kubeServer, hostname, node, 300, error)
        }
    }
}
