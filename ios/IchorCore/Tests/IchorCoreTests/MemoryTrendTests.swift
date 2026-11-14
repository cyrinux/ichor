import XCTest
@testable import IchorCore

final class MemoryTrendTests: XCTestCase {
    private func node(_ daysToCritical: Double?, high: Bool = true, slope: Double = 1.8) -> NodeMemoryForecast {
        NodeMemoryForecast(
            node: "10.0.0.2", hostname: "worker-1", usedPercent: 84.6, slopePerDay: slope,
            daysToFull: daysToCritical.map { $0 + 8 }, daysToCritical: daysToCritical,
            confidence: high ? VolumeForecast.high : "low")
    }

    func testDecodesTheGoShape() throws {
        let json = #"""
            {"nodes":[{"node":"10.0.0.2","hostname":"worker-1","usedPercent":71.2,"latestAt":1780000000000,
             "slopePerDay":1.8,"daysToCritical":6.1,"daysToFull":16,"confidence":"high","points":120,"spanHours":166}]}
            """#
        let forecast = try TalosJSON.decode(HistoryMemoryForecast.self, from: json)
        let row = try XCTUnwrap(forecast.node("10.0.0.2"))
        XCTAssertEqual(row.hostname, "worker-1")
        XCTAssertEqual(row.line, .projected(full: .days(16), critical: .days(6)))
        XCTAssertEqual(try TalosJSON.decode(HistoryMemoryForecast.self, from: "{}"), HistoryMemoryForecast())
    }

    func testFlatAndNoisyNodes() {
        XCTAssertNil(node(nil, high: false, slope: 0).line)
        XCTAssertEqual(node(4, high: false).line, .growing("1.8"))
    }

    func testOpensWithinThreeDaysAndClosesPastSeven() throws {
        let opened = evaluateMemoryTrends(HistoryMemoryForecast(nodes: [node(2.4)]), open: [:], hostnames: [:])
        XCTAssertEqual(Array(opened.open.keys), ["10.0.0.2"])
        let alert = try XCTUnwrap(opened.alerts.first)
        XCTAssertEqual(alert.key, "memory:10.0.0.2:trend")
        XCTAssertTrue(alert.problem)
        XCTAssertEqual(alert.title, "worker-1 memory critical in ~2 days")
        XCTAssertTrue(StorageTrendIssue(value: opened.open["10.0.0.2"] ?? "").critical)

        let kept = evaluateMemoryTrends(HistoryMemoryForecast(nodes: [node(5)]), open: opened.open, hostnames: [:])
        XCTAssertEqual(kept.open.keys.sorted(), ["10.0.0.2"])
        XCTAssertTrue(kept.alerts.isEmpty)

        let closed = evaluateMemoryTrends(HistoryMemoryForecast(nodes: [node(9)]), open: kept.open, hostnames: [:])
        XCTAssertTrue(closed.open.isEmpty)
        XCTAssertEqual(closed.alerts.map(\.problem), [false])
        XCTAssertEqual(closed.alerts.first?.title, "worker-1 memory no longer growing fast")
    }

    func testALowFitNeverOpens() {
        let out = evaluateMemoryTrends(HistoryMemoryForecast(nodes: [node(1, high: false)]), open: [:], hostnames: [:])
        XCTAssertTrue(out.alerts.isEmpty)
    }

    func testRuleKindSeverityAndLink() {
        let key = memoryTrendAlertKey("fd00::7")
        XCTAssertEqual(key, "memory:fd00::7:trend")
        XCTAssertEqual(alertRuleKind(key: key), "memory")
        XCTAssertEqual(memoryTrendNode(subject: "fd00::7:trend"), "fd00::7")
        var snapshot = ClusterSnapshot(context: "lab", takenAt: Date(), nodes: [:])
        snapshot.memoryTrends["fd00::7"] =
            StorageTrendIssue(hostname: "cp-1", name: "memory", critical: true, days: 2, slopePerDay: 2, usedPercent: 85)
            .value
        let alert = Alert(key: key, title: "", text: "", problem: true)
        XCTAssertEqual(alertRuleSeverity(alert, snapshot: snapshot, now: Date()), alertSeverityCritical)
        XCTAssertEqual(
            ShareTarget.forAlert(key: key, snapshot: snapshot), .node(address: "fd00::7", hostname: "cp-1", tab: ""))
    }
}
