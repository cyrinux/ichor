import XCTest
@testable import IchorCore

final class TopologyTests: XCTestCase {
    func testFlagFromCountryCode() {
        XCTAssertEqual(countryFlag("FR"), "🇫🇷")
        XCTAssertEqual(countryFlag("dk"), "🇩🇰")
        XCTAssertEqual(countryFlag(""), "")
        XCTAssertEqual(countryFlag("FRA"), "")
        XCTAssertEqual(countryFlag("F1"), "")
        XCTAssertEqual(countryFlag("éé"), "")
        XCTAssertEqual(countryFlag("ß"), "")
    }

    func testDecodesTheBrokenVerdictAndTheMesh() throws {
        let json = """
            {"links":[{"a":"node-a","b":"node-b","state":"unknown","broken":true,"reason":"neverHandshaked","since":0,"sides":[]},
              {"a":"node-a","b":"node-c","state":"unknown","broken":false,"since":0,"sides":[]},
              {"a":"node-b","b":"node-c","state":"up","broken":true,"reason":"oneSide","since":1800000000,"sides":[]}],
             "mesh":{"checked":true,"links":3,"broken":2,"pairs":["node-a ↔ node-b","node-b ↔ node-c"]}}
            """
        let topology = try JSONDecoder().decode(ClusterTopology.self, from: Data(json.utf8))

        XCTAssertEqual(topology.links.map(\.isBroken), [true, false, true])
        XCTAssertEqual(topology.links.map(\.isOneSided), [false, false, true])
        XCTAssertEqual(topology.links[0].reason, "neverHandshaked")
        XCTAssertEqual(topology.links[2].since, 1_800_000_000)
        XCTAssertEqual(
            topology.mesh, TopologyMesh(checked: true, links: 3, broken: 2, pairs: ["node-a ↔ node-b", "node-b ↔ node-c"]))
        XCTAssertEqual(topology.brokenLinks(of: "node-b"), 2)
    }

    func testAMapCachedBeforeTheVerdictJudgesByItsState() throws {
        let json = """
            {"links":[{"a":"a","b":"b","state":"down"},{"a":"a","b":"c","state":"degraded"},
              {"a":"b","b":"c","state":"unknown"},{"a":"c","b":"d","state":"up"}]}
            """
        let topology = try JSONDecoder().decode(ClusterTopology.self, from: Data(json.utf8))

        XCTAssertEqual(topology.links.map(\.isBroken), [true, true, false, false])
        XCTAssertEqual(topology.links.map(\.isOneSided), [false, true, false, false])
        XCTAssertEqual(topology.mesh, TopologyMesh())
    }

    func testPeersCachedBeforeTheVerdictJudgeByTheirState() throws {
        let json = """
            {"node":"10.0.0.1","down":1,"peers":[{"publicKey":"k","label":"node-b","state":"down"}]}
            """
        let node = try JSONDecoder().decode(KubeSpanNode.self, from: Data(json.utf8))

        XCTAssertEqual(node.broken, 1)
        XCTAssertEqual(node.peers.first?.broken, true)
    }

    func testMergingLastKnownKeepsTheMesh() {
        let mesh = TopologyMesh(checked: true, links: 1, broken: 1, pairs: ["node-a ↔ node-b"])
        let down = TopologyNode(id: "10.0.0.2", node: "10.0.0.2", queried: true, error: "unreachable")
        let before = TopologyNode(id: "node-b", node: "10.0.0.2", hostname: "node-b", queried: true)
        let merged = mergeLastKnown(
            current: ClusterTopology(nodes: [down], mesh: mesh), previous: ClusterTopology(nodes: [before]),
            previousAt: Date())

        XCTAssertEqual(merged.nodes.first?.hostname, "node-b")
        XCTAssertEqual(merged.mesh, mesh)
    }

    func testDecodesTheGoCoreJSON() throws {
        let json = """
            {"nodes":[{"id":"cp-1","node":"10.0.0.1","hostname":"cp-1","role":"controlplane","addresses":["10.0.0.1"],
              "zone":"fr-par-1","country":"FR","site":"site-1","kubespan":true,"queried":true},
              {"id":"vps","hostname":"vps","addresses":null,"site":"site-2","kubespan":false,"queried":false}],
             "links":[{"a":"cp-1","b":"vps","state":"degraded","sides":[{"from":"cp-1","to":"vps","state":"up",
              "endpoint":"203.0.113.5:51820","private":false,"rx":10,"tx":20,"lastHandshake":1800000000}]}],
             "sites":[{"id":"site-1","label":"fr-par-1","kind":"zone","country":"FR","nodes":["cp-1"]},
              {"id":"site-2","label":"","kind":"node","nodes":["vps"]}]}
            """
        let topology = try JSONDecoder().decode(ClusterTopology.self, from: Data(json.utf8))

        XCTAssertEqual(topology.nodes.count, 2)
        XCTAssertEqual(topology.sites[0].country, "FR")
        XCTAssertEqual(topology.nodes[1].node, "")
        XCTAssertEqual(topology.nodes[1].addresses, [])
        XCTAssertEqual(topology.brokenLinks(of: "vps"), 1)
        XCTAssertEqual(topology.brokenLinks(of: "nobody"), 0)
        XCTAssertEqual(topology.links[0].sides[0].endpoint, "203.0.113.5:51820")
        XCTAssertFalse(topology.links[0].sides[0].private)
    }

    func testDecodesNullSlices() throws {
        let topology = try JSONDecoder().decode(
            ClusterTopology.self, from: Data(#"{"nodes":null,"links":null,"sites":null}"#.utf8))
        XCTAssertEqual(topology, ClusterTopology())
    }

    private func node(_ hostname: String, role: String = "worker", target: String? = nil) -> NodeOverview {
        NodeOverview(node: target ?? hostname, hostname: hostname, reachable: true, role: role)
    }

    func testGroupsNodesSiteBySiteInTheMapOrder() {
        let topology = ClusterTopology(
            nodes: [TopologyNode(id: "cp-1", node: "10.0.0.1"), TopologyNode(id: "w-2", node: "10.0.0.3")],
            sites: [
                TopologySite(id: "site-1", label: "fr-par-1", nodes: ["w-2", "renamed"]),
                TopologySite(id: "site-2", label: "nl-ams-1", nodes: ["cp-1", "w-1"]),
                TopologySite(id: "site-3", nodes: ["gone"]),
            ]
        )
        let nodes = [
            node("w-1"), node("cp-1", role: "controlplane"), node("new-name", target: "10.0.0.3"), node("w-9"),
        ]

        let groups = groupNodes(nodes, by: topology)

        XCTAssertEqual(groups.map(\.site?.id), ["site-1", "site-2", nil])
        XCTAssertEqual(groups.map { $0.nodes.map(\.hostname) }, [["new-name"], ["cp-1", "w-1"], ["w-9"]])
        XCTAssertEqual(groups.map(\.key), ["site-1", "site-2", ""])
    }

    func testOneGroupWithoutAMap() {
        let nodes = [node("w-1"), node("cp-1", role: "controlplane")]

        let groups = groupNodes(nodes, by: nil)

        XCTAssertEqual(groups.count, 1)
        XCTAssertNil(groups[0].site)
        XCTAssertEqual(groups[0].nodes.map(\.hostname), ["cp-1", "w-1"])
        XCTAssertEqual(groupNodes(nodes, by: ClusterTopology()), groups)
    }
}
