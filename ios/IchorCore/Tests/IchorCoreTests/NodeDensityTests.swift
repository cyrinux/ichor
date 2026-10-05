import XCTest
@testable import IchorCore

final class NodeDensityTests: XCTestCase {
    private func node(_ i: Int, reachable: Bool = true, ready: Bool = true, error: String? = nil, publicIPs: [String] = []) -> NodeOverview {
        NodeOverview(node: "10.0.0.\(i)", hostname: "node-\(i)", reachable: reachable, error: error, ready: ready, publicIPs: publicIPs)
    }

    private func cluster(_ size: Int) -> [NodeOverview] { (1...size).map { node($0) } }

    func testDenseOnlyAboveTheThreshold() {
        XCTAssertFalse(isDenseCluster(cluster(3).count))
        XCTAssertFalse(isDenseCluster(cluster(24).count))
        XCTAssertTrue(isDenseCluster(cluster(25).count))
        XCTAssertTrue(isDenseCluster(cluster(200).count))
    }

    func testCountsNodesPerHealth() {
        let nodes = cluster(200).enumerated().map { i, n -> NodeOverview in
            if i < 3 { return node(i + 1, ready: false) }
            if i < 4 { return node(i + 1, reachable: false) }
            return n
        }
        let counts = nodes.healthCounts
        XCTAssertEqual(counts, HealthCounts(ready: 196, notReady: 3, unreachable: 1))
        XCTAssertEqual(counts.total, 200)
        XCTAssertEqual(counts[.notReady], 3)
        XCTAssertEqual([NodeOverview]().healthCounts, HealthCounts())
    }

    func testReadyNodeReportingAProblemIsNotCountedCalm() {
        let condition = NodeOverview(
            node: "10.0.0.2", hostname: "node-2", reachable: true, ready: true,
            unmetConditions: [UnmetCondition(name: "services", reason: "etcd not healthy")]
        )
        let nodes = [node(1), condition, node(3, error: "disk pressure"), node(4, reachable: false)]
        XCTAssertEqual(nodes.map(\.status), [.ready, .attention, .attention, .unreachable])
        XCTAssertEqual(nodes.healthCounts, HealthCounts(ready: 1, attention: 2, unreachable: 1))
        XCTAssertEqual(nodes.healthCounts.total, 4)
        // Listed as problems, like the dots say.
        XCTAssertEqual(nodes.problemNodes().shown.map(\.hostname), ["node-4", "node-2", "node-3"])
    }

    func testProblemsWorstFirstCappedAtFive() {
        let nodes = cluster(200).enumerated().map { i, n -> NodeOverview in
            switch i {
            case 0: return node(1, error: "disk pressure")
            case 1, 2, 3: return node(i + 1, ready: false)
            case 10, 11, 12: return node(i + 1, reachable: false)
            default: return n
            }
        }
        let problems = nodes.problemNodes()
        XCTAssertEqual(problems.shown.map(\.hostname), ["node-11", "node-12", "node-13", "node-2", "node-3"])
        XCTAssertEqual(problems.more, 2)
    }

    func testFewProblemsAreAllShown() {
        var nodes = cluster(25)
        nodes[24] = node(25, reachable: false)
        XCTAssertEqual(nodes.problemNodes(), ProblemNodes(shown: [nodes[24]], more: 0))
        XCTAssertEqual(cluster(3).problemNodes(), ProblemNodes(shown: [], more: 0))
    }

    func testOverviewOrderPutsControlPlanesFirst() {
        let nodes = [
            NodeOverview(node: "a", hostname: "w-2", reachable: true, role: "worker"),
            NodeOverview(node: "b", hostname: "cp-2", reachable: true, role: "controlplane"),
            NodeOverview(node: "c", hostname: "w-1", reachable: true, role: "worker"),
            NodeOverview(node: "d", hostname: "cp-1", reachable: true, role: "controlplane"),
        ]
        XCTAssertEqual(nodes.overviewOrder.map(\.hostname), ["cp-1", "cp-2", "w-1", "w-2"])
    }

    func testFiltersBySearchAndHealth() {
        let nodes = [node(1), node(2, reachable: false), node(3, publicIPs: ["203.0.113.9"]), node(4), node(5, ready: false)]
        XCTAssertEqual(nodes.filtered(query: "", filter: nil), nodes)
        XCTAssertEqual(nodes.filtered(query: "", filter: .attention).map(\.hostname), ["node-2", "node-5"])
        XCTAssertEqual(nodes.filtered(query: "", filter: .notReady).map(\.hostname), ["node-5"])
        XCTAssertEqual(nodes.filtered(query: "", filter: .ready).map(\.hostname), ["node-1", "node-3", "node-4"])
        XCTAssertEqual(nodes.filtered(query: " 10.0.0.4 ", filter: nil).map(\.hostname), ["node-4"])
        XCTAssertEqual(nodes.filtered(query: "203.0", filter: nil).map(\.hostname), ["node-3"])
        XCTAssertEqual(nodes.filtered(query: "NODE-5", filter: .notReady).map(\.hostname), ["node-5"])
        XCTAssertEqual(nodes.filtered(query: "nothing", filter: nil), [])
    }
}
