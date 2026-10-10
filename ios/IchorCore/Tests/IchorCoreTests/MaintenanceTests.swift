import XCTest
@testable import IchorCore

final class MaintenanceTests: XCTestCase {
    private let planJSON = """
    {"node":"192.0.2.11","hostname":"cp-1","kubeNode":"cp-1","controlPlane":true,"cordoned":false,
     "pods":[{"namespace":"db","name":"pg-1","owner":"Cluster/pg","kind":"evict","emptyDir":false,"pdb":"pg-primary","pdbAllowed":0},
             {"namespace":"web","name":"cache-0","owner":"StatefulSet/cache","kind":"evict","emptyDir":true,"pdb":"","pdbAllowed":-1},
             {"namespace":"tmp","name":"debug","owner":"","kind":"bare","emptyDir":false,"pdb":"","pdbAllowed":-1},
             {"namespace":"kube-system","name":"cilium-x","owner":"DaemonSet/cilium","kind":"daemonset","emptyDir":false,"pdb":"","pdbAllowed":-1},
             {"namespace":"kube-system","name":"kube-apiserver-cp-1","owner":"Node/cp-1","kind":"static","emptyDir":false,"pdb":"","pdbAllowed":-1}],
     "blockers":null,"warnings":["PodDisruptionBudget db/pg-primary allows no disruption now: the drain waits for it"],
     "acknowledge":["etcd keeps quorum with 2 of 3 members"]}
    """

