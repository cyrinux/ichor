import XCTest
@testable import IchorCore

final class TopologyLastKnownTests: XCTestCase {
    private let at = Date(timeIntervalSince1970: 1_700_000_000)

    private let workerUp = TopologyNode(id: "worker-1", node: "10.1.0.10", hostname: "worker-1", role: "worker", addresses: ["10.1.0.10"],
                                      zone: "fr-1", country: "FR", site: "site-1", queried: true)
    private let peer = TopologyNode(id: "peer", node: "10.0.0.2", hostname: "peer", zone: "dk-1", country: "DK", site: "site-2", queried: true)
    // What the core says of a target that does not answer: named by its address, alone.
    private let workerDown = TopologyNode(id: "10.1.0.10", node: "10.1.0.10", hostname: "10.1.0.10", addresses: ["10.1.0.10"],
                                        site: "site-9", queried: true, error: "no route to host")
    private let lone = TopologySite(id: "site-9", kind: "node", nodes: ["10.1.0.10"])

    private var before: ClusterTopology {
        ClusterTopology(nodes: [workerUp, peer], sites: [
            TopologySite(id: "site-1", label: "fr-1", kind: "zone", country: "FR", nodes: ["worker-1"]),
            TopologySite(id: "site-2", label: "dk-1", kind: "zone", country: "DK", nodes: ["peer"]),
        ])
    }

    func testAnUnreachableNodeKeepsItsNameRoleAndZone() {
        let now = ClusterTopology(nodes: [peer, workerDown], sites: [
            TopologySite(id: "site-1", label: "dk-1", kind: "zone", country: "DK", nodes: ["peer"]), lone,
        ])
        let node = mergeLastKnown(current: now, previous: before, previousAt: at).nodes[1]
        XCTAssertEqual(node.id, "10.1.0.10")
        XCTAssertEqual(node.hostname, "worker-1")
        XCTAssertEqual(node.role, "worker")
        XCTAssertEqual(node.zone, "fr-1")
        XCTAssertEqual(node.country, "FR")
        XCTAssertEqual(node.error, "no route to host")
        XCTAssertEqual(node.lastSeen, 1_700_000_000_000)
    }

    func testItGoesBackToTheSiteOfItsZone() {
        let neighbour = TopologyNode(id: "neighbour", node: "10.0.0.3", hostname: "neighbour", zone: "fr-1", site: "site-1", queried: true)
        let now = ClusterTopology(nodes: [neighbour, workerDown], sites: [
            TopologySite(id: "site-1", label: "fr-1", kind: "zone", country: "FR", nodes: ["neighbour"]), lone,
        ])
        let merged = mergeLastKnown(current: now, previous: before, previousAt: at)
        XCTAssertEqual(merged.sites, [TopologySite(id: "site-1", label: "fr-1", kind: "zone", country: "FR", nodes: ["neighbour", "10.1.0.10"])])
        XCTAssertEqual(merged.nodes[1].site, "site-1")
    }

    func testALoneNodeHasItsSiteNamedAsBefore() {
        let now = ClusterTopology(nodes: [workerDown], sites: [lone])
        XCTAssertEqual(mergeLastKnown(current: now, previous: before, previousAt: at).sites,
                       [TopologySite(id: "site-9", label: "fr-1", kind: "zone", country: "FR", nodes: ["10.1.0.10"])])
    }

    func testANodeStillDownKeepsWhenItWasLastSeen() {
        let now = ClusterTopology(nodes: [workerDown], sites: [lone])
        let first = mergeLastKnown(current: now, previous: before, previousAt: at)
        let second = mergeLastKnown(current: now, previous: first, previousAt: at.addingTimeInterval(60)).nodes[0]
        XCTAssertEqual(second.hostname, "worker-1")
        XCTAssertEqual(second.lastSeen, 1_700_000_000_000)
    }

    func testANodeNeverSeenUpStaysNamedByItsAddress() {
        let now = ClusterTopology(nodes: [workerDown])
        XCTAssertEqual(mergeLastKnown(current: now, previous: now, previousAt: at), now)
    }

    func testANameAnotherNodeGoesByIsNotReused() {
        let otherNamesake = TopologyNode(id: "worker-1", node: "10.0.0.5", hostname: "worker-1", queried: true)
        let now = ClusterTopology(nodes: [otherNamesake, workerDown])
        XCTAssertEqual(mergeLastKnown(current: now, previous: before, previousAt: at), now)
    }

    func testReachableNodesAndNoPreviousMapAreLeftAsTheyAre() {
        let now = ClusterTopology(nodes: [TopologyNode(id: "worker-1", node: "10.1.0.10", hostname: "renamed", queried: true)])
        XCTAssertEqual(mergeLastKnown(current: now, previous: nil, previousAt: at), now)
        XCTAssertEqual(mergeLastKnown(current: now, previous: before, previousAt: at), now)
    }
}
