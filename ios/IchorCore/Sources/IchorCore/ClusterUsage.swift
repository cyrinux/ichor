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

    public init(
        node: String, cpuBusy: Double, cpuTotal: Double, cpuCount: Int = 0, memTotal: UInt64 = 0,
        memAvailable: UInt64 = 0
    ) {
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

/// One sample of the asked nodes that answered; `at` in epoch milliseconds. `sampled` nodes
/// were asked (a down one is asked but missing from `nodes`) of the context's `total`; both 0
/// from an older core, which always asks every node.
public struct ClusterStatsSample: Decodable, Equatable, Sendable {
    public let at: Int64
    public let nodes: [NodeCounters]
    public let sampled: Int
    public let total: Int

    public init(at: Int64, nodes: [NodeCounters] = [], sampled: Int = 0, total: Int = 0) {
        self.at = at
        self.nodes = nodes
        self.sampled = sampled
        self.total = total
    }

    /// Only some of the nodes were asked (the focus set of a large cluster).
    public var isPartial: Bool { total > 0 && sampled < total }

    private enum CodingKeys: String, CodingKey { case at, nodes, sampled, total }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        at = try c.field(.at, 0)
        nodes = try c.field(.nodes, [])
        sampled = try c.field(.sampled, 0)
        total = try c.field(.total, 0)
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

/// Horizontal gap between two of `count` sparkline points on a `width`-wide canvas holding a
/// `points`-sample window. Until the window is full the samples span the whole width; after that
/// the step is fixed and the line scrolls. Drawn from `x = width - step * (count - 1)`, which is 0
/// while the window fills, and the two steps meet at `count == points`, so nothing jumps.
public func sparklineStep(count: Int, points: Int, width: Double) -> Double {
    width / Double(min(max(count, 2), max(points, 2)) - 1)
}

/// CPU history for the sparkline: three minutes, nine on a dense cluster (see clusterPollSeconds).
public let clusterHistoryPoints = 36

/// After this many failed samples in a row, the summary falls back to the overview's snapshot.
public let clusterLiveMaxFailures = 3

/// The second sample comes sooner, so the CPU shows up (or catches up) about a second after opening.
public let clusterFirstDeltaSeconds = 1.0

/// Live cluster usage: `usage` nil until the first sample (or after repeated failures).
/// Fed by the overview's polling loop, which only makes the Go calls. On a large cluster most
/// samples ask only the focus set (`nodesToSample`); from the first partial one on, the usage
/// is merged from the last known counters of every node (NodeCountersMerge).
public struct ClusterLive: Equatable, Sendable {
    public private(set) var usage: ClusterUsage?
    public private(set) var cpuHistory: [Double] = []
    /// Nodes the last sample asked, and of how many; equal unless it was partial.
    public private(set) var sampledCount = 0
    public private(set) var total = 0
    private var last: ClusterStatsSample?
    private var merge = NodeCountersMerge()
    /// A partial sample came: the usage is the merge's from then on.
    private var merging = false
    private var failures = 0
    /// Samples since (re)starting; the second one comes after clusterFirstDeltaSeconds.
    private var samples = 0
    /// Polls since (re)starting, sampled, failed or skipped: every clusterFullSampleEvery-th asks every node.
    private var ticks = 0

    public init() {}

    /// The last sample asked only some of the nodes: the summary says "live: N of M nodes".
    public var isPartial: Bool { total > 0 && sampledCount < total }

    /// Sampling starts again (back on the overview): the values stay, but the next delta comes
    /// soon and earlier failures no longer count.
    public mutating func resume() {
        samples = 0
        failures = 0
        ticks = 0
    }

    /// The nodes the next sample asks on a cluster of `nodeCount` nodes: [] for every node, nil
    /// for no sample this tick (skipTick). Up to the dense threshold, at the start (the first
    /// sample and its quick delta) and every clusterFullSampleEvery-th tick: every node;
    /// otherwise the `focus` set (problem and visible nodes), nothing when it is empty.
    public func nodesToSample(nodeCount: Int, focus: Set<String>) -> [String]? {
        guard isDenseCluster(nodeCount), samples >= 2, ticks % clusterFullSampleEvery != 0 else { return [] }
        return focus.isEmpty ? nil : focus.sorted()
    }

    /// A tick without a sample (an empty focus set): the values stay.
    public mutating func skipTick() {
        ticks += 1
    }

    /// A sample answered by at least one node (an empty one is a failure, not an empty cluster;
    /// an empty partial one only means the asked nodes are down, so the values stay).
    public mutating func record(_ sample: ClusterStatsSample) {
        ticks += 1
        guard !sample.nodes.isEmpty else {
            if sample.isPartial { return noteSampled(sample) }
            failures += 1
            if failures >= clusterLiveMaxFailures { self = ClusterLive() }
            return
        }
        merge.add(sample)
        merging = merging || sample.isPartial
        var fresh = merging ? merge.usage(at: sample.at) : ClusterUsage(previous: last, current: sample)
        // One sample without CPU (nodes rebooted or replaced) keeps the last reading.
        let cpu = fresh.cpuFraction
        fresh.cpuFraction = cpu ?? usage?.cpuFraction
        cpuHistory = appendHistory(cpuHistory, cpu, max: clusterHistoryPoints)
        usage = fresh
        last = sample
        failures = 0
        samples += 1
        noteSampled(sample)
    }

    private mutating func noteSampled(_ sample: ClusterStatsSample) {
        sampledCount = sample.total > 0 ? sample.sampled : sample.nodes.count
        total = sample.total > 0 ? sample.total : sample.nodes.count
    }

    /// A blip keeps the last values; a cluster that stopped answering must not look live.
    public mutating func recordFailure() {
        ticks += 1
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
