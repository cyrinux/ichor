import Foundation

// Live usage of a large cluster sampled part by part: most samples ask only the focus set
// (problem and visible nodes), every clusterFullSampleEvery-th one every node, and the usage
// comes from the last known counters of every node. Android merges its samples the same way.

/// On a dense cluster, every this many samples asks every node; the others only the focus set.
public let clusterFullSampleEvery = 4

/// A node's last counters older than this no longer count (two full rounds on a dense cluster,
/// slow calls included): a node that stopped answering drops out rather than look live.
public let maxNodeStaleMillis = Int64(2 * clusterFullSampleEvery * denseLiveStatsPollSeconds) * 1000

/// The last known counters of every node, and each one's CPU rate between its last two samples.
struct NodeCountersMerge: Equatable, Sendable {
    private struct Seen: Equatable, Sendable {
        let counters: NodeCounters
        let at: Int64
        /// Busy and total CPU time per millisecond since the node's previous sample; nil
        /// without one recent enough, or when its counters went backwards (it rebooted).
        let rate: CpuRate?
    }

    private struct CpuRate: Equatable, Sendable {
        let busy: Double
        let total: Double
    }

    private var seen: [String: Seen] = [:]

    /// The sample's nodes replace their last counters; a full sample also forgets the nodes it
    /// misses (down), as a full sample's usage leaves them out.
    mutating func add(_ sample: ClusterStatsSample) {
        var next = sample.isPartial ? seen : [:]
        for n in sample.nodes {
            next[n.node] = Seen(counters: n, at: sample.at, rate: rate(of: n, at: sample.at, since: seen[n.node]))
        }
        seen = next
    }

    /// The cluster's usage at `at` over the nodes known recently enough. CPU sums each node's
    /// busy and total rates, so a node sampled a minute ago weighs as much as one sampled now and
    /// a big node more than a small one; memory is each node's last, dropped when one did not say.
    func usage(at: Int64) -> ClusterUsage {
        let fresh = seen.values.filter { at - $0.at <= maxNodeStaleMillis }
        let rates = fresh.compactMap(\.rate)
        let total = rates.reduce(0) { $0 + $1.total }
        let cpu = total > 0 ? min(max(rates.reduce(0) { $0 + $1.busy } / total, 0), 1) : nil
        let memoryComplete = !fresh.isEmpty && fresh.allSatisfy { $0.counters.memTotal > 0 }
        return ClusterUsage(
            cpuFraction: cpu,
            memTotal: memoryComplete ? fresh.reduce(0) { $0 + $1.counters.memTotal } : 0,
            memAvailable: memoryComplete ? fresh.reduce(0) { $0 + $1.counters.memAvailable } : 0,
            nodes: fresh.count
        )
    }

    private func rate(of n: NodeCounters, at: Int64, since previous: Seen?) -> CpuRate? {
        guard let previous, at > previous.at, at - previous.at <= maxNodeStaleMillis else { return nil }
        let elapsed = Double(at - previous.at)
        let busy = n.cpuBusy - previous.counters.cpuBusy
        let total = n.cpuTotal - previous.counters.cpuTotal
        return busy < 0 || total <= 0 ? nil : CpuRate(busy: busy / elapsed, total: total / elapsed)
    }
}
