import XCTest
@testable import IchorCore

final class ClusterUsageTests: XCTestCase {
    private let gib: UInt64 = 1 << 30

    private func counters(_ node: String, _ busy: Double, _ total: Double, mem: UInt64? = nil, free: UInt64? = nil) -> NodeCounters {
        NodeCounters(node: node, cpuBusy: busy, cpuTotal: total, cpuCount: 4, memTotal: mem ?? 8 * gib, memAvailable: free ?? 4 * gib)
    }

    private func sample(_ at: Int64, _ nodes: NodeCounters...) -> ClusterStatsSample {
        ClusterStatsSample(at: at, nodes: nodes)
    }

    func testCpuIsBusyOverTotalTimeAcrossNodes() {
        let prev = sample(0, counters("a", 0, 0), counters("b", 0, 0))
        let cur = sample(5_000, counters("a", 10, 100), counters("b", 90, 300))
        // Bigger nodes weigh more: (10 + 90) / (100 + 300), not the mean of 10% and 30%.
        XCTAssertEqual(ClusterUsage(previous: prev, current: cur).cpuFraction ?? -1, 0.25, accuracy: 1e-6)
    }

    func testMemoryComesFromTheLatestSample() {
        let prev = sample(0, counters("a", 0, 0))
        let cur = sample(5_000, counters("a", 1, 2, mem: 16 * gib, free: 4 * gib))
        let usage = ClusterUsage(previous: prev, current: cur)
        XCTAssertEqual(usage.memTotal, 16 * gib)
        XCTAssertEqual(usage.memAvailable, 4 * gib)
        XCTAssertEqual(usage.memUsedFraction ?? -1, 0.75, accuracy: 1e-6)
    }

    func testNodesMissingFromEitherSampleDoNotCountForCpu() {
        let prev = sample(0, counters("a", 0, 0), counters("gone", 0, 0))
        let cur = sample(5_000, counters("a", 50, 100), counters("new", 1_000, 1_000))
        XCTAssertEqual(ClusterUsage(previous: prev, current: cur).cpuFraction ?? -1, 0.5, accuracy: 1e-6)
    }

    func testRebootedNodeIsSkippedRatherThanSkewingTheAverage() {
        let prev = sample(0, counters("a", 0, 0), counters("rebooted", 5_000, 9_000))
        let cur = sample(5_000, counters("a", 20, 100), counters("rebooted", 1, 10))
        XCTAssertEqual(ClusterUsage(previous: prev, current: cur).cpuFraction ?? -1, 0.2, accuracy: 1e-6)
    }

    func testNoElapsedCpuTimeGivesNoCpu() {
        let same = sample(0, counters("a", 10, 100))
        XCTAssertNil(ClusterUsage(previous: same, current: same).cpuFraction)
    }

    func testSamplesTooFarApartGiveNoCpu() {
        let prev = sample(0, counters("a", 0, 0))
        let cur = sample(maxSampleGapMillis + 1, counters("a", 50, 100))
        XCTAssertNil(ClusterUsage(previous: prev, current: cur).cpuFraction)
    }

    func testANodeWithoutMemoryDropsTheMemoryRatherThanUnderstateIt() {
        let cur = sample(0, counters("a", 1, 2), counters("b", 1, 2, mem: 0, free: 0))
        let usage = ClusterUsage(previous: nil, current: cur)
        XCTAssertEqual(usage.nodes, 2)
        XCTAssertNil(usage.memUsedFraction)
    }

    func testFirstSampleAloneHasMemoryButNoCpu() {
        let usage = ClusterUsage(previous: nil, current: sample(0, counters("a", 10, 100)))
        XCTAssertNil(usage.cpuFraction)
        XCTAssertEqual(usage.memUsedFraction ?? -1, 0.5, accuracy: 1e-6)
    }

    func testHistoryKeepsTheNewestPoints() {
        XCTAssertEqual(appendHistory([0.1, 0.2], 0.3, max: 2), [0.2, 0.3])
        XCTAssertEqual(appendHistory([0.1], nil, max: 2), [0.1])
    }

    func testDecodesTheGoCoreJSON() throws {
        let json = #"{"at":1800000000000,"nodes":[{"node":"10.0.0.1","cpuBusy":1.5,"cpuTotal":10,"cpuCount":4,"memTotal":8589934592,"memAvailable":4294967296}]}"#
        let decoded = try JSONDecoder().decode(ClusterStatsSample.self, from: Data(json.utf8))
        XCTAssertEqual(decoded, sample(1_800_000_000_000, counters("10.0.0.1", 1.5, 10)))
        XCTAssertEqual(try JSONDecoder().decode(ClusterStatsSample.self, from: Data(#"{"at":1,"nodes":null}"#.utf8)).nodes, [])
    }

    func testLiveTakesTheSecondSampleSoonThenTheClusterCadence() {
        var live = ClusterLive()
        XCTAssertNil(live.usage)
        live.record(sample(0, counters("a", 0, 0)))
        XCTAssertNil(live.usage?.cpuFraction)
        XCTAssertNotNil(live.usage?.memUsedFraction)
        XCTAssertEqual(live.nextDelay(nodeCount: 3), clusterFirstDeltaSeconds)
        live.record(sample(1_000, counters("a", 10, 100)))
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, 0.1, accuracy: 1e-6)
        XCTAssertEqual(live.cpuHistory.count, 1)
        XCTAssertEqual(live.nextDelay(nodeCount: 3), 5)
        XCTAssertEqual(live.nextDelay(nodeCount: 30), 15)
    }

    func testLiveResumesWithItsValuesAndASoonSample() {
        var live = ClusterLive()
        live.record(sample(0, counters("a", 0, 0)))
        live.record(sample(5_000, counters("a", 20, 100)))
        live.recordFailure()
        live.recordFailure()
        live.resume()
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, 0.2, accuracy: 1e-6)
        // Earlier failures no longer count: two more keep the values.
        live.recordFailure()
        live.recordFailure()
        XCTAssertNotNil(live.usage)
        live.record(sample(60_000, counters("a", 40, 200)))
        XCTAssertEqual(live.nextDelay(nodeCount: 3), clusterFirstDeltaSeconds)
        // Too long since the last sample for a delta: the last CPU stays.
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, 0.2, accuracy: 1e-6)
    }

    func testLiveKeepsTheLastCpuThroughASampleWithout() {
        var live = ClusterLive()
        live.record(sample(0, counters("a", 0, 0)))
        live.record(sample(5_000, counters("a", 30, 100)))
        // Rebooted: counters went backwards, no CPU this time.
        live.record(sample(10_000, counters("a", 1, 10)))
        XCTAssertEqual(live.usage?.cpuFraction ?? -1, 0.3, accuracy: 1e-6)
        XCTAssertEqual(live.cpuHistory.count, 1)
    }

    func testLiveHistoryIsCapped() {
        var live = ClusterLive()
        for i in 0...(clusterHistoryPoints + 5) {
            live.record(sample(Int64(i) * 5_000, counters("a", Double(i * 10), Double(i * 100))))
        }
        XCTAssertEqual(live.cpuHistory.count, clusterHistoryPoints)
    }

    func testLiveFallsBackAfterRepeatedFailures() {
        var live = ClusterLive()
        live.record(sample(0, counters("a", 0, 0)))
        live.recordFailure()
        live.recordFailure()
        XCTAssertNotNil(live.usage)
        // No node answering is a failure too.
        live.record(sample(5_000))
        XCTAssertNil(live.usage)
        XCTAssertEqual(live, ClusterLive())
    }
}
