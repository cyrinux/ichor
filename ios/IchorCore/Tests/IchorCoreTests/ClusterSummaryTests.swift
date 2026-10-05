import XCTest
@testable import IchorCore

final class ClusterSummaryTests: XCTestCase {
    private let gib: UInt64 = 1 << 30

    private func node(_ addr: String, ready: Bool = true, reachable: Bool = true, version: String = "v1.11.2",
                      cpus: Int = 4, mem: UInt64? = nil, free: UInt64? = nil) -> NodeOverview {
        NodeOverview(node: addr, hostname: addr, reachable: reachable, version: version, ready: ready,
                     cpuCount: cpus, memTotal: mem ?? 8 * gib, memAvailable: free ?? 4 * gib)
    }

    func testAllReadyIsHealthyWithSummedCapacity() {
        let s = ClusterSummary(nodes: [node("a"), node("b", cpus: 8, mem: 16 * gib, free: 2 * gib)])
        XCTAssertEqual(s.status, .healthy)
        XCTAssertEqual(s.total, 2)
        XCTAssertEqual(s.ready, 2)
        XCTAssertEqual(s.cpuCount, 12)
        XCTAssertEqual(s.memTotal, 24 * gib)
        XCTAssertEqual(s.memAvailable, 6 * gib)
        XCTAssertEqual(s.versions, ["v1.11.2"])
        XCTAssertEqual(s.versionSpan, "v1.11.2")
    }

    func testAnyNodeNotReadyOrUnreachableIsDegraded() {
        let s = ClusterSummary(nodes: [node("a"), node("b", ready: false), node("c", reachable: false, cpus: 0, mem: 0, free: 0)])
        XCTAssertEqual(s.status, .degraded)
        XCTAssertEqual(s.ready, 1)
        XCTAssertEqual(s.notReady, 1)
        XCTAssertEqual(s.unreachable, 1)
        XCTAssertEqual(s.cpuCount, 8)
    }

    func testNoReachableNodeIsDown() {
        let s = ClusterSummary(nodes: [node("a", reachable: false), node("b", reachable: false)])
        XCTAssertEqual(s.status, .down)
        // An unreachable node's last answer must not count.
        XCTAssertEqual(s.cpuCount, 0)
        XCTAssertEqual(s.memTotal, 0)
        XCTAssertNil(s.versionSpan)
    }

    func testNoNodesIsDown() {
        XCTAssertEqual(ClusterSummary(nodes: []).status, .down)
    }

    func testVersionsAreDistinctAndOrderedNumerically() {
        let s = ClusterSummary(nodes: [node("a", version: "v1.10.0"), node("b", version: "v1.9.5"), node("c", version: "v1.10.0"), node("d", version: "")])
        XCTAssertEqual(s.versions, ["v1.9.5", "v1.10.0"])
        XCTAssertEqual(s.versionSpan, "v1.9.5 – v1.10.0")
    }

    func testMemoryFractionIsNilWhenUnknown() {
        XCTAssertNil(ClusterSummary(nodes: [node("a", mem: 0, free: 0)]).memUsedFraction)
        XCTAssertEqual(ClusterSummary(nodes: [node("a", mem: 8 * gib, free: 2 * gib)]).memUsedFraction ?? 0, 0.75, accuracy: 0.001)
    }
}
