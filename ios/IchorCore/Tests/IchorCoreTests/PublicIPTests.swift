import XCTest
@testable import IchorCore

final class PublicIPTests: XCTestCase {
    private func node(_ addr: String, _ hostname: String, publicIPs: [String] = [], reachable: Bool = true) -> NodeOverview {
        NodeOverview(node: addr, hostname: hostname, reachable: reachable, publicIPs: publicIPs)
    }

    private let report = PublicIPReport(nodes: [
        PublicIPProbe(name: "cp-1", address: "10.0.0.2", publicIP: "203.0.113.2"),
        PublicIPProbe(name: "edge-1", address: "172.16.0.9", publicIP: "198.51.100.9"),
        PublicIPProbe(name: "lan-1", address: "10.0.0.4", error: "no answer"),
    ], at: 1)

    func testMatchesByAddressThenByHostname() {
        XCTAssertEqual(report.ip(for: node("10.0.0.2", "other-name")), "203.0.113.2")
        XCTAssertEqual(report.ip(for: node("192.0.2.9", "edge-1.example.org")), "198.51.100.9")
        XCTAssertNil(report.ip(for: node("10.0.0.4", "lan-1")))
        // An unreachable node's hostname is its address: no name to match.
        XCTAssertNil(report.ip(for: node("10.9.9.9", "10.9.9.9")))
    }

    func testTalosKnowledgeWinsOverAProbe() {
        XCTAssertEqual(node("10.0.0.2", "cp-1", publicIPs: ["2001:db8::2"]).shownPublicIPs(probed: report), ["2001:db8::2"])
        XCTAssertEqual(node("10.0.0.2", "cp-1").shownPublicIPs(probed: report), ["203.0.113.2"])
        XCTAssertEqual(node("10.0.0.2", "cp-1").shownPublicIPs(probed: nil), [])
    }

    func testLacksPublicIPsOnlyCountsNodesThatAnswer() {
        let known = node("10.0.0.2", "cp-1", publicIPs: ["203.0.113.2"])
        XCTAssertFalse(ClusterOverview(context: "c", nodes: [known, node("10.0.0.3", "10.0.0.3", reachable: false)]).lacksPublicIPs)
        XCTAssertTrue(ClusterOverview(context: "c", nodes: [known, node("10.0.0.3", "w-1")]).lacksPublicIPs)
    }

    func testDecodesGoOutput() throws {
        let json = #"{"nodes":[{"name":"a","address":"10.0.0.2","publicIP":"203.0.113.2"},{"name":"b","error":"x"}],"at":5}"#
        let decoded = try JSONDecoder().decode(PublicIPReport.self, from: Data(json.utf8))
        XCTAssertEqual(decoded.nodes.count, 2)
        XCTAssertEqual(decoded.firstError, "b: x")
    }
}
