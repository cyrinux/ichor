import XCTest
@testable import IchorCore

final class EtcdFixTests: XCTestCase {
    func testProgressDecoding() throws {
        let p = try TalosJSON.decode(EtcdFixProgress.self, from: """
        {"phase":"defrag","message":"defragmenting cp-2 (1 of 3)","at":5,"step":2,"steps":4,
         "members":[{"node":"10.0.0.2","hostname":"cp-2","state":"running","reclaimedBytes":0},{"node":"10.0.0.3","state":"odd"}]}
        """)
        XCTAssertEqual(p.step, 2)
        XCTAssertEqual(p.steps, 4)
        XCTAssertEqual(p.members.map(\.memberState), [.running, .pending])
        XCTAssertEqual(try TalosJSON.decode(EtcdFixProgress.self, from: #"{"phase":"snapshot","members":null}"#).members, [])
    }

    func testNospace() throws {
        let with = try TalosJSON.decode(EtcdOverview.self, from: """
        {"leaderId":"a1","members":[],"statuses":[],"alarms":[{"memberId":"a1","alarm":"NOSPACE"}]}
        """)
        let without = try TalosJSON.decode(EtcdOverview.self, from: """
        {"leaderId":"a1","members":[],"statuses":[],"alarms":[{"memberId":"a1","alarm":"CORRUPT"}]}
        """)
        XCTAssertTrue(with.hasNospace)
        XCTAssertFalse(without.hasNospace)
    }

    func testTimeline() {
        let events = [EtcdFixProgress(phase: "snapshot"), EtcdFixProgress(phase: "defrag")]
        XCTAssertEqual(etcdFixTimeline(events).map(\.state), [.done, .current, .pending, .pending])
        XCTAssertEqual(etcdFixTimeline(events, finished: true).map(\.state), [.done, .done, .done, .done])
        XCTAssertEqual(etcdFixTimeline(events, failure: "defragmenting cp-2 failed").map(\.state), [.done, .failed, .pending, .pending])
    }
}
