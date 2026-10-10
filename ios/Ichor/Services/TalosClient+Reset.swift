import Foundation
import IchorCore
import Ichorgo

/// `talosctl reset` and the plan that says what it would wipe and leave.
extension TalosClient {
    /// Role, etcd member, user disks and what forbids resetting node (the last control plane,
    /// a loss of etcd quorum); also reads the cluster upgrade lock (Kubernetes API). Read-only.
    func resetPlan(node: String) async throws -> NodeResetPlan {
        try await Self.json { [config, context, kubeServer] in IchorgoNodeResetPlan(config, context, kubeServer, node, $0) }
    }

    /// `talosctl reset --wipe-mode --graceful --reboot` (os:admin); Go refuses it on a plan blocker.
    func reset(node: String, wipe: ResetWipe, graceful: Bool, reboot: Bool) async throws {
        try await Self.run { [config, context] error -> Void in
            _ = IchorgoNodeReset(config, context, node, wipe.rawValue, graceful, reboot, error)
        }
    }
}
