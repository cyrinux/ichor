import XCTest
@testable import IchorCore

final class TalosRolloutTests: XCTestCase {
    private let latest = "v1.14.2"

    private func node(_ name: String, _ version: String = "v1.14.1", cp: Bool = false, reachable: Bool = true,
                      ready: Bool = true) -> NodeOverview {
        NodeOverview(node: name, hostname: name, reachable: reachable, version: version,
                     role: cp ? "controlplane" : "worker", ready: reachable && ready)
    }

    /// The cluster with `name` replaced by another state of that node.
    private func cluster(_ changed: NodeOverview? = nil) -> [NodeOverview] {
        let nodes = [
            node("w2"), node("w1", "v1.13.5"),
            node("cp2", cp: true), node("cp1", cp: true), node("cp3", "v1.14.2", cp: true),
        ]
        return nodes.map { $0.hostname == changed?.hostname ? changed! : $0 }
    }

    private func row(_ plan: TalosRollout, _ name: String) -> RolloutRow {
        (plan.controlPlane + plan.workers).first { $0.node.hostname == name }!
    }

    func testPlanByRoleInUpgradeOrder() {
        let plan = TalosRollout(nodes: cluster(), latest: latest)
        // Pending first (oldest version, then hostname), done last.
        XCTAssertEqual(plan.controlPlane.map(\.node.hostname), ["cp1", "cp2", "cp3"])
        XCTAssertEqual(plan.workers.map(\.node.hostname), ["w1", "w2"])
        XCTAssertEqual(row(plan, "cp3").state, .done)
        XCTAssertEqual(row(plan, "cp1").state, .pending)
        XCTAssertEqual(plan.controlPlaneDone, 1)
        XCTAssertEqual(plan.workersDone, 0)
        XCTAssertNil(plan.hold)
        XCTAssertEqual(plan.next?.node.hostname, "cp1")
        XCTAssertTrue(plan.canOpen(row(plan, "cp2")))
        XCTAssertFalse(plan.canOpen(row(plan, "cp3")))
    }

    func testWorkersAreASoftGateUntilTheControlPlaneIsDone() {
        let plan = TalosRollout(nodes: cluster(), latest: latest)
        XCTAssertTrue(plan.workersWait)
        XCTAssertTrue(plan.canOpen(row(plan, "w1")))
        XCTAssertTrue(plan.needsConfirm(row(plan, "w1")))
        XCTAssertFalse(plan.needsConfirm(row(plan, "cp1")))

        let upgraded = cluster().map { $0.role == "controlplane" ? node($0.hostname, latest, cp: true) : $0 }
        let done = TalosRollout(nodes: upgraded, latest: latest)
        XCTAssertFalse(done.workersWait)
        XCTAssertFalse(done.needsConfirm(row(done, "w1")))
        XCTAssertEqual(done.next?.node.hostname, "w1")
    }

    func testOneAtATime() {
        let plan = TalosRollout(nodes: cluster(), latest: latest, run: RolloutRun(node: "cp1"))
        XCTAssertEqual(row(plan, "cp1").state, .upgrading)
        XCTAssertEqual(plan.hold, .upgrading(row(plan, "cp1").node, waiting: false))
        // Its own row opens the progress; every other one is held.
        XCTAssertTrue(plan.canOpen(row(plan, "cp1")))
        XCTAssertFalse(plan.canOpen(row(plan, "cp2")))
        XCTAssertFalse(plan.canOpen(row(plan, "w1")))
        XCTAssertEqual(plan.next?.node.hostname, "cp2")
        XCTAssertFalse(plan.canOpen(plan.next!))

        let rebooted = cluster(node("cp1", cp: true, reachable: false))
        let waiting = TalosRollout(nodes: rebooted, latest: latest, run: RolloutRun(node: "cp1", waiting: true),
                                   etcd: EtcdHealth(healthy: 2, members: 3))
        XCTAssertEqual(row(waiting, "cp1").state, .waitingHealthy)
        XCTAssertEqual(waiting.hold, .upgrading(row(waiting, "cp1").node, waiting: true))
        XCTAssertEqual(waiting.etcd, EtcdHealth(healthy: 2, members: 3))
    }

    func testUpgradedNodeWaitsUntilHealthy() {
        // On the new version but not ready yet, with no run followed (the app was restarted).
        let plan = TalosRollout(nodes: cluster(node("cp1", latest, cp: true, ready: false)), latest: latest)
        XCTAssertEqual(row(plan, "cp1").state, .waitingHealthy)
        XCTAssertEqual(plan.hold, .unhealthy([row(plan, "cp1").node]))
        XCTAssertFalse(plan.canOpen(row(plan, "cp1")))
        XCTAssertFalse(plan.canOpen(row(plan, "cp2")))

        // The run ended well, the overview still shows the old version: not pending again.
        let stale = TalosRollout(nodes: cluster(), latest: latest,
                                 run: RolloutRun(node: "cp1", waiting: true, finished: true, reached: latest))
        XCTAssertEqual(row(stale, "cp1").state, .waitingHealthy)
        XCTAssertEqual(stale.hold, .upgrading(row(stale, "cp1").node, waiting: true))

        // A finished run that did not bring the node to latest (staged, another target) holds nothing.
        for reached in ["", "v1.14.1"] {
            let other = TalosRollout(nodes: cluster(), latest: latest, run: RolloutRun(node: "cp1", finished: true, reached: reached))
            XCTAssertEqual(row(other, "cp1").state, .pending)
            XCTAssertNil(other.hold)
        }
    }

