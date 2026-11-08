import XCTest
@testable import IchorCore

final class PodSweepStateTests: XCTestCase {
    func testDecodesTheProgressAndTheResult() throws {
        let progress = try TalosJSON.decode(
            PodSweepProgress.self, from: #"{"done":12,"total":140,"name":"shop/web-1","failed":1,"at":1700000000000}"#)
        XCTAssertEqual(progress, PodSweepProgress(done: 12, total: 140, name: "shop/web-1", failed: 1))

        let result = try TalosJSON.decode(
            KubeSanitizeResult.self,
            from: #"{"deleted":1,"failed":[{"name":"shop/gone","error":"the pod no longer exists"}],"stopped":true}"#)
        XCTAssertEqual(
            result,
            KubeSanitizeResult(
                deleted: 1, failed: [SanitizeFailure(name: "shop/gone", error: "the pod no longer exists")],
                stopped: true))
        // The synchronous sweep's result, before "stopped" existed.
        XCTAssertFalse(try TalosJSON.decode(KubeSanitizeResult.self, from: #"{"deleted":2,"failed":[]}"#).stopped)
    }

    func testASweepCountsThenEndsWithItsResult() {
        var state = PodSweepState(context: "admin@test", namespace: "shop", total: 3)
        XCTAssertTrue(state.running)
        XCTAssertEqual(state.fraction, 0)

        state.apply(PodSweepProgress(done: 2, total: 3, failed: 1))
        XCTAssertEqual(state.done, 2)
        XCTAssertEqual(state.deleted, 1)
        XCTAssertEqual(state.failedCount, 1)
        XCTAssertEqual(state.fraction, 2.0 / 3.0, accuracy: 0.001)

        state.finish(
            KubeSanitizeResult(deleted: 2, failed: [SanitizeFailure(name: "shop/gone", error: "gone")]), error: nil)
        XCTAssertFalse(state.running)
        XCTAssertEqual(state.done, 3)
        XCTAssertEqual(state.deleted, 2)
        XCTAssertEqual(state.failures.map(\.name), ["shop/gone"])
        XCTAssertNil(state.error)
        XCTAssertFalse(state.stopped)
    }

    func testAStoppedOrRefusedSweep() {
        var stopped = PodSweepState(context: "admin@test", namespace: "", total: 5)
        stopped.stopping = true
        stopped.finish(KubeSanitizeResult(deleted: 1, stopped: true), error: nil)
        XCTAssertTrue(stopped.stopped)
        XCTAssertEqual(stopped.done, 1)

        var refused = PodSweepState(context: "demo", namespace: "demo", total: 1)
        refused.finish(KubeSanitizeResult(), error: "this action is unavailable in the demo cluster")
        XCTAssertEqual(refused.error, "this action is unavailable in the demo cluster")
        XCTAssertFalse(refused.running)
    }

    func testCountsTheNamesASweepTakes() {
        XCTAssertEqual(sanitizeCount("shop/a\n\nshop/b\n \nshop/c"), 3)
        XCTAssertEqual(sanitizeCount(""), 0)
    }
}
