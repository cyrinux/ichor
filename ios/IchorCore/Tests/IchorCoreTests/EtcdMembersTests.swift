import XCTest
@testable import IchorCore

final class EtcdMembersTests: XCTestCase {
    private func overview(_ json: String) throws -> EtcdOverview {
        try TalosJSON.decode(EtcdOverview.self, from: json)
    }

    private let three = """
    {"error":null,"leaderId":"a1","members":[
      {"id":"a1","hostname":"cp-1","peerUrls":[],"clientUrls":[],"isLearner":false},
      {"id":"b2","hostname":"cp-2","peerUrls":[],"clientUrls":[],"isLearner":false},
      {"id":"c3","hostname":"cp-3","peerUrls":[],"clientUrls":[],"isLearner":false}],
     "statuses":[
      {"node":"10.0.0.1","error":null,"memberId":"a1","isLeader":true,"isLearner":false,"dbSize":1,"dbSizeInUse":1,"raftIndex":1,"raftTerm":1,"version":"3.5","errors":[]},
      {"node":"10.0.0.2","error":null,"memberId":"b2","isLeader":false,"isLearner":false,"dbSize":1,"dbSizeInUse":1,"raftIndex":1,"raftTerm":1,"version":"3.5","errors":[]},
      {"node":"10.0.0.3","error":"unreachable","memberId":"","isLeader":false,"isLearner":false,"dbSize":0,"dbSizeInUse":0,"raftIndex":0,"raftTerm":0,"version":"","errors":[]}],
     "alarms":[]}
    """

