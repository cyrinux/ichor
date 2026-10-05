import Foundation

// Live cluster CPU and memory for the overview's summary, from the Go core's ClusterStats.
// Same as Android's model/ClusterUsage.kt and ui/overview/ClusterLiveViewModel.kt.

/// Cumulative CPU times and current memory of one node.
public struct NodeCounters: Decodable, Equatable, Sendable {
    public let node: String
    public let cpuBusy: Double
    public let cpuTotal: Double
    public let cpuCount: Int
    public let memTotal: UInt64
    public let memAvailable: UInt64

    public init(node: String, cpuBusy: Double, cpuTotal: Double, cpuCount: Int = 0, memTotal: UInt64 = 0, memAvailable: UInt64 = 0) {
        self.node = node
        self.cpuBusy = cpuBusy
        self.cpuTotal = cpuTotal
        self.cpuCount = cpuCount
        self.memTotal = memTotal
        self.memAvailable = memAvailable
    }

    private enum CodingKeys: String, CodingKey { case node, cpuBusy, cpuTotal, cpuCount, memTotal, memAvailable }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        node = try c.decode(String.self, forKey: .node)
        cpuBusy = try c.field(.cpuBusy, 0)
        cpuTotal = try c.field(.cpuTotal, 0)
        cpuCount = try c.field(.cpuCount, 0)
        memTotal = try c.field(.memTotal, 0)
        memAvailable = try c.field(.memAvailable, 0)
    }
}

/// One sample of every node that answered; `at` in epoch milliseconds.
public struct ClusterStatsSample: Decodable, Equatable, Sendable {
    public let at: Int64
    public let nodes: [NodeCounters]

    public init(at: Int64, nodes: [NodeCounters] = []) {
        self.at = at
        self.nodes = nodes
    }

    private enum CodingKeys: String, CodingKey { case at, nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.field(.at, 0)
        nodes = try c.field(.nodes, [])
    }
}

/// Samples further apart than this (the screen was left a while) do not make a CPU point: it
/// would average minutes into what the sparkline draws as one step.
public let maxSampleGapMillis: Int64 = 15_000

/// Live usage of the cluster for the overview's summary.
public struct ClusterUsage: Equatable, Sendable {
    /// Share of all CPU time spent busy since the previous sample; nil until there are two.
    public var cpuFraction: Double?
    /// Total memory of the answering nodes; 0 when one of them did not say, so it is not understated.
    public let memTotal: UInt64
    public let memAvailable: UInt64
    /// Nodes that answered the sample.
    public let nodes: Int

    public init(cpuFraction: Double?, memTotal: UInt64, memAvailable: UInt64, nodes: Int) {
        self.cpuFraction = cpuFraction
        self.memTotal = memTotal
        self.memAvailable = memAvailable
        self.nodes = nodes
    }

    public var memUsedFraction: Double? {
        memTotal == 0 ? nil : usedFraction(total: memTotal, available: memAvailable)
    }

    /// Usage between two samples. CPU sums the busy and total time deltas of every node present
    /// in both, so a big node weighs more than a small one; a node whose counters went backwards
    /// (it rebooted) is skipped. Memory is the latest sample's.
    public init(previous: ClusterStatsSample?, current cur: ClusterStatsSample) {
        var before: [String: NodeCounters] = [:]
        if let previous, cur.at - previous.at <= maxSampleGapMillis {
            before = Dictionary(previous.nodes.map { ($0.node, $0) }, uniquingKeysWith: { _, last in last })
        }
        let deltas = cur.nodes.compactMap { n -> (busy: Double, total: Double)? in
            guard let p = before[n.node] else { return nil }
            let busy = n.cpuBusy - p.cpuBusy
            let total = n.cpuTotal - p.cpuTotal
            return busy < 0 || total <= 0 ? nil : (busy, total)
        }
        let total = deltas.reduce(0) { $0 + $1.total }
        let cpu = total > 0 ? min(max(deltas.reduce(0) { $0 + $1.busy } / total, 0), 1) : nil
        let memoryComplete = !cur.nodes.isEmpty && cur.nodes.allSatisfy { $0.memTotal > 0 }
        self.init(
            cpuFraction: cpu,
            memTotal: memoryComplete ? cur.nodes.reduce(0) { $0 + $1.memTotal } : 0,
            memAvailable: memoryComplete ? cur.nodes.reduce(0) { $0 + $1.memAvailable } : 0,
            nodes: cur.nodes.count
        )
    }
}

/// `history` with `value` appended (nothing when nil), keeping the newest `max` points.
public func appendHistory(_ history: [Double], _ value: Double?, max: Int) -> [Double] {
    guard let value else { return history }
    return Array((history + [value]).suffix(Swift.max(max, 0)))
}

/// CPU history for the sparkline: three minutes, nine on a dense cluster (see clusterPollSeconds).
public let clusterHistoryPoints = 36

/// After this many failed samples in a row, the summary falls back to the overview's snapshot.
public let clusterLiveMaxFailures = 3

/// The second sample comes sooner, so the CPU shows up (or catches up) about a second after opening.
public let clusterFirstDeltaSeconds = 1.0

/// Live cluster usage: `usage` nil until the first sample (or after repeated failures).
/// Fed by the overview's polling loop, which only makes the Go calls.
public struct ClusterLive: Equatable, Sendable {
    public private(set) var usage: ClusterUsage?
    public private(set) var cpuHistory: [Double] = []
    private var last: ClusterStatsSample?
    private var failures = 0
    /// Samples since (re)starting; the second one comes after clusterFirstDeltaSeconds.
    private var samples = 0

    public init() {}

    /// Sampling starts again (back on the overview): the values stay, but the next delta comes
    /// soon and earlier failures no longer count.
    public mutating func resume() {
        samples = 0
        failures = 0
    }

    /// A sample answered by at least one node (an empty one is a failure, not an empty cluster).
    public mutating func record(_ sample: ClusterStatsSample) {
        guard !sample.nodes.isEmpty else { return recordFailure() }
        var fresh = ClusterUsage(previous: last, current: sample)
        // One sample without CPU (nodes rebooted or replaced) keeps the last reading.
        let cpu = fresh.cpuFraction
        fresh.cpuFraction = cpu ?? usage?.cpuFraction
        cpuHistory = appendHistory(cpuHistory, cpu, max: clusterHistoryPoints)
        usage = fresh
        last = sample
        failures = 0
        samples += 1
    }

    /// A blip keeps the last values; a cluster that stopped answering must not look live.
    public mutating func recordFailure() {
        failures += 1
        if failures >= clusterLiveMaxFailures {
            self = ClusterLive()
        }
    }

    /// Seconds to wait before the next sample of a cluster of `nodeCount` nodes: soon right
    /// after (re)starting, so a fresh delta shows, then the cluster's cadence.
    public func nextDelay(nodeCount: Int) -> Double {
        samples == 1 ? clusterFirstDeltaSeconds : Double(clusterPollSeconds(nodeCount))
    }
}
