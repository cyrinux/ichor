import Foundation
import Ichorgo
import IchorCore

/// The overview's extras: the cluster map that groups its nodes by site, and live CPU and
/// memory for its summary (see TalosClient for the conventions).
extension TalosClient {
    /// Members, KubeSpan links and sites (os:reader); every part is best-effort in Go.
    func topology() async throws -> ClusterTopology {
        try await Self.json { [config, context] in IchorgoClusterTopology(config, context, $0) }
    }

    /// Cumulative CPU times and current memory of every node that answered in time.
    func clusterStats() async throws -> ClusterStatsSample {
        try await Self.json { [config, context] in IchorgoClusterStats(config, context, $0) }
    }
}
