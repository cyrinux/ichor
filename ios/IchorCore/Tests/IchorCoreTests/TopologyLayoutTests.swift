import XCTest
@testable import IchorCore

/// Same cases as Android's TopologyLayoutTest and the map's part of NetPerfTest.
final class TopologyLayoutTests: XCTestCase {
    private let topology = ClusterTopology(
        nodes: ["a", "b", "c", "d", "e"].map { TopologyNode(id: $0, site: $0 == "e" ? "s2" : "s1") },
        links: [TopologyLink(a: "a", b: "b", state: "up"), TopologyLink(a: "a", b: "e", state: "down")],
        sites: [TopologySite(id: "s1", nodes: ["a", "b", "c", "d"]), TopologySite(id: "s2", nodes: ["e"])]
    )

    func testNodesSitInsideTheirSite() throws {
        let layout = topologyLayout(topology, width: 300)

        XCTAssertEqual(layout.sites.count, 2)
        for box in layout.sites {
            for id in box.site.nodes {
                let p = try XCTUnwrap(layout.nodes[id])
                XCTAssertGreaterThan(p.y, box.top + TopologyLayout.headerHeight - 1, "\(id) above its site")
                XCTAssertLessThan(p.y, box.top + box.height, "\(id) below its site")
                XCTAssertTrue((0...300).contains(p.x), "\(id) outside the width")
            }
        }
        // Four nodes: a row of three, then the fourth centred on the next row.
        XCTAssertEqual(try XCTUnwrap(layout.nodes["d"]).x, 150, accuracy: 0.01)
        XCTAssertEqual(layout.cellWidth, 100, accuracy: 0.01)
        XCTAssertGreaterThanOrEqual(layout.sites[1].top, layout.sites[0].top + layout.sites[0].height)
        XCTAssertEqual(layout.sites[1].top + layout.sites[1].height, layout.height, accuracy: 0.01)
    }

    func testRowsGrowWithTheTextButNeverShrink() {
        let base = topologyLayout(topology, width: 300)
        XCTAssertGreaterThan(topologyLayout(topology, width: 300, textScale: 1.5).height, base.height)
        XCTAssertEqual(topologyLayout(topology, width: 300, textScale: 0.8), base)
    }

    func testCrossSiteLinksBowToTheRight() throws {
        let layout = topologyLayout(topology, width: 300)
        let a = try XCTUnwrap(layout.nodes["a"])
        let e = try XCTUnwrap(layout.nodes["e"])

        XCTAssertEqual(layout.control(a, e, sameSite: true), MapPoint(x: (a.x + e.x) / 2, y: (a.y + e.y) / 2))
        XCTAssertGreaterThan(layout.control(a, e, sameSite: false).x, (a.x + e.x) / 2)
        XCTAssertLessThanOrEqual(layout.control(a, e, sameSite: false).x, 300 - 8)
    }

    func testMiddleOfALinkIsOnItsCurve() throws {
        let layout = topologyLayout(topology, width: 300)
        let a = try XCTUnwrap(layout.nodes["a"])
        let b = try XCTUnwrap(layout.nodes["b"])
        XCTAssertEqual(layout.middle(of: topology.links[0], in: topology), MapPoint(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2))
        // Across sites the middle is pushed right like its control point, by half the bow.
        let middle = try XCTUnwrap(layout.middle(of: topology.links[1], in: topology))
        XCTAssertGreaterThan(middle.x, try XCTUnwrap(layout.nodes["a"]).x)
        XCTAssertNil(layout.middle(of: TopologyLink(a: "a", b: "gone"), in: topology))
    }

    func testTapFindsTheClosestLink() throws {
        let layout = topologyLayout(topology, width: 300)
        let a = try XCTUnwrap(layout.nodes["a"])
        let b = try XCTUnwrap(layout.nodes["b"])
        let midAB = MapPoint(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2)

        XCTAssertEqual(layout.linkAt(topology, at: midAB, slop: 10), 0)
        XCTAssertNil(layout.linkAt(topology, at: MapPoint(x: 0, y: layout.height + 200), slop: 10))
    }

    func testEmptyTopology() {
        let layout = topologyLayout(ClusterTopology(), width: 300)
        XCTAssertEqual(layout.height, 0)
        XCTAssertTrue(layout.nodes.isEmpty)
        XCTAssertEqual(layout.cellWidth, 300)
    }

    func testQuadraticEnds() {
        let a = MapPoint(x: 0, y: 0), c = MapPoint(x: 10, y: 0), b = MapPoint(x: 10, y: 10)
        XCTAssertEqual(quadratic(a, c, b, t: 0), a)
        XCTAssertEqual(quadratic(a, c, b, t: 1), b)
        XCTAssertEqual(quadratic(a, c, b, t: 0.5), MapPoint(x: 7.5, y: 2.5))
    }

    // MARK: Network test from the map

    func testLatestBetweenTakesEitherDirectionWithAThroughput() {
        let ok = [NetPerfResult(path: NetPerfPath.pod, test: NetPerfTest.throughput, throughputMbps: 900)]
        let failed = [NetPerfResult(path: NetPerfPath.pod, test: NetPerfTest.throughput, error: "refused")]
        let history = [
            NetPerfReport(server: "b", client: "a", started: 4, results: failed),
            NetPerfReport(server: "a", client: "b", started: 3, results: ok),
            NetPerfReport(server: "b", client: "a", started: 2, results: ok),
            NetPerfReport(server: "c", client: "a", started: 5, results: ok),
        ]
        XCTAssertEqual(history.latestBetween("a", "b")?.started, 3)
        XCTAssertEqual(history.latestBetween("b", "a")?.started, 3)
        XCTAssertNil(history.latestBetween("b", "c"))
        XCTAssertEqual(history.latestBetween("c", "a")?.podThroughputMbps, 900)
        XCTAssertNil(history[0].podThroughputMbps)
    }

    func testPickingOnTheMapTakesTheClientThenTheServer() {
        XCTAssertEqual([String]().pickingNode("a"), ["a"])
        XCTAssertEqual(["a"].pickingNode("b"), ["a", "b"])
        // Tapping the picked node again drops it.
        XCTAssertEqual(["a"].pickingNode("a"), [])
        XCTAssertEqual(["a", "b"].pickingNode("b"), ["a"])
        // A third node starts a new pair.
        XCTAssertEqual(["a", "b"].pickingNode("c"), ["c"])
    }

    func testBetweenKeepsOnlyReadyNodes() {
        let nodes = [
            NetPerfNode(name: "w-1", ready: true),
            NetPerfNode(name: "w-2", ready: true),
            NetPerfNode(name: "w-3", ready: false),
        ]
        let setup = NetPerfSetup(seconds: 20).between(client: "w-2", server: "w-1", nodes: nodes)
        XCTAssertEqual(setup.client, "w-2")
        XCTAssertEqual(setup.server, "w-1")
        XCTAssertEqual(setup.seconds, 20)
        // Unknown or not ready: the default pair fills in, like a refreshed node list.
        let fallback = NetPerfSetup().between(client: "w-3", server: "gone", nodes: nodes)
        XCTAssertTrue(fallback.ready)
        XCTAssertNotEqual(fallback.client, "w-3")
        XCTAssertNotEqual(fallback.server, "gone")
        // Node list not loaded yet: kept as asked, checked once it loads.
        XCTAssertEqual(NetPerfSetup().between(client: "x", server: "y", nodes: nil).client, "x")
    }
}
