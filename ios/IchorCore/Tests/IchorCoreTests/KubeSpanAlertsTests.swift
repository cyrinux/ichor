import XCTest
@testable import IchorCore

final class KubeSpanAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let key = "node-a|node-b"
    private let value = "critical|neverHandshaked|0|node-a|node-b"

    private func snap(_ issues: [String: String]?, watched: Bool = true) -> ClusterSnapshot {
        ClusterSnapshot(
            context: "lab", takenAt: now, nodes: ["192.0.2.20": NodeState(hostname: "node-a", health: .ready)],
            etcdChecked: true, kubespanWatched: watched, kubespanChecked: watched && issues != nil,
            kubespanIssues: issues ?? [:])
    }

    func testTheGoJSONDecodes() throws {
        let health = try TalosJSON.decode(
            KubeSpanMeshHealth.self,
            from: """
                {"context":"lab","checked":true,"partial":false,"links":3,"broken":1,"issues":{"\(key)":"\(value)"}}
                """)
        XCTAssertEqual(kubespanIssuesOf(health), [key: value])
        let issue = KubeSpanIssue(value: value)
        XCTAssertEqual(issue.reason, "neverHandshaked")
        XCTAssertEqual(issue.since, 0)
        XCTAssertEqual(issue.subject, "node-a ↔ node-b")
    }

    func testOncePerTransition() {
        let first = evaluate(previous: snap([:]), current: snap([key: value]), now: now)
        XCTAssertEqual(first.alerts.map(\.key), ["kubespan:\(key)"])
        XCTAssertEqual(first.alerts.first?.title, "Tunnel node-a ↔ node-b is down")

        let still = evaluate(previous: first.next, current: snap([key: value]), now: now)
        XCTAssertTrue(still.alerts.isEmpty)

        let back = evaluate(previous: still.next, current: snap([:]), now: now)
        XCTAssertEqual(back.alerts.map(\.title), ["Tunnel node-a ↔ node-b is back"])
        XCTAssertTrue(evaluate(previous: back.next, current: snap([:]), now: now).alerts.isEmpty)
    }

    func testNotCheckedKeepsTheKnownIssues() {
        XCTAssertNil(kubespanIssuesOf(KubeSpanMeshHealth(checked: false), known: [key: value]))
        let first = evaluate(previous: snap([:]), current: snap([key: value]), now: now)
        let blind = evaluate(previous: first.next, current: snap(nil), now: now)
        XCTAssertTrue(blind.alerts.isEmpty)
        XCTAssertEqual(blind.next.kubespanIssues, [key: value])
    }

    func testAPartialReadNeverSaysBack() {
        let other = "node-c|node-d"
        let partial = KubeSpanMeshHealth(checked: true, partial: true, issues: [other: value])
        XCTAssertEqual(kubespanIssuesOf(partial, known: [key: value]), [key: value, other: value])
        XCTAssertEqual(kubespanIssuesOf(KubeSpanMeshHealth(checked: true), known: [key: value]), [:])
    }

    func testTurningTheToggleOffForgetsWithoutABack() {
        let first = evaluate(previous: snap([:]), current: snap([key: value]), now: now)
        let off = evaluate(previous: first.next, current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertTrue(off.next.kubespanIssues.isEmpty)
    }

    func testAnOldSnapshotDecodes() throws {
        let json = #"{"context":"lab","takenAt":0,"nodes":{}}"#
        let snapshot = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(json.utf8))
        XCTAssertFalse(snapshot.kubespanWatched)
        XCTAssertTrue(snapshot.kubespanIssues.isEmpty)
    }

    func testItOpensTheKubeSpanScreen() {
        XCTAssertEqual(ShareTarget.forAlert(key: "kubespan:\(key)", snapshot: snap([key: value])), .screen(.kubespan))
    }

    func testTheStatusAndGlanceCountIt() {
        let down = snap([key: value])
        let summary = clusterStatusSummary(down, now: now)
        XCTAssertEqual(summary?.parts, [.nodes(1), .tunnelsDown(1), .etcdHealthy])
        XCTAssertEqual(summary?.healthy, false)
        XCTAssertEqual(glanceItems(down), [.tunnelsDown(1), .etcdNoAlarms])
        XCTAssertEqual(GlanceItem.tunnelsDown(1).tone, .problem)
        XCTAssertEqual(glanceItems(snap(nil)).first, .allReady)
    }
}
