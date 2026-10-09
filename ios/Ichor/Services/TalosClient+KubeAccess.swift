import Foundation
import IchorCore
import Ichorgo

/// "Can I?": what the cluster's credentials may do, asked with SelfSubjectAccessReviews before
/// a tap rather than learnt from a 403 after it (see go/ichorgo/kube_access.go).
extension TalosClient {
    /// Which of the app's Kubernetes actions may run in `namespace` ("" for cluster-wide).
    /// Cached a few minutes by the core.
    func kubeActionAccess(namespace: String) async throws -> KubeActionAccess {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeActionAccess(config, context, kubeServer, namespace, $0)
        }
    }

    /// Whether `verb` on `resource` ("resource" or "resource/subresource") of `group` may run
    /// in `namespace` ("" for every namespace or cluster-scoped), on `name` ("" for any).
    func kubeCan(verb: String, group: String, resource: String, namespace: String, name: String = "") async throws -> KubeAccess {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeCan(config, context, kubeServer, verb, group, resource, namespace, name, $0)
        }
    }

    /// Who the credentials are to the API server.
    func kubeWhoAmI() async throws -> KubeWhoAmI {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeWhoAmI(config, context, kubeServer, $0)
        }
    }
}
