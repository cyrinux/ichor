import XCTest
@testable import IchorCore

final class WidgetGlanceTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func snap(_ health: [NodeHealth], alarms: [String] = [], etcdChecked: Bool = true,
                      age: TimeInterval = 0) -> ClusterSnapshot {
        var nodes: [String: NodeState] = [:]
        for (i, h) in health.enumerated() { nodes["10.0.0.\(i)"] = NodeState(hostname: "n\(i)", health: h) }
        return ClusterSnapshot(context: "lab", takenAt: now.addingTimeInterval(-age), nodes: nodes,
                               etcdAlarms: alarms, etcdChecked: etcdChecked)
    }

    func testAllReadyWithHealthyEtcd() {
        XCTAssertEqual(glanceItems(snap([.ready, .ready])), [.allReady, .etcdNoAlarms])
    }

    func testNotReadyThenEtcdAlarms() {
        XCTAssertEqual(glanceItems(snap([.ready, .notReady], alarms: ["1:NOSPACE", "2:NOSPACE"])),
                       [.notReady(1), .etcdAlarms(2)])
    }

    func testProblemsComeFirstAndCapAtTwo() {
        XCTAssertEqual(glanceItems(snap([.notReady, .unreachable, .unreachable])), [.notReady(1), .unreachable(2)])
    }

    func testEtcdOmittedWhenNotChecked() {
        XCTAssertEqual(glanceItems(snap([.unreachable], etcdChecked: false)), [.unreachable(1)])
    }

    func testTones() {
        XCTAssertEqual(GlanceItem.notReady(1).tone, .warning)
        XCTAssertEqual(GlanceItem.unreachable(1).tone, .problem)
        XCTAssertEqual(GlanceItem.etcdAlarms(1).tone, .problem)
        XCTAssertEqual(GlanceItem.allReady.tone, .ok)
        XCTAssertEqual(GlanceItem.etcdNoAlarms.tone, .ok)
    }

    func testStaleAfterOneHour() {
        XCTAssertFalse(isGlanceStale(snap([.ready], age: 3600), now: now))
        XCTAssertTrue(isGlanceStale(snap([.ready], age: 3601), now: now))
    }
}
