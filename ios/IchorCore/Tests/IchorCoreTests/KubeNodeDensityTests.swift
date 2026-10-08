import XCTest
@testable import IchorCore

final class KubeNodeDensityTests: XCTestCase {
    private func node(_ i: Int, ready: Bool = true, cordoned: Bool = false, pressure: [String] = []) -> KubeNodeInfo {
        KubeNodeInfo(name: "ip-10-0-0-\(i)", ready: ready, cordoned: cordoned, internalIP: "10.0.0.\(i)", pressure: pressure)
    }

    private func cluster(_ size: Int) -> [KubeNodeInfo] { (1...size).map { node($0) } }

    func testStatusIsTheWorstOfReadyCordonAndPressure() {
        XCTAssertEqual(node(1).status, .ready)
        XCTAssertEqual(node(1, cordoned: true).status, .attention)
        XCTAssertEqual(node(1, pressure: ["DiskPressure"]).status, .attention)
        // Not ready outranks a cordon: the node is down, the cordon is a detail.
        XCTAssertEqual(node(1, ready: false, cordoned: true).status, .notReady)
    }

    func testCountsNodesPerStatus() {
        let nodes = cluster(40).enumerated().map { i, n -> KubeNodeInfo in
            if i < 2 { return node(i + 1, ready: false) }
            if i < 5 { return node(i + 1, cordoned: true) }
            return n
        }
        XCTAssertEqual(nodes.healthCounts, HealthCounts(ready: 35, attention: 3, notReady: 2))
        XCTAssertEqual([KubeNodeInfo]().healthCounts, HealthCounts())
    }

    func testGroupsByStatusWorstFirstKeepingTheOrderWithin() {
        let nodes = [node(1), node(2, cordoned: true), node(3, ready: false), node(4), node(5, pressure: ["MemoryPressure"])]
        let groups = nodes.byStatus
        XCTAssertEqual(groups.map(\.status), [.notReady, .attention, .ready])
        XCTAssertEqual(groups[0].nodes.map(\.name), ["ip-10-0-0-3"])
        XCTAssertEqual(groups[1].nodes.map(\.name), ["ip-10-0-0-2", "ip-10-0-0-5"])
        XCTAssertEqual(groups[2].nodes.map(\.name), ["ip-10-0-0-1", "ip-10-0-0-4"])
        // No empty group: a healthy cluster is one group.
        XCTAssertEqual(cluster(3).byStatus.map(\.status), [.ready])
    }

    func testProblemNodesAreWorstFirstAndCapped() {
        let nodes = cluster(30).enumerated().map { i, n -> KubeNodeInfo in
            if i < 6 { return node(i + 1, cordoned: true) }
            if i < 8 { return node(i + 1, ready: false) }
            return n
        }
        let problems = nodes.problemNodes()
        // The two not-ready ones first, then the cordoned, cut at five.
        XCTAssertEqual(problems.shown.map(\.name), ["ip-10-0-0-7", "ip-10-0-0-8", "ip-10-0-0-1", "ip-10-0-0-2", "ip-10-0-0-3"])
        XCTAssertEqual(problems.more, 3)
        XCTAssertEqual(cluster(3).problemNodes(), KubeProblemNodes(shown: [], more: 0))
    }

    func testFiltersByStatusAndQuery() {
        let nodes = [node(1), node(2, cordoned: true), node(3, ready: false), node(14)]
        XCTAssertEqual(nodes.filtered(query: "", filter: .attention).map(\.name), ["ip-10-0-0-2", "ip-10-0-0-3"])
        XCTAssertEqual(nodes.filtered(query: "", filter: .ready).map(\.name), ["ip-10-0-0-1", "ip-10-0-0-2", "ip-10-0-0-14"])
        XCTAssertEqual(nodes.filtered(query: "", filter: .notReady).map(\.name), ["ip-10-0-0-3"])
        // Nothing is unreachable to Kubernetes: the filter is not offered, and matches nothing.
        XCTAssertEqual(nodes.filtered(query: "", filter: .unreachable), [])
        XCTAssertFalse(kubeNodeFilters.contains(.unreachable))
        // The query matches the name or the address, case-insensitively, trimmed.
        XCTAssertEqual(nodes.filtered(query: " 0-1 ", filter: nil).map(\.name), ["ip-10-0-0-1", "ip-10-0-0-14"])
        XCTAssertEqual(nodes.filtered(query: "10.0.0.14", filter: nil).map(\.name), ["ip-10-0-0-14"])
        XCTAssertEqual(nodes.filtered(query: "IP-10-0-0-2", filter: .attention).map(\.name), ["ip-10-0-0-2"])
        XCTAssertEqual(nodes.filtered(query: "", filter: nil), nodes)
    }

    func testExternalAddressMatchesToo() {
        let nodes = [KubeNodeInfo(name: "w-1", ready: true, externalIP: "203.0.113.9")]
        XCTAssertEqual(nodes.filtered(query: "203.0", filter: nil).count, 1)
    }
}
