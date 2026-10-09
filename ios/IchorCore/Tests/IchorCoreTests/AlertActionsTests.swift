import XCTest
@testable import IchorCore

final class AlertActionsTests: XCTestCase {
    func testActionsPerAlert() {
        XCTAssertEqual(alertActions(key: "node:10.0.0.2", problem: true, canWake: false), [.reboot, .snooze])
        XCTAssertEqual(alertActions(key: "node:10.0.0.2", problem: true, canWake: true), [.wake, .reboot, .snooze])
        XCTAssertEqual(alertActions(key: "etcd:abc:NOSPACE", problem: true, canWake: true), [.snooze])
        XCTAssertEqual(alertActions(key: "data:longhorn|pvc-1", problem: true, canWake: false), [.snooze])
        XCTAssertEqual(alertActions(key: "gitops:argocd|argocd/shop", problem: true, canWake: false), [.sync, .snooze])
        XCTAssertEqual(alertActions(key: "gitops:flux|HelmRelease flux-system/ingress", problem: true, canWake: false),
                       [.reconcile, .snooze])
        XCTAssertEqual(alertActions(key: "checkup:pods|crash|shop/web", problem: true, canWake: false), [.snooze])
        XCTAssertEqual(alertActions(key: "am:f00d", problem: true, canWake: false), [.silence, .snooze])
        XCTAssertEqual(alertActions(key: "cert", problem: true, canWake: false), [.snooze])
        XCTAssertEqual(alertActions(key: "other:x", problem: true, canWake: false), [])
    }

    func testResolvedAlertsGetNoActions() {
        for key in ["node:10.0.0.2", "data:garage|g1", "gitops:argocd|argocd/shop", "am:f00d", "checkup:a|b|c"] {
            XCTAssertEqual(alertActions(key: key, problem: false, canWake: true), [], key)
        }
    }

    func testOnlyClusterChangesAreConfirmedInTheApp() {
        XCTAssertEqual(AlertAction.allCases.filter(\.changesCluster), [.reboot, .sync, .reconcile, .silence])
    }

    func testCategories() {
        XCTAssertEqual(alertCategory(kind: "node", actions: [], hideDetails: false), "alert.node")
        XCTAssertEqual(alertCategory(kind: "node", actions: [.reboot, .snooze], hideDetails: false), "alert.node.reboot")
        XCTAssertEqual(alertCategory(kind: "node", actions: [.wake, .reboot, .snooze], hideDetails: true), "alert.node.wake-reboot.private")
        XCTAssertEqual(alertCategory(kind: "etcd", actions: [.snooze], hideDetails: false), "alert.etcd.snooze")
        XCTAssertEqual(alertCategory(kind: "alertmanager", actions: [.silence, .snooze], hideDetails: true),
                       "alert.alertmanager.silence.private")
    }

    /// Every action set an alert can get has its category registered.
    func testEveryActionSetIsRegistered() {
        let keys = ["node:a", "etcd:m:x", "cert", "data:s|l", "gitops:argocd|n/a", "gitops:flux|Kustomization n/a",
                    "gitops:other|n/a", "checkup:s|k|x", "am:f"]
        for key in keys {
            for wake in [false, true] {
                let prefix = String(key.split(separator: ":").first ?? "")
                let kind = prefix == "am" ? "alertmanager" : prefix
                let actions = alertActions(key: key, problem: true, canWake: wake)
                XCTAssertTrue(alertActionSets(kind: kind).contains(actions), "\(key) \(actions)")
            }
        }
    }

    func testSnoozeActionIDs() {
        XCTAssertEqual(alertSnoozeHours.map(snoozeActionID), ["snooze.1", "snooze.8", "snooze.24"])
        XCTAssertEqual(snoozeHours(actionID: "snooze.8"), 8)
        XCTAssertNil(snoozeHours(actionID: "snooze.0"))
        XCTAssertNil(snoozeHours(actionID: "snooze.x"))
        XCTAssertNil(snoozeHours(actionID: "reboot"))
    }

    func testRequestTargets() {
        let reboot = AlertActionRequest(action: .reboot, alertKey: "node:10.0.0.2")
        XCTAssertTrue(reboot.isReboot(node: "10.0.0.2"))
        XCTAssertFalse(reboot.isReboot(node: "10.0.0.3"))
        let sync = AlertActionRequest(action: .sync, alertKey: "gitops:argocd|argocd/shop")
        XCTAssertTrue(sync.isSync(namespace: "argocd", name: "shop"))
        XCTAssertFalse(sync.isSync(namespace: "argocd", name: "web"))
        XCTAssertFalse(sync.isReconcile(kind: "", namespace: "argocd", name: "shop"))
        let reconcile = AlertActionRequest(action: .reconcile, alertKey: "gitops:flux|HelmRelease flux-system/ingress")
        XCTAssertTrue(reconcile.isReconcile(kind: "HelmRelease", namespace: "flux-system", name: "ingress"))
        XCTAssertFalse(reconcile.isReconcile(kind: "Kustomization", namespace: "flux-system", name: "ingress"))
        XCTAssertEqual(AlertActionRequest(action: .silence, alertKey: "am:f00d").silenceFingerprint, "f00d")
        XCTAssertNil(AlertActionRequest(action: .snooze, alertKey: "am:f00d").silenceFingerprint)
    }

