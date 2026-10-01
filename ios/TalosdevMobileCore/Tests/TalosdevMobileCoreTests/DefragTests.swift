import XCTest
@testable import TalosdevMobileCore

final class DefragTests: XCTestCase {
    private func member(_ node: String, leader: Bool = false, size: Int64 = 100, inUse: Int64 = 40, error: String? = nil) -> EtcdNodeStatus {
        let json = """
        {"node":"\(node)",\(error.map { "\"error\":\"\($0)\"," } ?? "")"memberId":"\(error == nil ? "id-\(node)" : "")","isLeader":\(leader),"isLearner":false,
         "dbSize":\(size),"dbSizeInUse":\(inUse),"raftIndex":1,"raftTerm":1,"version":"3.7.0","errors":[]}
        """
        return try! TalosJSON.decode(EtcdNodeStatus.self, from: json)
    }

    func testFollowersFirstLeaderLastSkippingUnreachable() {
        let order = defragOrder([
            member("leader", leader: true, size: 900),
            member("small", size: 100, inUse: 90),
            member("down", error: "timed out"),
            member("big", size: 500, inUse: 50),
        ])
        XCTAssertEqual(order.map(\.node), ["big", "small", "leader"])
    }

    func testReclaimableNeverNegative() {
        XCTAssertEqual(member("a").reclaimable, 60)
        XCTAssertEqual(member("b", size: 10, inUse: 20).reclaimable, 0)
    }

    func testRoles() {
        XCTAssertTrue(ContextSummary(name: "o", roles: ["os:operator"]).allows(.etcdDefrag))
        XCTAssertFalse(ContextSummary(name: "r", roles: ["os:reader"]).allows(.etcdDefrag))
    }
}
