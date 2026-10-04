import XCTest
@testable import IchorCore

final class EtcdLagTests: XCTestCase {
    private func status(_ node: String, leader: Bool = false, index: UInt64, applied: UInt64? = nil, error: String? = nil) throws -> EtcdNodeStatus {
        let appliedField = applied.map { #","raftAppliedIndex":\#($0)"# } ?? ""
        let errorField = error.map { #""\#($0)""# } ?? "null"
        return try TalosJSON.decode(EtcdNodeStatus.self, from: """
        {"node":"\(node)","error":\(errorField),"memberId":"\(node)","isLeader":\(leader),"isLearner":false,"dbSize":1,"dbSizeInUse":1,
         "raftIndex":\(index),"raftTerm":1\(appliedField),"version":"3.6","errors":[]}
        """)
    }

    func testFollowerCloseToTheLeaderIsNotLagging() throws {
        let statuses = [try status("a", leader: true, index: 10_000), try status("b", index: 9_990, applied: 9_990)]
        let lag = try XCTUnwrap(etcdLag(statuses[1], in: statuses))
        XCTAssertEqual(lag, EtcdLag(behindLeader: 10, applyBacklog: 0))
        XCTAssertFalse(lag.lagging)
        XCTAssertEqual(etcdLag(statuses[0], in: statuses)?.behindLeader, 0)
    }

    func testFollowerFarBehindTheLeaderIsLagging() throws {
        let statuses = [try status("a", leader: true, index: 10_000), try status("c", index: 7_000)]
        XCTAssertEqual(etcdLag(statuses[1], in: statuses)?.behindLeader, 3_000)
        XCTAssertEqual(laggingMembers(statuses).map(\.node), ["c"])
    }

    func testFollowerAheadOfTheLeaderProbeIsNotBehind() throws {
        // Probes are not simultaneous: a follower read later can be ahead of the leader read.
        let statuses = [try status("a", leader: true, index: 10_000), try status("b", index: 10_050)]
        XCTAssertEqual(etcdLag(statuses[1], in: statuses)?.behindLeader, 0)
    }

    func testApplyBacklogAloneMakesAMemberLag() throws {
        let leader = try status("a", leader: true, index: 10_000, applied: 5_000)
        let lag = try XCTUnwrap(etcdLag(leader, in: [leader]))
        XCTAssertEqual(lag.applyBacklog, 5_000)
        XCTAssertTrue(lag.lagging)
    }

    func testMissingAppliedIndexIsNoBacklog() throws {
        // Older cores do not send raftAppliedIndex.
        let follower = try status("b", index: 9_000)
        XCTAssertNil(follower.raftAppliedIndex)
        XCTAssertEqual(etcdLag(follower, in: [follower])?.applyBacklog, 0)
    }

    func testWithoutAnAnsweringLeaderTheGapIsUnknown() throws {
        let statuses = [try status("a", leader: true, index: 10_000, error: "down"), try status("b", index: 1)]
        let lag = try XCTUnwrap(etcdLag(statuses[1], in: statuses))
        XCTAssertNil(lag.behindLeader)
        XCTAssertFalse(lag.lagging)
        XCTAssertNil(etcdLag(statuses[0], in: statuses))
    }
}
