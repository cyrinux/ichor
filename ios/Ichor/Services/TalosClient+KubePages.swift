import Foundation
import Ichorgo
import IchorCore

/// Kubernetes lists page by page, namespace first (the Linear plan document "U11. Large clusters: home at scale, namespace-first paged lists"; os:admin,
/// see TalosClient for the conventions). `namespace` nil is every namespace; `token` is the
/// previous page's continue token, "" for the first page.
extension TalosClient {
    /// The cluster's namespaces, to pick the scope of the lists. Forbidden is an answer, not an error.
    func namespaces() async throws -> KubeNamespaces {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubeNamespaces(config, context, kubeServer, $0) }
    }

    /// One page of pods in the API server's order. `table`: the server's Table rows, without
    /// images nor containers (`pod` reads those).
    func podsPage(namespace: String?, token: String, table: Bool, limit: Int = kubePageSize) async throws -> KubePage<KubePod> {
        let ns = namespace ?? ""
        let page: KubePodPage = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubePodsPage(config, context, kubeServer, ns, token, limit, table, $0)
        }
        return page.page(detailed: !table)
    }

    /// The Kubernetes name of the Talos node at `node`, as its kubelet registered it (Talos only).
    func kubeNodeName(node: String) async throws -> String {
        try await Self.run { [config, context] in IchorgoKubeNodeName(config, context, node, $0) }
    }

    /// One page of the pods scheduled on the Kubernetes node `kubeNode` (`kubeNodeName`), in
    /// every namespace, narrowed to `phase`; as `podsPage` otherwise.
    func nodePodsPage(kubeNode: String, phase: PodPhaseFilter, token: String, table: Bool,
                      limit: Int = selectedPodsPageSize) async throws -> KubePage<KubePod> {
        let query = phase.query
        let page: KubePodPage = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeNodePodsPage(config, context, kubeServer, kubeNode, query, token, limit, table, $0)
        }
        return page.page(detailed: !table)
    }

    /// One page of the pods a workload's selector matches, narrowed to `phase`; as `podsPage` otherwise.
    func workloadPodsPage(kind: String, namespace: String, name: String, phase: PodPhaseFilter, token: String, table: Bool,
                          limit: Int = selectedPodsPageSize) async throws -> KubePage<KubePod> {
        let query = phase.query
        let page: KubePodPage = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeWorkloadPodsPage(config, context, kubeServer, kind, namespace, name, query, token, limit, table, $0)
        }
        return page.page(detailed: !table)
    }

    /// One pod in full (images, containers, last termination), for a row read from a Table.
    func pod(namespace: String, name: String) async throws -> KubePod {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in IchorgoKubePod(config, context, kubeServer, namespace, name, $0) }
    }

    /// One page of the Deployments, StatefulSets or DaemonSets (`kind`).
    func workloadsPage(kind: String, namespace: String?, token: String, limit: Int = kubePageSize) async throws -> KubePage<KubeWorkload> {
        let ns = namespace ?? ""
        let page: KubeWorkloadPage = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeWorkloadsPage(config, context, kubeServer, kind, ns, token, limit, $0)
        }
        return page.page
    }

    /// One page of every workload kind, side by side, merged (see fetchWorkloadPage).
    func workloadsPage(namespace: String?, token: String) async throws -> KubePage<KubeWorkload> {
        try await fetchWorkloadPage(token) { kind, kindToken in
            try await workloadsPage(kind: kind, namespace: namespace, token: kindToken)
        }
    }

    /// One workload as it is now: its replicas, for a restart confirmation.
    func workload(kind: String, namespace: String, name: String) async throws -> KubeWorkload {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeWorkload(config, context, kubeServer, kind, namespace, name, $0)
        }
    }

    /// One page of CronJobs with their recent runs.
    func cronJobsPage(namespace: String?, token: String, limit: Int = kubePageSize) async throws -> KubePage<KubeCronJob> {
        let ns = namespace ?? ""
        let page: KubeCronJobPage = try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubeCronJobsPage(config, context, kubeServer, ns, token, limit, $0)
        }
        return page.page
    }
}
