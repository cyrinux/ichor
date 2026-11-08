import XCTest
@testable import IchorCore

final class ClusterLiveMergeTests: XCTestCase {
    private let gib: UInt64 = 1 << 30

    private func counters(_ node: String, _ busy: Double, _ total: Double, mem: UInt64? = nil) -> NodeCounters {
        NodeCounters(
            node: node, cpuBusy: busy, cpuTotal: total, cpuCount: 4, memTotal: mem ?? 8 * gib, memAvailable: 4 * gib)
    }

    private func sample(_ at: Int64, of total: Int, sampled: Int? = nil, _ nodes: NodeCounters...) -> ClusterStatsSample
    {
        ClusterStatsSample(at: at, nodes: nodes, sampled: sampled ?? nodes.count, total: total)
    }

    private let dense = nodeDenseThreshold + 6

    func testDecodesSampledAndTotal() throws {
        let json = #"{"at":5,"nodes":[{"node":"10.0.0.1","cpuBusy":1,"cpuTotal":2}],"sampled":2,"total":30}"#
        let decoded = try JSONDecoder().decode(ClusterStatsSample.self, from: Data(json.utf8))
        XCTAssertEqual(decoded.sampled, 2)
        XCTAssertEqual(decoded.total, 30)
        XCTAssertTrue(decoded.isPartial)
        // An older core says neither: a full sample.
        XCTAssertFalse(
            try JSONDecoder().decode(ClusterStatsSample.self, from: Data(#"{"at":1,"nodes":[]}"#.utf8)).isPartial)
    }

    func testSmallClusterAlwaysAsksEveryNode() {
        var live = ClusterLive()
        for i in 0..<8 {
            XCTAssertEqual(live.nodesToSample(nodeCount: 3, focus: ["a"]), [])
            live.record(sample(Int64(i) * 5_000, of: 3, sampled: 3, counters("a", Double(i), Double(i * 10))))
        }
        XCTAssertFalse(live.isPartial)
    }

    func testDenseClusterAsksTheFocusBetweenFullSamples() {
        var live = ClusterLive()
        var asked: [[String]?] = []
        for i in 0..<10 {
            let nodes = live.nodesToSample(nodeCount: dense, focus: ["b", "a"])
            asked.append(nodes)
            live.record(sample(Int64(i) * 15_000, of: 3, sampled: nodes?.isEmpty == false ? 2 : 3, counters("a", 0, 0)))
        }
        // The first sample and its quick delta ask every node, then every 4th tick.
        XCTAssertEqual(
            asked, [[], [], ["a", "b"], ["a", "b"], [], ["a", "b"], ["a", "b"], ["a", "b"], [], ["a", "b"]])
    }

    func testAnEmptyFocusSkipsTheTick() {
        var live = ClusterLive()
        live.record(sample(0, of: 3, counters("a", 0, 0)))
        live.record(sample(1_000, of: 3, counters("a", 1, 10)))
        XCTAssertNil(live.nodesToSample(nodeCount: dense, focus: []))
        live.skipTick()
        live.skipTick()
        XCTAssertEqual(live.nodesToSample(nodeCount: dense, focus: []), [])
    }

    func testPartialSampleKeepsTheOtherNodesLastCounters() {
        var live = ClusterLive()
        live.record(sample(0, of: 3, counters("a", 0, 0), counters("b", 0, 0), counters("c", 0, 0)))
        live.record(sample(15_000, of: 3, counters("a", 10, 100), counters("b", 50, 100), counters("c", 90, 100)))
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, 0.5, accuracy: 1e-6)
        // Only "a" asked: "b" and "c" keep their last rates, "a" goes idle.
        live.record(sample(30_000, of: 3, counters("a", 10, 200)))
        XCTAssertTrue(live.isPartial)
        XCTAssertEqual(live.sampledCount, 1)
        XCTAssertEqual(live.total, 3)
        XCTAssertEqual(live.usage?.nodes, 3)
        XCTAssertEqual(live.usage?.memTotal, 24 * gib)
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, (0 + 50 + 90) / 300.0, accuracy: 1e-6)
    }

    func testCpuIsAveragedAcrossTheGapOfANodeSampledOnlyOnFullTicks() {
        var live = ClusterLive()
        live.record(sample(0, of: 2, counters("a", 0, 0), counters("b", 0, 0)))
        live.record(sample(1_000, of: 2, counters("a", 1, 10), counters("b", 1, 10)))
        // "a" in focus each tick at 50 %; "b" only on the next full tick, 60 s later, at 20 %.
        for i in 1...3 {
            let at = 1_000 + Int64(i) * 15_000
            live.record(sample(at, of: 2, counters("a", 1 + Double(i) * 75, 10 + Double(i) * 150)))
        }
        live.record(sample(61_000, of: 2, counters("a", 301, 610), counters("b", 121, 610)))
        XCTAssertFalse(live.isPartial)
        // Both nodes ran for 60 s at the same CPU rate: (50 % + 20 %) / 2.
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, 0.35, accuracy: 1e-6)
    }

    func testANodeTooLongUnseenDropsOut() {
        var live = ClusterLive()
        live.record(sample(0, of: 2, counters("a", 0, 0), counters("b", 0, 0)))
        live.record(sample(maxNodeStaleMillis + 1, of: 2, counters("a", 10, 100)))
        XCTAssertEqual(live.usage?.nodes, 1)
        XCTAssertNil(live.usage?.cpuFraction)
    }

    func testAFullSampleForgetsTheNodesItMisses() {
        var live = ClusterLive()
        live.record(sample(0, of: 2, counters("a", 0, 0), counters("b", 0, 0)))
        live.record(sample(15_000, of: 2, sampled: 1, counters("a", 10, 100)))
        XCTAssertEqual(live.usage?.nodes, 2)
        // "b" asked but down.
        live.record(sample(30_000, of: 2, sampled: 2, counters("a", 20, 200)))
        XCTAssertEqual(live.usage?.nodes, 1)
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, 0.1, accuracy: 1e-6)
    }

    func testAnEmptyPartialSampleKeepsTheValuesAndIsNoFailure() {
        var live = ClusterLive()
        live.record(sample(0, of: 3, counters("a", 0, 0), counters("b", 0, 0), counters("c", 0, 0)))
        for i in 1...clusterLiveMaxFailures {
            live.record(sample(Int64(i) * 15_000, of: 3, sampled: 1))
        }
        XCTAssertEqual(live.usage?.nodes, 3)
        XCTAssertTrue(live.isPartial)
    }

    func testFullSamplesMatchTheUnmergedUsage() {
        var live = ClusterLive()
        let first = sample(0, of: 2, counters("a", 0, 0), counters("b", 0, 0))
        let second = sample(5_000, of: 2, counters("a", 10, 100), counters("b", 90, 300))
        live.record(first)
        live.record(second)
        XCTAssertEqual(live.usage, ClusterUsage(previous: first, current: second))
    }
}
