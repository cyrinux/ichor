import XCTest

@testable import IchorCore

final class BGPAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private let node = "192.0.2.20"
    private var key: String { "\(node)|fabric|198.51.100.2" }
    private let value = "warning|node-a|fabric|198.51.100.2|ACTIVE"
    private var down: BGPDown {
        BGPDown(node: node, hostname: "node-a", instance: "fabric", peer: "198.51.100.2", state: "ACTIVE")
    }

    private func snap(_ issues: [String: String]?, watched: Bool = true) -> ClusterSnapshot {
        ClusterSnapshot(
            context: "lab", takenAt: now, nodes: [node: NodeState(hostname: "node-a", health: .ready)],
            etcdChecked: true, bgpWatched: watched, bgpChecked: watched && issues != nil, bgpIssues: issues ?? [:])
    }

    private func report(_ down: [BGPDown], error: String = "") -> BGPPeerReport {
        BGPPeerReport(nodes: [BGPNodePeers(node: node, hostname: "node-a", supported: true, error: error)], down: down)
    }

    func testTheGoJSONDecodes() throws {
        let report = try TalosJSON.decode(
            BGPPeerReport.self,
            from: """
                {"context":"lab","nodes":[{"node":"\(node)","hostname":"node-a","supported":true,"peers":[
                {"instance":"fabric","peer":"198.51.100.1","peerAsn":64512,"localAsn":64601,"state":"ESTABLISHED",
                "established":true,"since":"2026-11-20T08:30:00Z","received":14,"advertised":2,
                "gracefulRestart":true,"peerRestarting":false}]}],
                "down":[{"node":"\(node)","hostname":"node-a","instance":"fabric","peer":"198.51.100.2","state":"ACTIVE"}]}
                """)
        XCTAssertEqual(bgpIssuesOf(report), [key: value])
        let peer = try XCTUnwrap(report.nodes.first?.peers.first)
        XCTAssertEqual(peer.peerAsn, 64512)
        XCTAssertFalse(peer.isDown)
        XCTAssertEqual(peer.sinceDate, Date(timeIntervalSince1970: 1_795_163_400))
        let issue = BGPIssue(value: value)
        XCTAssertEqual(issue.hostname, "node-a")
        XCTAssertEqual(issue.peer, "198.51.100.2")
        XCTAssertEqual(issue.state, "ACTIVE")
    }

    func testTheNetworkDecodesItsSessions() throws {
        let network = try TalosJSON.decode(
            NodeNetwork.self,
            from: """
                {"links":null,"bgpSupported":true,"bgpPeers":[{"instance":"edge","peer":"link/eth1","state":"IDLE",
                "gracefulRestart":true,"peerRestarting":true}],"errors":{}}
                """)
        XCTAssertTrue(network.showsBGP)
        XCTAssertFalse(network.bgpPeers[0].isDown)
        XCTAssertFalse(NodeNetwork(bgpSupported: true).showsBGP)
        XCTAssertFalse(NodeNetwork(bgpPeers: network.bgpPeers, bgpSupported: false).showsBGP)
        XCTAssertTrue(NodeNetwork(bgpSupported: true, errors: ["bgp": "refused"]).showsBGP)
    }

    func testDownOnceIsPendingThenNotifiedAtTheNextCheck() {
        let first = evaluate(previous: snap([:]), current: snap(bgpIssuesOf(report([down]))), now: now)
        XCTAssertTrue(first.alerts.isEmpty)
        XCTAssertEqual(first.next.bgpPending, [key])

        let second = evaluate(previous: first.next, current: snap(bgpIssuesOf(report([down]))), now: now)
        XCTAssertEqual(second.alerts.map(\.key), ["bgp:\(key)"])
        XCTAssertEqual(second.alerts.first?.title, "BGP session to 198.51.100.2 is down on node-a")

        let back = evaluate(previous: second.next, current: snap(bgpIssuesOf(report([]))), now: now)
        XCTAssertEqual(back.alerts.map(\.title), ["BGP session to 198.51.100.2 is up again on node-a"])
    }

    func testARebootBlipNeverNotifies() {
        let first = evaluate(previous: snap([:]), current: snap(bgpIssuesOf(report([down]))), now: now)
        let second = evaluate(previous: first.next, current: snap(bgpIssuesOf(report([]))), now: now)
        XCTAssertTrue(second.alerts.isEmpty)
        XCTAssertTrue(second.next.bgpPending.isEmpty)
    }

    func testAnUnreadNodeKeepsItsKnownIssues() {
        let known = [key: value]
        XCTAssertEqual(bgpIssuesOf(report([], error: "timeout"), known: known), known)
        XCTAssertEqual(bgpIssuesOf(report([]), known: known), [:])
    }

    func testASeparatorInTheTextNeverShiftsTheFields() {
        let odd = BGPDown(node: node, hostname: "node-a", instance: "a|b", peer: "link/eth1", state: "IDLE")
        let issue = BGPIssue(value: bgpIssuesOf(report([odd])).values.first ?? "")
        XCTAssertEqual(issue.instance, "a/b")
        XCTAssertEqual(issue.peer, "link/eth1")
        XCTAssertEqual(issue.state, "IDLE")
    }

    func testItOpensTheNodeNetworkScreen() {
        XCTAssertEqual(
            ShareTarget.forAlert(key: "bgp:\(key)", snapshot: snap([key: value])),
            .network(address: node, hostname: "node-a"))
        let alert = Alert(key: "bgp:\(key)", title: "", text: "", problem: true)
        XCTAssertEqual(alertRuleKind(key: alert.key), "bgp")
        XCTAssertEqual(alertRuleSeverity(alert, snapshot: nil, now: now), alertSeverityWarning)
    }

    func testAnOldSnapshotDecodes() throws {
        let json = #"{"context":"lab","takenAt":0,"nodes":{}}"#
        let snapshot = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(json.utf8))
        XCTAssertFalse(snapshot.bgpWatched)
        XCTAssertTrue(snapshot.bgpIssues.isEmpty)
    }
}