    func testPlanDecoding() throws {
        let plan = try TalosJSON.decode(MaintenancePlan.self, from: planJSON)
        XCTAssertEqual(plan.hostname, "cp-1")
        XCTAssertTrue(plan.controlPlane)
        XCTAssertFalse(plan.cordoned)
        XCTAssertEqual(plan.pods.count, 5)
        XCTAssertEqual(plan.blockers, [])
        XCTAssertEqual(plan.acknowledge, ["etcd keeps quorum with 2 of 3 members"])
        XCTAssertTrue(plan.pods[0].pdbBlocks)
        XCTAssertTrue(plan.pods[0].hasPDB)
        XCTAssertFalse(plan.pods[1].hasPDB)
        XCTAssertTrue(plan.pods[1].emptyDir)
        XCTAssertEqual(plan.pods[4].podKind, .staticPod)

        let empty = try TalosJSON.decode(MaintenancePlan.self, from: #"{"node":"n","pods":null,"acknowledge":null}"#)
        XCTAssertEqual(empty, MaintenancePlan(node: "n"))
    }

    func testPodGroups() throws {
        let groups = try TalosJSON.decode(MaintenancePlan.self, from: planJSON).podGroups
        XCTAssertEqual(groups.toEvict.map(\.name), ["pg-1", "cache-0"])
        XCTAssertEqual(groups.bare.map(\.name), ["debug"])
        XCTAssertEqual(groups.leftAlone.map(\.name), ["cilium-x", "kube-apiserver-cp-1"])
        // An unknown kind from a newer core is left alone, never evicted.
        XCTAssertEqual(MaintenancePodGroups([DrainPod(namespace: "a", name: "b", kind: "mystery")]).leftAlone.count, 1)
    }

    func testCanStart() {
        let ack = "etcd keeps quorum"
        let plan = MaintenancePlan(node: "n", acknowledge: [ack])
        XCTAssertFalse(plan.canStart(action: .reboot, acknowledged: []))
        XCTAssertTrue(plan.canStart(action: .reboot, acknowledged: [ack]))
        XCTAssertFalse(plan.canStart(action: .shutdown, acknowledged: []))
        XCTAssertTrue(plan.canStart(action: .none, acknowledged: []))
        XCTAssertEqual(plan.acknowledgments(for: .none), [])
        XCTAssertEqual(plan.acknowledgments(for: .shutdown), [ack])

        let blocked = MaintenancePlan(node: "n", blockers: ["etcd would lose quorum"])
        XCTAssertFalse(blocked.canStart(action: .reboot, acknowledged: []))
        XCTAssertFalse(blocked.canStart(action: .shutdown, acknowledged: []))
        XCTAssertTrue(blocked.canStart(action: .none, acknowledged: []))
        XCTAssertTrue(MaintenancePlan(node: "n").canStart(action: .reboot, acknowledged: []))
    }

    func testProgressDecoding() throws {
        let event = try TalosJSON.decode(MaintenanceProgress.self, from: """
        {"phase":"drain","message":"1 of 2 pods evicted, 1 waiting for a PodDisruptionBudget","at":1700000000000,
         "pods":[{"namespace":"db","name":"pg-1","kind":"evict","state":"blocked","reason":"PDB pg-primary allows 0 disruptions"},
                 {"namespace":"web","name":"a","kind":"evict","state":"gone"}]}
        """)
        XCTAssertEqual(event.at, 1_700_000_000_000)
        XCTAssertEqual(event.pods.map(\.podState), [.blocked, .gone])
        XCTAssertEqual(event.pods[0].reason, "PDB pg-primary allows 0 disruptions")
        let bare = try TalosJSON.decode(MaintenanceProgress.self, from: #"{"phase":"reboot","message":"rebooting cp-1"}"#)
        XCTAssertEqual(bare.pods, [])
    }

    func testTimeline() {
        XCTAssertEqual(MaintenancePhase.steps(for: .none), [.cordon, .drain])
        XCTAssertEqual(MaintenancePhase.steps(for: .shutdown), [.cordon, .drain, .shutdown])

        let events = [
            MaintenanceProgress(phase: "cordon", message: "cordoning w1", at: 1),
            MaintenanceProgress(phase: "drain", message: "evicting 2 pods", at: 2, pods: [DrainPod(namespace: "a", name: "p", state: "pending")]),
            MaintenanceProgress(phase: "drain", message: "2 of 2 pods evicted", at: 3, pods: [DrainPod(namespace: "a", name: "p", state: "gone")]),
            MaintenanceProgress(phase: "reboot", message: "rebooting w1", at: 4),
        ]
        let running = maintenanceTimeline(events, action: .reboot)
        XCTAssertEqual(running.map(\.state), [.done, .done, .current, .pending, .pending])
        XCTAssertEqual(running[1].at, 2)
        XCTAssertEqual(running[1].message, "2 of 2 pods evicted")

        let failed = maintenanceTimeline(events, action: .reboot, failure: "boom; w1 stays cordoned")
        XCTAssertEqual(failed.map(\.state), [.done, .done, .failed, .pending, .pending])
        XCTAssertEqual(failed[2].message, "boom; w1 stays cordoned")

        XCTAssertEqual(maintenanceTimeline(events, action: .reboot, finished: true).map(\.state), Array(repeating: .done, count: 5))
        XCTAssertEqual(maintenanceTimeline([], action: .none).map(\.state), [.pending, .pending])
        XCTAssertEqual(maintenanceTimeline([], action: .none, failure: "refused").map(\.state), [.failed, .pending])
        XCTAssertEqual(latestDrainPods(events).map(\.podState), [.gone])
        XCTAssertEqual(latestDrainPods([]), [])
    }

    func testUpgradeAction() throws {
        XCTAssertEqual(MaintenancePhase.steps(for: .upgrade), [.cordon, .drain, .upgrade, .waiting, .uncordon])
        XCTAssertFalse(MaintenanceAction.planned.contains(.upgrade))
        XCTAssertTrue(MaintenanceAction.upgrade.takesNodeDown)
        XCTAssertLessThan(MaintenancePhase.upgrade, MaintenancePhase.waiting)

        let events = [MaintenanceProgress(phase: "cordon"), MaintenanceProgress(phase: "drain"),
                      MaintenanceProgress(phase: "upgrade", message: "installing: installing")]
        let timeline = maintenanceTimeline(events, action: .upgrade)
        XCTAssertEqual(timeline.map(\.state), [.done, .done, .current, .pending, .pending])
        XCTAssertEqual(timeline[2].message, "installing: installing")

        let plan = try TalosJSON.decode(MaintenancePlan.self, from: #"{"node":"n","upgradeDrainable":true}"#)
        XCTAssertTrue(plan.upgradeDrainable)
        XCTAssertFalse(try TalosJSON.decode(MaintenancePlan.self, from: planJSON).upgradeDrainable)
        XCTAssertTrue(try TalosJSON.decode(UpgradePlan.self, from: #"{"node":"n","drainable":true}"#).drainable)
        XCTAssertFalse(try TalosJSON.decode(UpgradePlan.self, from: #"{"node":"n"}"#).drainable)
    }
}
