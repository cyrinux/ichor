import XCTest
@testable import IchorCore

final class ClusterUpgradeTests: XCTestCase {
    func testAPartlyUpgradedClusterContinues() throws {
        let plan = try TalosJSON.decode(ClusterUpgradePlan.self, from: """
        {"version":"v1.12.1","image":"ghcr.io/siderolabs/installer:v1.12.1","nodes":[
          {"node":"10.0.0.1","hostname":"cp-1","role":"controlplane","from":"v1.12.1","state":"done","blockers":[],"warnings":[]},
          {"node":"10.0.0.2","hostname":"cp-2","role":"controlplane","from":"v1.12.0","state":"pending","leader":true,"blockers":null,
           "warnings":["no build of the extension gvisor"]},
          {"node":"10.0.0.3","role":"worker","from":"v1.12.0","state":"pending"}
        ],"blockers":[],"warnings":[],"drain":false}
        """)
        XCTAssertTrue(plan.continues)
        XCTAssertTrue(plan.canStart)
        XCTAssertEqual(plan.pending.map(\.name), ["cp-2", "10.0.0.3"])
        XCTAssertEqual(plan.allWarnings, ["cp-2: no build of the extension gvisor"])
        XCTAssertTrue(plan.nodes[1].leader && plan.nodes[1].controlPlane)
    }

    func testBlockersStopTheStart() throws {
        let plan = try TalosJSON.decode(ClusterUpgradePlan.self, from: """
        {"version":"v1.12.1","nodes":[{"node":"10.0.0.3","hostname":"w-1","state":"pending","blockers":["the node does not answer"]}],
         "blockers":["another phone holds the upgrade lock"]}
        """)
        XCTAssertEqual(plan.allBlockers, ["another phone holds the upgrade lock", "w-1: the node does not answer"])
        XCTAssertFalse(plan.canStart)

        let upToDate = try TalosJSON.decode(ClusterUpgradePlan.self, from: #"{"version":"v1.12.1","nodes":[{"node":"10.0.0.1","state":"done"}]}"#)
        XCTAssertTrue(upToDate.upToDate)
        XCTAssertFalse(upToDate.canStart)
    }

    func testProgress() throws {
        let progress = try TalosJSON.decode(ClusterUpgradeProgress.self, from: """
        {"phase":"paused","index":1,"total":3,"node":"10.0.0.2","message":"etcd is not healthy",
         "nodes":[{"node":"10.0.0.1","state":"done"},{"node":"10.0.0.2","state":"later"}]}
        """)
        XCTAssertEqual(progress.rollPhase, .paused)
        XCTAssertEqual(progress.name, "10.0.0.2")
        XCTAssertEqual(progress.nodes.map(\.nodeState), [.done, .pending])
    }
}
