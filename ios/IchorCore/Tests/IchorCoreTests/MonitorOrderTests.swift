import XCTest
@testable import IchorCore

final class MonitorOrderTests: XCTestCase {
    private let contexts = [
        ContextSummary(name: "alpha", fingerprint: "fa", clusterID: "alpha"),
        ContextSummary(name: "bravo", fingerprint: "fb", clusterID: "bravo"),
        ContextSummary(name: "charlie", fingerprint: "fc", clusterID: "charlie"),
        ContextSummary(name: "delta", kind: ContextKind.kube, fingerprint: "fd"),
    ]

    private func order(active: String?, appActive: Bool, lastFailed: Set<String>) -> [String] {
        monitorOrder(contexts, active: active, appActive: appActive, lastFailed: lastFailed).map(\.name)
    }

    func testStableOrderWithoutFailures() {
        XCTAssertEqual(
            order(active: "bravo", appActive: false, lastFailed: []), ["alpha", "bravo", "charlie", "delta"])
        XCTAssertEqual(order(active: nil, appActive: true, lastFailed: []), ["alpha", "bravo", "charlie", "delta"])
    }

    func testLastFailedFirstInStoredOrder() {
        XCTAssertEqual(
            order(active: nil, appActive: false, lastFailed: ["fd", "bravo"]), ["bravo", "delta", "alpha", "charlie"])
    }

    func testActiveLastWhenTheAppIsInFront() {
        XCTAssertEqual(order(active: "alpha", appActive: true, lastFailed: []), ["bravo", "charlie", "delta", "alpha"])
        // In front, the active cluster waits even when it failed last time: its screen reads it.
        XCTAssertEqual(
            order(active: "charlie", appActive: true, lastFailed: ["charlie", "fd"]),
            ["delta", "alpha", "bravo", "charlie"])
        // In the background it keeps its place among the failed ones.
        XCTAssertEqual(
            order(active: "charlie", appActive: false, lastFailed: ["charlie", "fd"]),
            ["charlie", "delta", "alpha", "bravo"])
    }

    func testEveryContextKeptOnce() {
        let names = order(active: "delta", appActive: true, lastFailed: ["alpha", "unknown"])
        XCTAssertEqual(names.sorted(), contexts.map(\.name).sorted())
    }

    func testBoundIsFour() {
        XCTAssertEqual(monitorParallel, 4)
    }
}