    func testSnoozeStore() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        let snoozes = AlertSnoozes().snoozing(cluster: "c1", key: "node:a", hours: 8, now: now)
        XCTAssertTrue(snoozes.isSnoozed(cluster: "c1", key: "node:a", now: now))
        XCTAssertTrue(snoozes.isSnoozed(cluster: "c1", key: "node:a", now: now.addingTimeInterval(8 * 3600 - 1)))
        XCTAssertFalse(snoozes.isSnoozed(cluster: "c1", key: "node:a", now: now.addingTimeInterval(8 * 3600)))
        // Per cluster and key.
        XCTAssertFalse(snoozes.isSnoozed(cluster: "c2", key: "node:a", now: now))
        XCTAssertFalse(snoozes.isSnoozed(cluster: "c1", key: "node:b", now: now))
        // Nothing to snooze without a cluster, a key or a duration.
        XCTAssertEqual(snoozes.snoozing(cluster: "", key: "x", hours: 1, now: now), snoozes)
        XCTAssertEqual(snoozes.snoozing(cluster: "c1", key: "x", hours: 0, now: now), snoozes)
    }

    func testPruneDropsExpiredSnoozes() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        let snoozes = AlertSnoozes()
            .snoozing(cluster: "c1", key: "node:a", hours: 1, now: now)
            .snoozing(cluster: "c1", key: "cert", hours: 24, now: now)
            .snoozing(cluster: "c2", key: "am:f", hours: 1, now: now)
        let later = snoozes.pruned(now: now.addingTimeInterval(2 * 3600))
        XCTAssertEqual(later.until.keys.sorted(), ["c1"])
        XCTAssertEqual(later.until["c1"]?.keys.sorted(), ["cert"])
        XCTAssertEqual(snoozes.pruned(now: now), snoozes)
        XCTAssertEqual(snoozes.pruned(now: now.addingTimeInterval(48 * 3600)), AlertSnoozes())
    }

    func testSnoozesRoundTrip() throws {
        let snoozes = AlertSnoozes().snoozing(cluster: "c1", key: "gitops:argocd|argocd/shop", hours: 1, now: Date())
        let data = try JSONEncoder().encode(snoozes)
        XCTAssertEqual(try JSONDecoder().decode(AlertSnoozes.self, from: data), snoozes)
    }

    /// The monitor posts nothing for a snoozed key, neither the problem nor its end.
    func testMonitorSkipsSnoozedKeys() {
        let start = Date(timeIntervalSince1970: 1_000_000)
        func snapshot(_ health: NodeHealth, at date: Date) -> ClusterSnapshot {
            ClusterSnapshot(context: "home", takenAt: date, nodes: [
                "10.0.0.2": NodeState(hostname: "cp-1", health: .ready, reason: ""),
                "10.0.0.3": NodeState(hostname: "worker-1", health: health, reason: ""),
            ])
        }
        let first = evaluate(previous: nil, current: snapshot(.ready, at: start), now: start)
        let down = evaluate(previous: first.next, current: snapshot(.unreachable, at: start), now: start)
        XCTAssertEqual(down.alerts.map(\.key), ["node:10.0.0.3"])
        let snoozes = AlertSnoozes().snoozing(cluster: "c1", key: "node:10.0.0.3", hours: 1, now: start)
        XCTAssertEqual(snoozes.notSnoozed(down.alerts, cluster: "c1", now: start), [])
        // Another cluster's snooze does not apply.
        XCTAssertEqual(snoozes.notSnoozed(down.alerts, cluster: "c2", now: start).map(\.key), ["node:10.0.0.3"])
        // Back up while snoozed: the "ready again" is quiet too.
        let soon = start.addingTimeInterval(1800)
        let up = evaluate(previous: down.next, current: snapshot(.ready, at: soon), now: soon)
        XCTAssertEqual(up.alerts.map(\.key), ["node:10.0.0.3"])
        XCTAssertEqual(snoozes.notSnoozed(up.alerts, cluster: "c1", now: soon), [])
        // Once over, the key alerts again.
        let after = start.addingTimeInterval(3600)
        XCTAssertEqual(snoozes.notSnoozed(down.alerts, cluster: "c1", now: after).map(\.key), ["node:10.0.0.3"])
    }
}
