import XCTest
@testable import IchorCore

final class EtcdRecoverTests: XCTestCase {
    func testSnapshotInfo() throws {
        let info = try TalosJSON.decode(SnapshotInfo.self, from: #"{"encrypted":true,"recipientsHint":"scrypt","size":4096,"sha256":"ab12"}"#)
        XCTAssertEqual(info.opener, .passphrase)
        XCTAssertEqual(info.size, 4096)
        XCTAssertEqual(SnapshotInfo(encrypted: false).opener, .clear)
        XCTAssertEqual(SnapshotInfo(encrypted: true, recipientsHint: "x25519").opener, .secretKey)
        XCTAssertEqual(SnapshotInfo(encrypted: true, recipientsHint: "unknown").opener, .unsupported)
    }

    func testProgressDecoding() throws {
        let p = try TalosJSON.decode(EtcdRecoverProgress.self, from: """
        {"phase":"uploading","message":"uploading the snapshot to 10.0.0.2","at":5,"bytes":1024,"total":4096}
        """)
        XCTAssertEqual(p.phase, "uploading")
        XCTAssertEqual(p.bytes, 1024)
        XCTAssertEqual(p.total, 4096)
    }

    func testTimelineSkipsDecryptingForAClearFile() {
        let events = [EtcdRecoverProgress(phase: "uploading"), EtcdRecoverProgress(phase: "bootstrapping")]
        XCTAssertEqual(etcdRecoverTimeline(events, encrypted: false).map(\.phase), [.uploading, .bootstrapping, .waiting])
        XCTAssertEqual(etcdRecoverTimeline(events, encrypted: false).map(\.state), [.done, .current, .pending])
        XCTAssertEqual(etcdRecoverTimeline(events, encrypted: true, finished: true).map(\.state), [.done, .done, .done, .done])
    }

    /// A status as Go encodes it: every field, `error` set when the member did not answer.
    private func status(_ node: String, error: String?, memberId: String = "") -> String {
        let err = error.map { "\"\($0)\"" } ?? "null"
        return #"{"node":"\#(node)","error":\#(err),"memberId":"\#(memberId)","isLeader":false,"isLearner":false,"dbSize":0,"dbSizeInUse":0,"raftIndex":0,"raftTerm":0,"version":"","errors":[]}"#
    }

    func testLostOnlyWhenNoMemberAnswers() throws {
        let down = "etcd is not running"
        let lost = try TalosJSON.decode(EtcdOverview.self, from: """
        {"leaderId":"","members":[],"alarms":[],"statuses":[\(status("10.0.0.2", error: down)),\(status("10.0.0.3", error: down))]}
        """)
        let live = try TalosJSON.decode(EtcdOverview.self, from: """
        {"leaderId":"a1","members":[],"alarms":[],"statuses":[\(status("10.0.0.2", error: down)),\(status("10.0.0.3", error: nil, memberId: "a1"))]}
        """)
        XCTAssertTrue(lost.etcdLost)
        XCTAssertFalse(live.etcdLost)
        XCTAssertEqual(live.recoverCandidates, ["10.0.0.2", "10.0.0.3"])
    }
}
