import XCTest
@testable import TalosViewerCore

final class MonitorTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func snap(_ nodes: [(String, NodeHealth)], context: String = "lab", alarms: [String] = [],
                      cert: Int64? = nil, lastWarn: Int64 = -1) -> ClusterSnapshot {
        ClusterSnapshot(
            context: context, takenAt: now,
            nodes: Dictionary(uniqueKeysWithValues: nodes.map { ($0.0, NodeState(hostname: "host-\($0.0)", health: $0.1, reason: $0.1 == .ready ? "" : "why")) }),
            etcdAlarms: alarms, etcdChecked: true,
            certNotAfter: cert ?? Int64(now.timeIntervalSince1970) + 365 * 86_400, lastCertWarnDay: lastWarn)
    }

    func testFirstSnapshotIsBaseline() {
        XCTAssertTrue(evaluate(previous: nil, current: snap([("a", .unreachable)]), now: now).alerts.isEmpty)
    }

    func testOnlyTransitionsAlert() {
        let alerts = evaluate(previous: snap([("a", .ready), ("b", .notReady), ("c", .ready)]),
                              current: snap([("a", .unreachable), ("b", .ready), ("c", .ready)]), now: now).alerts
        XCTAssertEqual(alerts.map(\.key), ["node:a", "node:b"])
        XCTAssertEqual(alerts[0].title, "host-a is unreachable")
        XCTAssertEqual(alerts[1].title, "host-b is ready again")
    }

    func testSteadyStateAndContextSwitchAreSilent() {
        let s = snap([("a", .notReady)])
        XCTAssertTrue(evaluate(previous: s, current: s, now: now).alerts.isEmpty)
        XCTAssertTrue(evaluate(previous: snap([("a", .ready)], context: "one"),
                               current: snap([("a", .unreachable)], context: "two"), now: now).alerts.isEmpty)
    }

    func testNewEtcdAlarm() {
        let alerts = evaluate(previous: snap([("a", .ready)]), current: snap([("a", .ready)], alarms: ["beef:NOSPACE"]), now: now).alerts
        XCTAssertEqual(alerts.map(\.key), ["etcd:beef:NOSPACE"])
    }

    func testCertWarnsOncePerDay() {
        let soon = Int64(now.timeIntervalSince1970) + 5 * 86_400
        let first = evaluate(previous: snap([("a", .ready)]), current: snap([("a", .ready)], cert: soon), now: now)
        XCTAssertEqual(first.alerts.map(\.key), ["cert"])
        let sameDay = evaluate(previous: first.next, current: snap([("a", .ready)], cert: soon), now: now.addingTimeInterval(3_600))
        XCTAssertTrue(sameDay.alerts.isEmpty)
        let nextDay = evaluate(previous: sameDay.next, current: snap([("a", .ready)], cert: soon), now: now.addingTimeInterval(86_400))
        XCTAssertEqual(nextDay.alerts.map(\.key), ["cert"])
    }

    func testSnapshotRoundTripsAsJSON() throws {
        let s = snap([("a", .ready), ("b", .unreachable)])
        let data = try JSONEncoder().encode(s)
        XCTAssertEqual(try JSONDecoder().decode(ClusterSnapshot.self, from: data), s)
        XCTAssertEqual(s.readyCount, 1)
        XCTAssertEqual(s.unreachableCount, 1)
    }
}
