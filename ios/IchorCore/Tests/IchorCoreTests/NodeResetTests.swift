import XCTest
@testable import IchorCore

final class NodeResetTests: XCTestCase {
    func testPlanDecoding() throws {
        let plan = try TalosJSON.decode(NodeResetPlan.self, from: """
        {"node":"10.0.0.1","hostname":"cp-1","role":"controlplane","etcdMember":{"id":"a1","healthy":true},
         "lastControlPlane":true,"blockers":["it is the only control plane"],"warnings":[],"userDisks":["/dev/sdb"]}
        """)
        XCTAssertEqual(plan.hostname, "cp-1")
        XCTAssertTrue(plan.isControlPlane)
        XCTAssertEqual(plan.etcdMember, NodeResetPlan.Member(id: "a1", healthy: true))
        XCTAssertTrue(plan.lastControlPlane)
        XCTAssertFalse(plan.allowed)
        XCTAssertEqual(plan.userDisks, ["/dev/sdb"])
    }

    func testWorkerWithoutMemberOrLists() throws {
        let plan = try TalosJSON.decode(NodeResetPlan.self, from: """
        {"node":"10.0.0.5","role":"worker","etcdMember":null,"blockers":null}
        """)
        XCTAssertNil(plan.etcdMember)
        XCTAssertFalse(plan.isControlPlane)
        XCTAssertTrue(plan.allowed)
        XCTAssertFalse(plan.leavesDeadMember(graceful: false))
    }

    func testWipeModesNeedUserDisks() {
        XCTAssertEqual(NodeResetPlan().wipeModes, [.system])
        XCTAssertEqual(NodeResetPlan(userDisks: ["/dev/sdb"]).wipeModes, [.all, .system, .user])
        XCTAssertEqual(ResetWipe.allCases.map(\.rawValue), ["all", "system", "user"])
    }

    func testForcedResetOfAMemberLeavesItBehind() {
        let plan = NodeResetPlan(role: "controlplane", etcdMember: .init(id: "a1"))
        XCTAssertTrue(plan.leavesDeadMember(graceful: false))
        XCTAssertFalse(plan.leavesDeadMember(graceful: true))
    }

    func testResetIsANodeFeature() {
        XCTAssertEqual(NodeFeature(rawValue: "reset"), .reset)
    }
}