    func testPlanDecoding() throws {
        let plan = try TalosJSON.decode(EtcdMemberPlan.self, from: """
        {"member":{"id":"c3","hostname":"cp-3"},"found":true,"isLeader":false,"isLearner":false,"healthy":false,"members":3,
         "healthyAfter":2,"membersAfter":2,"quorumAfter":true,"blockers":null,"warnings":["cp-3 is not reachable"]}
        """)
        XCTAssertEqual(plan, EtcdMemberPlan(member: .init(id: "c3", hostname: "cp-3"), healthyAfter: 2, membersAfter: 2, keepsQuorum: true,
                                            warnings: ["cp-3 is not reachable"]))
        XCTAssertTrue(plan.keepsQuorum)
        XCTAssertEqual(try TalosJSON.decode(EtcdForfeitResult.self, from: #"{"member":"cp-1"}"#).member, "cp-1")
        XCTAssertEqual(try TalosJSON.decode(EtcdForfeitResult.self, from: "{}").member, "")
    }

    func testQuorum() throws {
        func plan(_ json: String) throws -> EtcdMemberPlan { try TalosJSON.decode(EtcdMemberPlan.self, from: json) }
        XCTAssertFalse(try plan(#"{"member":{"id":"c3"},"healthyAfter":1,"membersAfter":2,"quorumAfter":false}"#).keepsQuorum)
        // A count of members needed instead of the verdict.
        XCTAssertTrue(try plan(#"{"member":{"id":"c3"},"healthyAfter":2,"membersAfter":2,"quorumAfter":2}"#).keepsQuorum)
        XCTAssertFalse(try plan(#"{"member":{"id":"c3"},"healthyAfter":1,"membersAfter":2,"quorumAfter":2}"#).keepsQuorum)
        XCTAssertFalse(try plan(#"{"member":{"id":"c3"}}"#).keepsQuorum)
    }

    func testGate() {
        let clean = EtcdMemberPlan(member: .init(id: "c3", hostname: "cp-3"), healthyAfter: 2, membersAfter: 2,
                                   warnings: ["it is the leader"])
        XCTAssertTrue(EtcdRemovalGate(plan: clean).canRemove)
        XCTAssertFalse(EtcdRemovalGate(plan: clean, busy: true).canRemove)
        XCTAssertEqual(EtcdRemovalGate(plan: clean).confirmationName, "cp-3")

        let blocked = EtcdMemberPlan(member: .init(id: "c3", hostname: "cp-3"), healthyAfter: 1, membersAfter: 2, keepsQuorum: false,
                                     blockers: ["etcd would lose quorum"])
        XCTAssertTrue(EtcdRemovalGate(plan: blocked).blocked)
        XCTAssertFalse(EtcdRemovalGate(plan: blocked).canRemove)

        // No hostname: the id is typed instead; a plan without member cannot be run.
        XCTAssertEqual(EtcdRemovalGate(plan: EtcdMemberPlan(member: .init(id: "c3"))).confirmationName, "c3")
        XCTAssertFalse(EtcdRemovalGate(plan: EtcdMemberPlan(member: .init(id: ""))).canRemove)
    }

    func testMemberActions() throws {
        let etcd = try overview(three)
        // The reachable leader can forfeit; everyone can be removed.
        XCTAssertEqual(etcdMemberActions(memberId: "a1", etcd: etcd), EtcdMemberActions(forfeitNode: "10.0.0.1", canRemove: true))
        XCTAssertEqual(etcdMemberActions(memberId: "b2", etcd: etcd), EtcdMemberActions(forfeitNode: nil, canRemove: true))
        XCTAssertEqual(etcdMemberActions(memberId: "c3", etcd: etcd), EtcdMemberActions(forfeitNode: nil, canRemove: true))
        XCTAssertEqual(etcdMemberActions(memberId: "", etcd: etcd), EtcdMemberActions(forfeitNode: nil, canRemove: false))

        let single = try overview("""
        {"error":null,"leaderId":"a1","members":[{"id":"a1","hostname":"cp-1","peerUrls":[],"clientUrls":[],"isLearner":false}],
         "statuses":[{"node":"10.0.0.1","error":null,"memberId":"a1","isLeader":true,"isLearner":false,"dbSize":1,"dbSizeInUse":1,"raftIndex":1,"raftTerm":1,"version":"3.5","errors":[]}],
         "alarms":[]}
        """)
        // Nobody to hand over to, and the last member stays.
        XCTAssertEqual(etcdMemberActions(memberId: "a1", etcd: single), EtcdMemberActions(forfeitNode: nil, canRemove: false))
    }

    func testRemovalNode() throws {
        let etcd = try overview(three)
        // Another member that answered, the leader last.
        XCTAssertEqual(etcdRemovalNode(memberId: "c3", statuses: etcd.statuses), "10.0.0.2")
        XCTAssertEqual(etcdRemovalNode(memberId: "b2", statuses: etcd.statuses), "10.0.0.1")
        XCTAssertEqual(etcdRemovalNode(memberId: "a1", statuses: etcd.statuses), "10.0.0.2")
        XCTAssertNil(etcdRemovalNode(memberId: "a1", statuses: etcd.statuses.filter { $0.memberId == "a1" }))
    }

    func testRemovalNodePrefersAMemberWithoutErrors() throws {
        let etcd = try overview(three.replacingOccurrences(
            of: #""memberId":"b2","isLeader":false,"isLearner":false,"dbSize":1,"dbSizeInUse":1,"raftIndex":1,"raftTerm":1,"version":"3.5","errors":[]"#,
            with: #""memberId":"b2","isLeader":false,"isLearner":false,"dbSize":1,"dbSizeInUse":1,"raftIndex":1,"raftTerm":1,"version":"3.5","errors":["etcdserver: no leader"]"#))
        XCTAssertEqual(etcd.statuses[1].errors.count, 1)
        // b2 reports errors: the leader takes the request although it comes last otherwise.
        XCTAssertEqual(etcdRemovalNode(memberId: "c3", statuses: etcd.statuses), "10.0.0.1")
        // Only b2 is left to ask: better than nobody.
        XCTAssertEqual(etcdRemovalNode(memberId: "a1", statuses: etcd.statuses), "10.0.0.2")
    }

    func testTypedConfirmation() {
        XCTAssertTrue(typedConfirmationMatches("cp-3", token: "cp-3"))
        XCTAssertTrue(typedConfirmationMatches(" cp-3 ", token: "cp-3"))
        XCTAssertFalse(typedConfirmationMatches("cp-", token: "cp-3"))
        XCTAssertFalse(typedConfirmationMatches("CP-3", token: "cp-3"))
        // A blank token is never confirmed, least of all by an empty field.
        XCTAssertFalse(typedConfirmationMatches("", token: ""))
        XCTAssertFalse(typedConfirmationMatches("  ", token: " "))
        // A member without hostname is confirmed by its id.
        let gate = EtcdRemovalGate(plan: EtcdMemberPlan(member: .init(id: "c3", hostname: " ")))
        XCTAssertEqual(gate.confirmationName, "c3")
        XCTAssertTrue(typedConfirmationMatches("c3", token: gate.confirmationName))
    }
}
