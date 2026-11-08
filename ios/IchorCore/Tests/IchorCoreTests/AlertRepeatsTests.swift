import XCTest
@testable import IchorCore

/// Same cases as Android's AlertRepeatsTest and AlertAcksTest.
final class AlertRepeatsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let minute: Int64 = 60_000

    private func snapshot(_ nodes: [String: NodeHealth], context: String = "prod") -> ClusterSnapshot {
        ClusterSnapshot(
            context: context, takenAt: now,
            nodes: nodes.mapValues { NodeState(hostname: "host", health: $0) })
    }

    private var nowMillis: Int64 { now.epochMillis }

    func testFiringAlertsAreTheProblemsASnapshotStillHolds() {
        var snap = snapshot(["10.0.0.1": .ready, "10.0.0.2": .notReady, "10.0.0.3": .unreachable])
        snap.etcdChecked = true
        snap.etcdAlarms = ["m1:NOSPACE"]
        snap.amWatched = true
        snap.amChecked = true
        snap.amIssues = ["f1": AMIssue(severity: "critical", alertname: "DiskFull").value]
        snap.dataWatched = true
        snap.dataChecked = true
        // A warning already notified (kept in the issues) repeats like a critical one would.
        snap.dataIssues = ["longhorn|vol-1": dataWarning]
        let keys = Set(firingAlerts(snap, now: now).map(\.key))
        XCTAssertEqual(
            keys, ["node:10.0.0.2", "node:10.0.0.3", "etcd:m1:NOSPACE", "am:f1", "data:longhorn|vol-1"])
        XCTAssertTrue(firingAlerts(snap, now: now).allSatisfy(\.problem))
    }

    func testNothingFiresWhereNoNodeAnswered() {
        XCTAssertTrue(ongoingAlerts(snapshot(["a": .unreachable]), changed: [], now: now).isEmpty)
    }

    func testAlertTimesFollowTheFiringAlerts() {
        var previous = snapshot(["a": .notReady, "b": .notReady])
        previous.firstSeen = ["node:a": nowMillis - 40 * minute, "node:b": nowMillis - 20 * minute]
        previous.lastNotified = ["node:a": nowMillis - 16 * minute, "node:b": nowMillis - 20 * minute]
        let next = withAlertTimes(
            previous: previous, next: snapshot(["a": .unreachable, "b": .ready, "c": .notReady]), now: now)
        XCTAssertEqual(next.firstSeen, ["node:a": nowMillis - 40 * minute, "node:c": nowMillis])
        XCTAssertEqual(next.lastNotified, ["node:a": nowMillis - 16 * minute])
    }

    func testAnotherContextIsABaseline() {
        var previous = snapshot(["a": .notReady], context: "old")
        previous.firstSeen = ["node:a": 1]
        previous.lastNotified = ["node:a": 1]
        let next = withAlertTimes(previous: previous, next: snapshot(["a": .notReady]), now: now)
        XCTAssertEqual(next.firstSeen, ["node:a": nowMillis])
        XCTAssertTrue(next.lastNotified.isEmpty)
    }

    func testOnlyPostedUnchangedAlertsAreOngoing() {
        var snap = snapshot(["a": .notReady, "b": .notReady, "c": .notReady])
        snap.firstSeen = ["node:a": 1, "node:b": 1, "node:c": 1]
        // c was never posted: it does not repeat; b changed this run: posted as it is now.
        snap.lastNotified = ["node:a": 1, "node:b": 1]
        XCTAssertEqual(ongoingAlerts(snap, changed: ["node:b"], now: now).map(\.key), ["node:a"])
    }

    func testPostedAlertsSetTheirLastNotifiedTime() {
        var snap = snapshot(["a": .notReady, "b": .ready])
        snap.firstSeen = ["node:a": 1]
        let next = withNotified(snap, keys: ["node:a", "node:b"], now: now)
        XCTAssertEqual(next.lastNotified, ["node:a": nowMillis])
    }

    func testStillFiringMinutesAreWholeAndAtLeastOne() {
        XCTAssertEqual(stillFiringMinutes(firstSeen: nowMillis - 25 * minute - 30_000, now: now), 25)
        XCTAssertEqual(stillFiringMinutes(firstSeen: nowMillis - 10_000, now: now), 1)
        XCTAssertEqual(stillFiringMinutes(firstSeen: 0, now: now), 1)
    }

    func testOldSnapshotsDecodeWithoutAlertTimes() throws {
        let json = #"{"context":"prod","takenAt":0,"nodes":{}}"#
        let snap = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(json.utf8))
        XCTAssertTrue(snap.firstSeen.isEmpty)
        XCTAssertTrue(snap.lastNotified.isEmpty)
    }

    // MARK: - Plan

    func testOnlyARepeatDecisionPostsAnOngoingAlertAgain() {
        let ongoing = AlertInput(cluster: "fp", key: "node:a", kind: "node", problem: true, ongoing: true)
        let notDue = AlertInput(cluster: "fp", key: "node:b", kind: "node", problem: true, ongoing: true)
        let fresh = AlertInput(cluster: "fp", key: "node:c", kind: "node", problem: true)
        let decisions = AlertDecisions(decisions: [
            AlertDecision(cluster: "fp", key: "node:a", action: alertActionRepeat, reason: "repeat", repeats: true),
            AlertDecision(cluster: "fp", key: "node:b", action: alertActionMute, reason: "notDue"),
            AlertDecision(cluster: "fp", key: "node:c", action: alertActionNotify, repeats: true),
        ])
        let plan = alertRulePlan(inputs: [ongoing, notDue, fresh], decisions: decisions)
        XCTAssertEqual(plan.outcomes, [.repeat, .drop, .notify])
        XCTAssertEqual(plan.repeats, [true, false, true])
        // Without decisions an ongoing alert is never a fallback post.
        XCTAssertEqual(alertRulePlan(inputs: [ongoing, fresh], decisions: nil).outcomes, [.drop, .notify])
    }

    func testRulesKeepTheRepeat() throws {
        let rules = AlertRules(default: AlertRule(repeatMinutes: 15))
        XCTAssertEqual(rules.json, #"{"default":{"repeatMinutes":15}}"#)
        XCTAssertEqual(AlertRules.decode(Data(rules.json.utf8)), rules)
        XCTAssertFalse(AlertRule(repeatMinutes: 0).isEmpty)
    }

    // MARK: - Acknowledgements

    func testAcknowledgementsEndWhenTheAlertStopsFiring() {
        let acks = AlertAcks()
            .acknowledging(cluster: "fp", key: "node:a", now: now)
            .acknowledging(cluster: "fp", key: "node:b", now: now)
            .acknowledging(cluster: "fl", key: "node:a", now: now)
            .acknowledging(cluster: "", key: "node:a", now: now)
        let kept = acks.keepingFiring(cluster: "fp", firing: ["node:b"])
        XCTAssertFalse(kept.isAcknowledged(cluster: "fp", key: "node:a"))
        XCTAssertTrue(kept.isAcknowledged(cluster: "fp", key: "node:b"))
        XCTAssertTrue(kept.isAcknowledged(cluster: "fl", key: "node:a"))
        XCTAssertFalse(kept.keeping(clusters: ["fp"]).isAcknowledged(cluster: "fl", key: "node:a"))
        XCTAssertTrue(acks.keepingFiring(cluster: "fp", firing: []).at["fp"] == nil)
    }

    func testAcknowledgeComesBeforeSnooze() {
        XCTAssertEqual(withAcknowledge([.wake, .reboot, .snooze]), [.wake, .reboot, .acknowledge, .snooze])
        XCTAssertEqual(withAcknowledge([]), [])
        XCTAssertFalse(AlertAction.acknowledge.changesCluster)
        XCTAssertEqual(
            alertCategory(kind: "node", actions: [.reboot, .acknowledge, .snooze], hideDetails: false),
            "alert.node.reboot-acknowledge")
    }
}
