import XCTest
@testable import IchorCore

final class EtcdBackupTests: XCTestCase {
    private func member(_ node: String, leader: Bool = false, learner: Bool = false, error: String? = nil,
                        errors: [String] = []) -> EtcdNodeStatus {
        let errorList = errors.map { "\"\($0)\"" }.joined(separator: ",")
        let json = """
        {"node":"\(node)",\(error.map { "\"error\":\"\($0)\"," } ?? "")"memberId":"\(error == nil ? "id-\(node)" : "")","isLeader":\(leader),"isLearner":\(learner),
         "dbSize":100,"dbSizeInUse":40,"raftIndex":1,"raftTerm":1,"version":"3.7.0","errors":[\(errorList)]}
        """
        return try! TalosJSON.decode(EtcdNodeStatus.self, from: json)
    }

    func testDefaultMemberIsAHealthyFollower() {
        let statuses = [
            member("leader", leader: true),
            member("down", error: "timed out"),
            member("alarmed", errors: ["NOSPACE"]),
            member("learner", learner: true),
            member("follower"),
        ]
        XCTAssertEqual(defaultSnapshotMember(statuses)?.node, "follower")
    }

    func testDefaultMemberFallsBackToLeader() {
        XCTAssertEqual(defaultSnapshotMember([member("down", error: "x"), member("leader", leader: true)])?.node, "leader")
        XCTAssertNil(defaultSnapshotMember([member("down", error: "x")]))
    }

    func testFilename() {
        let date = Date(timeIntervalSince1970: 1_800_000_000) // 2027-01-15 08:00 UTC
        let utc = TimeZone(identifier: "UTC")!
        XCTAssertEqual(etcdSnapshotFilename(context: "prod", hostname: "cp-1", date: date, timeZone: utc),
                       "etcd-prod-cp-1-20270115-0800.snapshot")
        XCTAssertEqual(etcdSnapshotFilename(context: "admin@home lab", hostname: "a/b", date: date, timeZone: utc),
                       "etcd-admin-home-lab-a-b-20270115-0800.snapshot")
    }

    func testFraction() {
        XCTAssertNil(snapshotFraction(bytes: 10, expected: 0))
        XCTAssertEqual(snapshotFraction(bytes: 25, expected: 100) ?? -1, 0.25, accuracy: 0.0001)
        XCTAssertEqual(snapshotFraction(bytes: 300, expected: 100), 1)
    }

    func testRoles() {
        XCTAssertTrue(ContextSummary(name: "b", roles: ["os:etcd:backup"]).allows(.etcdSnapshot))
        XCTAssertTrue(ContextSummary(name: "o", roles: ["os:operator"]).allows(.etcdSnapshot))
        XCTAssertFalse(ContextSummary(name: "r", roles: ["os:reader"]).allows(.etcdSnapshot))
        XCTAssertFalse(ContextSummary(name: "o", roles: ["os:operator"]).allows(.machineConfig))
        XCTAssertTrue(ContextSummary(name: "a", roles: ["os:admin"]).allows(.machineConfig))
        XCTAssertFalse(ContextSummary(name: "b", roles: ["os:etcd:backup"]).allows(.etcdDefrag))
    }

    func testFilterLines() {
        let yaml = "machine:\n  token: abc\n  type: controlplane\ncluster:\n"
        XCTAssertEqual(filterLines(yaml, query: "").count, 4)
        XCTAssertEqual(filterLines(yaml, query: "TOKEN"), [NumberedLine(number: 2, text: "  token: abc")])
        XCTAssertEqual(filterLines(yaml, query: "e:").map(\.number), [1, 3])
    }
}