    func testFailedNodeIsRetriedFirst() {
        let plan = TalosRollout(nodes: cluster(), latest: latest, run: RolloutRun(node: "cp2", finished: true, failed: true))
        XCTAssertEqual(row(plan, "cp2").state, .failed)
        XCTAssertNil(plan.hold)
        XCTAssertEqual(plan.next?.node.hostname, "cp2")
        XCTAssertTrue(plan.canOpen(row(plan, "cp2")))
        XCTAssertTrue(plan.canOpen(row(plan, "cp1")))
    }

    func testDegradedClusterHoldsTheRollout() {
        let plan = TalosRollout(nodes: cluster(node("w2", reachable: false)), latest: latest)
        XCTAssertEqual(plan.hold, .unhealthy([row(plan, "w2").node]))
        XCTAssertFalse(plan.canOpen(row(plan, "cp1")))
        XCTAssertFalse(plan.canOpen(row(plan, "w2")), "unreachable: nothing to ask it")

        // The only unhealthy node may itself be upgraded (or retried): no second node goes down.
        let own = TalosRollout(nodes: cluster(node("cp1", cp: true, ready: false)), latest: latest)
        XCTAssertTrue(own.canOpen(row(own, "cp1")))
        XCTAssertFalse(own.canOpen(row(own, "cp2")))

        let etcd = TalosRollout(nodes: cluster(), latest: latest, etcd: EtcdHealth(healthy: 2, members: 3))
        XCTAssertEqual(etcd.hold, .etcd(EtcdHealth(healthy: 2, members: 3)))
        XCTAssertFalse(etcd.canOpen(row(etcd, "cp1")))
        XCTAssertNil(TalosRollout(nodes: cluster(), latest: latest, etcd: EtcdHealth(healthy: 3, members: 3)).hold)
    }

    func testUnknownVersionsSortLast() {
        let plan = TalosRollout(nodes: [node("b", ""), node("a"), node("c", "v1.13.0")], latest: latest)
        XCTAssertEqual(plan.workers.map(\.node.hostname), ["c", "a", "b"])
        XCTAssertEqual(row(plan, "b").state, .pending)
    }

    func testWaitsForNodeOnceItRebooted() {
        XCTAssertFalse(upgradeWaitsForNode([UpgradeProgress(phase: "requested"), UpgradeProgress(phase: "rebooting")]))
        XCTAssertTrue(upgradeWaitsForNode([UpgradeProgress(phase: "rebooting"), UpgradeProgress(phase: "waiting for node")]))
        XCTAssertTrue(upgradeWaitsForNode([UpgradeProgress(phase: "booted")]))
        XCTAssertFalse(upgradeWaitsForNode([]))
    }

    func testEtcdHealth() throws {
        func status(_ node: String, member: String, error: String? = nil, errors: [String] = []) -> String {
            let failure = error.map { #""error":"\#($0)","# } ?? ""
            let list = errors.map { #""\#($0)""# }.joined(separator: ",")
            return #"{"node":"\#(node)",\#(failure)"memberId":"\#(member)","isLeader":false,"isLearner":false,"dbSize":0,"dbSizeInUse":0,"raftIndex":0,"raftTerm":0,"version":"3.5.0","errors":[\#(list)]}"#
        }
        let members = ["a", "b", "c"].map { #"{"id":"\#($0)","hostname":"cp-\#($0)","peerUrls":[],"clientUrls":[],"isLearner":false}"# }
        let statuses = [
            status("1", member: "a"),
            status("2", member: "b", errors: ["etcdserver: no leader"]),
            status("3", member: "", error: "unreachable"),
        ]
        let etcd = try TalosJSON.decode(EtcdOverview.self, from: """
        {"leaderId":"a","members":[\(members.joined(separator: ","))],"statuses":[\(statuses.joined(separator: ","))],"alarms":[]}
        """)
        XCTAssertEqual(etcd.health, EtcdHealth(healthy: 1, members: 3))
        XCTAssertTrue(EtcdHealth(healthy: 1, members: 3).degraded)
        let failed = try TalosJSON.decode(EtcdOverview.self, from: #"{"error":"no control plane answered","leaderId":"","members":[],"statuses":[],"alarms":[]}"#)
        XCTAssertNil(failed.health)
    }
}
