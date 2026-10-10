import XCTest
@testable import IchorCore

final class ControlPlaneReplaceTests: XCTestCase {
    private let planJSON = """
    {"member":{"id":"a3","hostname":"talos-cp-c","node":"192.0.2.53","found":true,"healthy":false,"reachable":false},
     "quorum":{"members":3,"healthy":2,"afterRemoval":2,"healthyAfter":2,"safe":true},
     "leader":{"id":"a1","node":"192.0.2.51","hostname":"talos-cp-a"},
     "steps":[{"id":"confirmQuorum","state":"done","detail":"2 of 2"},
              {"id":"removeMember","state":"ready","detail":"sent through talos-cp-a"},
              {"id":"resetOrPowerOff","state":"skipped","detail":"192.0.2.53 does not answer"},
              {"id":"bootNewNode","state":"pending","detail":""},
              {"id":"waitMember","state":"someday"}],
     "template":{"id":"a1","node":"192.0.2.51","hostname":"talos-cp-a"}}
    """

    func testPlanDecoding() throws {
        let plan = try TalosJSON.decode(CpReplacePlan.self, from: planJSON)
        XCTAssertEqual(plan.member.hostname, "talos-cp-c")
        XCTAssertEqual(plan.confirmationName, "talos-cp-c")
        XCTAssertEqual(plan.template.node, "192.0.2.51")
        XCTAssertEqual(plan.state(.confirmQuorum), .done)
        XCTAssertEqual(plan.state(.removeMember), .ready)
        XCTAssertEqual(plan.state(.resetOrPowerOff), .skipped)
        // Unknown state: pending.
        XCTAssertEqual(plan.state(.waitMember), .pending)
        XCTAssertEqual(plan.membersBeforeJoin, 2)
    }

    func testMissingFieldsAndSteps() throws {
        let plan = try TalosJSON.decode(CpReplacePlan.self, from: #"{"member":{"id":"a3"},"steps":null}"#)
        XCTAssertEqual(plan.state(.bootNewNode), .pending)
        XCTAssertEqual(plan.confirmationName, "a3")
        XCTAssertFalse(plan.member.found)
        XCTAssertEqual(CpReplacePlan(quorum: .init(members: 3, afterRemoval: 2)).membersBeforeJoin, 3)
    }

    func testWaitDecoding() throws {
        let wait = try TalosJSON.decode(CpReplaceWait.self, from: """
        {"joined":true,"members":[{"id":"a1","hostname":"talos-cp-a","healthy":true}]}
        """)
        XCTAssertTrue(wait.joined)
        XCTAssertEqual(wait.members.count, 1)
        XCTAssertEqual(wait.detail, "")
    }

    func testReplaceCandidatesAreFailedVoters() throws {
        let etcd = try TalosJSON.decode(EtcdOverview.self, from: """
        {"leaderId":"a1","alarms":[],
         "members":[{"id":"a1","hostname":"talos-cp-a","peerUrls":[],"clientUrls":["https://192.0.2.51:2379"],"isLearner":false},
                    {"id":"a2","hostname":"talos-cp-b","peerUrls":["https://[2001:db8::2]:2380"],"clientUrls":[],"isLearner":false},
                    {"id":"a3","hostname":"talos-cp-c","peerUrls":[],"clientUrls":[],"isLearner":false},
                    {"id":"a4","hostname":"talos-cp-d","peerUrls":[],"clientUrls":[],"isLearner":true}],
         "statuses":[{"node":"192.0.2.51","memberId":"a1","isLeader":true,"isLearner":false,"dbSize":0,"dbSizeInUse":0,"raftIndex":0,"raftTerm":0,"version":"","errors":[]},
                     {"node":"192.0.2.52","memberId":"a2","isLeader":false,"isLearner":false,"dbSize":0,"dbSizeInUse":0,"raftIndex":0,"raftTerm":0,"version":"","errors":["etcdserver: no leader"]},
                     {"node":"192.0.2.53","error":"unreachable","memberId":"","isLeader":false,"isLearner":false,"dbSize":0,"dbSizeInUse":0,"raftIndex":0,"raftTerm":0,"version":"","errors":[]}]}
        """)
        XCTAssertEqual(replaceCandidates(etcd).map(\.id), ["a2", "a3"])
        XCTAssertEqual(etcdMemberAddress(etcd.members[0]), "192.0.2.51")
        XCTAssertEqual(etcdMemberAddress(etcd.members[1]), "2001:db8::2")
        XCTAssertEqual(etcdMemberAddress(etcd.members[2]), "")
    }
}
