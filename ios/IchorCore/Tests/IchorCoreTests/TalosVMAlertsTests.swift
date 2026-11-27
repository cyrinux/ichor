import XCTest

@testable import IchorCore

final class TalosVMAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let node = "192.0.2.20"
    private var key: String { "\(node)|vm|app" }
    private let value = "critical|vmError|node-a|app|domain name is occupied by another VM"

    private func snap(_ issues: [String: String]?, watched: Bool = true) -> ClusterSnapshot {
        ClusterSnapshot(
            context: "lab", takenAt: now, nodes: [node: NodeState(hostname: "node-a", health: .ready)],
            etcdChecked: true, vmWatched: watched, vmChecked: watched && issues != nil, vmIssues: issues ?? [:])
    }

    func testTheGoJSONDecodes() throws {
        let report = try TalosJSON.decode(
            TalosVMProblemReport.self,
            from: """
                {"context":"lab","nodes":[{"node":"\(node)","hostname":"node-a","supported":true,"vms":[],
                "errors":{},"problems":[{"node":"\(node)","vm":"app","kind":"vmError",
                "detail":"domain name is occupied by another VM"}]}]}
                """)
        XCTAssertEqual(talosVMIssuesOf(report), [key: value])
        let issue = TalosVMIssue(value: value)
        XCTAssertEqual(issue.kind, TalosVMIssue.error)
        XCTAssertEqual(issue.hostname, "node-a")
        XCTAssertEqual(issue.vm, "app")
    }

    func testASeparatorInTheTextNeverShiftsTheFields() {
        let problem = TalosVMProblem(node: node, vm: "app", kind: TalosVMIssue.down, detail: "a|b")
        let report = TalosVMProblemReport(
            nodes: [TalosVMProblemNode(node: node, hostname: "node-a", problems: [problem])])
        let issue = TalosVMIssue(value: talosVMIssuesOf(report)[key] ?? "")
        XCTAssertEqual(issue.vm, "app")
        XCTAssertEqual(issue.detail, "a/b")
    }

    func testAnUnreadNodeKeepsItsKnownIssues() {
        let known = [key: value]
        let failed = TalosVMProblemReport(nodes: [TalosVMProblemNode(node: node, error: "timeout")])
        XCTAssertEqual(talosVMIssuesOf(failed, known: known), known)
        let refused = TalosVMProblemReport(nodes: [TalosVMProblemNode(node: node, errors: ["vms": "denied"])])
        XCTAssertEqual(talosVMIssuesOf(refused, known: known), known)
        let fine = TalosVMProblemReport(nodes: [TalosVMProblemNode(node: node, errors: ["libraries": "x"])])
        XCTAssertEqual(talosVMIssuesOf(fine, known: known), [:])
    }

    func testOncePerTransition() {
        let first = evaluate(previous: snap([:]), current: snap([key: value]), now: now)
        XCTAssertEqual(first.alerts.map(\.key), ["vm:\(key)"])
        XCTAssertEqual(first.alerts.first?.title, "app failed on node-a")

        let still = evaluate(previous: first.next, current: snap([key: value]), now: now)
        XCTAssertTrue(still.alerts.isEmpty)

        let back = evaluate(previous: still.next, current: snap([:]), now: now)
        XCTAssertEqual(back.alerts.map(\.title), ["app on node-a is back to normal"])
        XCTAssertTrue(evaluate(previous: back.next, current: snap([:]), now: now).alerts.isEmpty)
    }

    func testNotCheckedKeepsTheKnownIssues() {
        let first = evaluate(previous: snap([:]), current: snap([key: value]), now: now)
        let blind = evaluate(previous: first.next, current: snap(nil), now: now)
        XCTAssertTrue(blind.alerts.isEmpty)
        XCTAssertEqual(blind.next.vmIssues, [key: value])
        XCTAssertEqual(knownTalosVMIssues(blind.next, context: "lab"), [key: value])
        XCTAssertEqual(knownTalosVMIssues(blind.next, context: "other"), [:])
    }

    func testTurningTheToggleOffForgetsWithoutABack() {
        let first = evaluate(previous: snap([:]), current: snap([key: value]), now: now)
        let off = evaluate(previous: first.next, current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertTrue(off.next.vmIssues.isEmpty)
    }

    func testAnOldSnapshotDecodes() throws {
        let json = #"{"context":"lab","takenAt":0,"nodes":{}}"#
        let snapshot = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(json.utf8))
        XCTAssertFalse(snapshot.vmWatched)
        XCTAssertTrue(snapshot.vmIssues.isEmpty)
    }

    func testItOpensTheTalosVMList() {
        XCTAssertEqual(ShareTarget.forAlert(key: "vm:\(key)", snapshot: snap([key: value])), .screen(.talosVMs))
        let alert = Alert(key: "vm:\(key)", title: "", text: "", problem: true)
        XCTAssertEqual(alertRuleKind(key: alert.key), "vm")
        XCTAssertEqual(alertRuleSeverity(alert, snapshot: nil, now: now), alertSeverityCritical)
    }

    func testTheStatusAndGlanceCountIt() {
        let down = snap([key: value])
        let summary = clusterStatusSummary(down, now: now)
        XCTAssertEqual(summary?.parts, [.nodes(1), .vmsDown(1), .etcdHealthy])
        XCTAssertEqual(summary?.healthy, false)
        XCTAssertEqual(glanceItems(down), [.vmsDown(1), .etcdNoAlarms])
        XCTAssertEqual(GlanceItem.vmsDown(1).tone, .problem)
        XCTAssertEqual(glanceItems(snap(nil)).first, .allReady)
    }

    func testTheFleetEntryCarriesIt() {
        let entry = FleetEntry(
            key: "k", name: "lab", context: "lab", kube: false, snapshot: snap([key: value]), failures: 0,
            masked: false)
        XCTAssertTrue(entry.vmChecked)
        XCTAssertEqual(entry.vmIssues, [key: value])
    }
}
