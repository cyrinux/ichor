import Foundation
import Ichorgo
import IchorCore

/// A node's taints and labels: the read, the dry-run plan of an edits document and its apply
/// (see TalosClient for the conventions). `node` is the Talos address, resolved to its
/// Kubernetes node (os:admin); the `kubeNode…` twins take the Kubernetes name on a cluster
/// without Talos.
extension TalosClient {
    func nodeMeta(node: String) async throws -> KubeNodeMeta {
        try await Self.json { [config, context, kubeServer] in IchorgoNodeMeta(config, context, kubeServer, node, $0) }
    }

    /// The diff, the pods a new NoExecute taint evicts, the blockers (the API server's dry run).
    func nodeMetaPlan(node: String, edits: NodeMetaEdits) async throws -> NodeMetaPlan {
        let json = try edits.json()
        return try await Self.json { [config, context, kubeServer] in
            IchorgoNodeMetaPlan(config, context, kubeServer, node, json, $0)
        }
    }

    /// `force`: changes protected taints and labels too.
    func nodeMetaApply(node: String, edits: NodeMetaEdits, force: Bool) async throws {
        let json = try edits.json()
        try await Self.run { [config, context, kubeServer] error -> Void in
            _ = IchorgoNodeMetaApply(config, context, kubeServer, node, json, force, error)
        }
    }

    func kubeNodeMeta(kubeNode: String) async throws -> KubeNodeMeta {
        try await Self.json { [config = kubeConfig, context = kubeContext, kubeServer = kubeAPIServer] in
            IchorgoKubeNodeMeta(config, context, kubeServer, kubeNode, $0)
        }
    }

    func kubeNodeMetaPlan(kubeNode: String, edits: NodeMetaEdits) async throws -> NodeMetaPlan {
        let json = try edits.json()
        return try await Self.json { [config = kubeConfig, context = kubeContext, kubeServer = kubeAPIServer] in
            IchorgoKubeNodeMetaPlan(config, context, kubeServer, kubeNode, json, $0)
        }
    }

    func kubeNodeMetaApply(kubeNode: String, edits: NodeMetaEdits, force: Bool) async throws {
        let json = try edits.json()
        try await Self.run { [config = kubeConfig, context = kubeContext, kubeServer = kubeAPIServer] error -> Void in
            _ = IchorgoKubeNodeMetaApply(config, context, kubeServer, kubeNode, json, force, error)
        }
    }
}
