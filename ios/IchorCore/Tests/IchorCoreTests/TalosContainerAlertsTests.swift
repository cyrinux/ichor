import XCTest
@testable import IchorCore

final class TalosContainerAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let restartingKey = "192.0.2.20|sensor-agent"
    private let restarting = "warning|backoff|12|1|edge-1|sensor-agent"
    private let failedKey = "192.0.2.21|backup"
    private let failed = "critical|failed|0|3|edge-2|backup"

    private func snap(_ issues: [String: String]?, watched: Bool = true) -> ClusterSnapshot {
        ClusterSnapshot(
            context: "lab", takenAt: now, nodes: ["192.0.2.20": NodeState(hostname: "edge-1", health: .ready)],
            etcdChecked: true, talosContainerWatched: watched, talosContainerChecked: watched && issues != nil,
            talosContainerIssues: issues ?? [:])
    }

    func testTheGoJSONDecodes() throws {
        let health = try TalosJSON.decode(
            TalosContainersHealth.self,
            from: """
                {"context":"lab","checked":true,"unread":[],"issues":{"\(restartingKey)":"\(restarting)"}}
                """)
        XCTAssertEqual(talosContainerIssuesOf(health), [restartingKey: restarting])
        let issue = TalosContainerIssue(value: restarting)
        XCTAssertEqual(issue.restartCount, 12)
        XCTAssertEqual(issue.exitCode, 1)
        XCTAssertEqual(issue.hostname, "edge-1")
        XCTAssertEqual(issue.name, "sensor-agent")
        XCTAssertFalse(issue.failed)
        XCTAssertTrue(TalosContainerIssue(value: failed).failed)
    }

    func testAFailedContainerAlertsAtOnceAndWhenRunningAgain() {
        let first = evaluate(previous: snap([:]), current: snap([failedKey: failed]), now: now)
        XCTAssertEqual(first.alerts.map(\.key), ["taloscontainer:\(failedKey)"])
        XCTAssertEqual(first.alerts.first?.title, "backup on edge-2 is failing")
        XCTAssertEqual(first.alerts.first?.text, "Failed with exit code 3: Talos does not start it again.")

        XCTAssertTrue(evaluate(previous: first.next, current: snap([failedKey: failed]), now: now).alerts.isEmpty)

        let back = evaluate(previous: first.next, current: snap([:]), now: now)
        XCTAssertEqual(back.alerts.map(\.title), ["backup on edge-2 is running again"])
    }

    func testARestartingContainerAlertsOnItsSecondCheck() {
        let once = evaluate(previous: snap([:]), current: snap([restartingKey: restarting]), now: now)
        XCTAssertTrue(once.alerts.isEmpty)
        let twice = evaluate(previous: once.next, current: snap([restartingKey: restarting]), now: now)
        XCTAssertEqual(twice.alerts.map(\.key), ["taloscontainer:\(restartingKey)"])
        XCTAssertEqual(twice.alerts.first?.text, "Restarted 12 times, last exit code 1.")
    }

    func testAnUnreadNodeKeepsItsIssuesAndNothingReadIsUnknown() {
        let known = [restartingKey: restarting, failedKey: failed]
        let partial = TalosContainersHealth(context: "lab", checked: true, unread: ["192.0.2.21"])
        XCTAssertEqual(talosContainerIssuesOf(partial, known: known), [failedKey: failed])
        XCTAssertNil(talosContainerIssuesOf(TalosContainersHealth(context: "lab"), known: known))
    }

    func testTurningTheToggleOffForgetsWithoutABack() {
        let first = evaluate(previous: snap([:]), current: snap([failedKey: failed]), now: now)
        let off = evaluate(previous: first.next, current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertTrue(off.next.talosContainerIssues.isEmpty)
    }

    func testAnOldSnapshotDecodes() throws {
        let json = #"{"context":"lab","takenAt":0,"nodes":{}}"#
        let snapshot = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(json.utf8))
        XCTAssertFalse(snapshot.talosContainerWatched)
        XCTAssertTrue(snapshot.talosContainerIssues.isEmpty)
    }

    func testItOpensTheNode() {
        XCTAssertEqual(
            ShareTarget.forAlert(key: "taloscontainer:\(failedKey)", snapshot: snap([failedKey: failed])),
            .node(address: "192.0.2.21", hostname: "edge-2", tab: ""))
    }

    func testItCanBeSnoozedAndKeepsItsSeverity() throws {
        let first = evaluate(previous: snap([:]), current: snap([failedKey: failed]), now: now)
        let alert = try XCTUnwrap(first.alerts.first)
        XCTAssertEqual(alertActions(key: alert.key, problem: true, canWake: false), [.snooze])
        XCTAssertEqual(alertRuleSeverity(alert, snapshot: first.next, now: now), alertSeverityCritical)
    }
}
