import Foundation
import Ichorgo
import IchorCore

/// PromQL panels from the cluster's Prometheus, Mimir, Thanos or VictoriaMetrics: through the
/// Kubernetes API service proxy (os:admin) or at a URL (see TalosClient for the conventions).
extension TalosClient {
    /// Query APIs among the cluster's Services, the likeliest first.
    func promDiscover() async throws -> [PromSource] {
        let found: PromDiscovery = try await Self.json { [config, context, kubeServer] in
            IchorgoPromDiscover(config, context, kubeServer, $0)
        }
        return found.sources
    }

    /// `query` from `start` to `end` (unix seconds) against `source`, about 250 points.
    func promRange(_ source: PromSource, query: String, start: Int64, end: Int64) async throws -> PromResult {
        let sourceJSON = try Self.encode(source)
        return try await Self.json { [config, context, kubeServer] in
            IchorgoPromQueryRange(config, context, kubeServer, sourceJSON, query, start, end, 0, $0)
        }
    }

    /// The built-in panels.
    static func promPresets() async throws -> [PromPanel] {
        try await json { IchorgoPromPresets($0) }
    }

    /// `source` checked and cleaned up by Go; the secret kept only with authentication.
    static func normalizePromSource(_ source: PromSource) async throws -> PromSource {
        let sourceJSON = try encode(source)
        var checked: PromSource = try await json { IchorgoNormalizePromSource(sourceJSON, $0) }
        checked.secret = checked.auth == PromSource.authNone ? "" : source.secret.trimmingCharacters(in: .whitespacesAndNewlines)
        return checked
    }

    private static func encode(_ source: PromSource) throws -> String {
        try TalosJSON.encode(source)
    }
}
