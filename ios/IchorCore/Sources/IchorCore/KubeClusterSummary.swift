import Foundation

/// What the Kubernetes home's summary shows, as the Talos one: the node counts, an overall
/// status, the capacity, and with metrics-server the share of it in use. Same as Android's
/// model/KubeClusterSummary.kt.
public struct KubeClusterSummary: Equatable, Sendable {
    public let total: Int
    public let ready: Int
    public let status: ClusterStatus
    /// Cores of every node, rounded; 0 when no node said.
    public let cpuCount: Int
    /// Bytes of every node; 0 when no node said.
    public let memTotal: Int64
    /// Busy share of the allocatable CPU, nil without metrics-server.
    public let cpuFraction: Double?
    /// Used share of the allocatable memory, nil without metrics-server.
    public let memFraction: Double?

    public var notReady: Int { total - ready }
}

public extension KubeNodesOverview {
    /// The summary of these nodes, with each node's `usage` when metrics-server answered (none: empty).
    func clusterSummary(usage used: [KubeTopNode] = []) -> KubeClusterSummary {
        let ready = readyCount
        let total = nodes.count
        let status: ClusterStatus = ready == total ? .healthy : ready == 0 ? .down : .degraded
        return KubeClusterSummary(
            total: total,
            ready: ready,
            status: status,
            cpuCount: Int(nodes.reduce(0) { $0 + $1.cpu }.rounded()),
            memTotal: Int64(nodes.reduce(0) { $0 + $1.memory }),
            cpuFraction: share(used.reduce(0) { $0 + $1.cpu }, of: used.reduce(0) { $0 + $1.cpuAllocatable }),
            memFraction: share(used.reduce(0) { $0 + $1.memory }, of: used.reduce(0) { $0 + $1.memoryAllocatable })
        )
    }
}

private func share(_ used: Double, of total: Double) -> Double? {
    total > 0 ? min(max(used / total, 0), 1) : nil
}
