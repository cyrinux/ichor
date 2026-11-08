import Foundation
import Ichorgo
import IchorCore

/// The cluster's log backend (Loki, VictoriaLogs): its labels and searches, reached like a metrics
/// source through the Kubernetes API service proxy or at a URL (see TalosClient for the conventions).
extension TalosClient {
    /// Log backends among the cluster's Services, the likeliest first.
    func logsDiscover() async throws -> [PromSource] {
        let found: LogsDiscovery = try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoLogsDiscover(config, context, kubeServer, $0)
        }
        return found.candidates.map(\.source)
    }

    /// Checks `source` answers: cleaned up by Go, with its kind and its secret. Throws why not.
    func logsProbe(_ source: PromSource) async throws -> PromSource {
        let sourceJSON = try TalosJSON.encode(source)
        var checked: PromSource = try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoLogsProbe(config, context, kubeServer, sourceJSON, $0)
        }
        checked.secret =
            checked.auth == PromSource.authNone ? "" : source.secret.trimmingCharacters(in: .whitespacesAndNewlines)
        return checked
    }

    /// The label names of `source`, or the values of `label`.
    func logsLabels(_ source: PromSource, label: String = "") async throws -> [String] {
        let sourceJSON = try TalosJSON.encode(source)
        let found: LogsValues = try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoLogsLabels(config, context, kubeServer, sourceJSON, label, $0)
        }
        return found.values
    }

    /// Runs `query` on `source` over the last `hours`, the newest lines first.
    func logsQuery(_ source: PromSource, query: String, hours: Int) async throws -> LogsResult {
        let sourceJSON = try TalosJSON.encode(source)
        let end = Int64(Date().timeIntervalSince1970 * 1000)
        let start = end - Int64(hours) * 3_600_000
        return try await Self.json {
            [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoLogsQuery(config, context, kubeServer, sourceJSON, query, start, end, 0, $0)
        }
    }

    /// The query selecting one pod in a backend of `kind`.
    static func logsPodQuery(kind: String, namespace: String, pod: String) -> String {
        IchorgoLogsPodQuery(kind, namespace, pod)
    }

    /// The query selecting the pods of one workload in a backend of `kind`.
    static func logsWorkloadQuery(kind: String, namespace: String, workload: String) -> String {
        IchorgoLogsWorkloadQuery(kind, namespace, workload)
    }
}
