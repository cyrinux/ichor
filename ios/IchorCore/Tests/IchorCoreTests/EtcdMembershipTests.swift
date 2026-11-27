import XCTest
@testable import IchorCore

final class EtcdMembershipTests: XCTestCase {
    private func overview(_ json: String) throws -> EtcdOverview {
        try TalosJSON.decode(EtcdOverview.self, from: json)
    }

    func testLearnerAndFailedJoinShowTheirPhase() throws {
        let etcd = try overview(
            """
            {"leaderId":"a1","members":[],"alarms":[],"statuses":[
              {"node":"10.0.0.1","memberId":"a1","isLeader":true,"isLearner":false,"dbSize":1,"dbSizeInUse":1,
               "raftIndex":1,"raftTerm":1,"version":"3.6","errors":[],"membershipPhase":"member"},
              {"node":"10.0.0.3","memberId":"c3","isLeader":false,"isLearner":true,"dbSize":1,"dbSizeInUse":1,
               "raftIndex":1,"raftTerm":1,"version":"3.6","errors":[],"membershipPhase":"learner"},
              {"node":"10.0.0.4","error":"etcd is not running","memberId":"","isLeader":false,"isLearner":false,
               "dbSize":0,"dbSizeInUse":0,"raftIndex":0,"raftTerm":0,"version":"","errors":[],
               "membershipPhase":"joining","membershipError":"too many learner members in cluster"}
            ]}
            """)
        XCTAssertEqual(etcd.statuses.map(\.unsettledPhase), [nil, "learner", "joining"])
        XCTAssertEqual(etcd.statuses[2].membershipError, "too many learner members in cluster")
    }

    func testTalos114HasNoPhase() throws {
        let etcd = try overview(
            """
            {"leaderId":"a1","members":[],"alarms":[],"statuses":[
              {"node":"10.0.0.1","memberId":"a1","isLeader":true,"isLearner":false,"dbSize":1,"dbSizeInUse":1,
               "raftIndex":1,"raftTerm":1,"version":"3.6","errors":[]}
            ]}
            """)
        XCTAssertNil(etcd.statuses[0].membershipPhase)
        XCTAssertNil(etcd.statuses[0].unsettledPhase)
    }
}
