import XCTest
@testable import IchorCore

final class NodeMenuTransportTests: XCTestCase {
    func testOnlyTheKubernetesRowsSkipTheTalosApi() {
        let kube = NodeHubEntry.allCases.filter { $0.transport == .kube }
        XCTAssertEqual(kube, [.kubePods, .maintenance, .drain, .cordon, .taintsLabels])
        XCTAssertEqual(NodeHubEntry.allCases.filter { $0.transport == .talos }.count, NodeHubEntry.allCases.count - 5)
        XCTAssertEqual(NodeHubEntry.power.transport, .talos)
    }

    func testTheOverviewSaysWhichNodeAnswers() {
        let overview = ClusterOverview(
            context: "lab",
            nodes: [
                NodeOverview(node: "10.0.0.4", hostname: "worker-0", reachable: true),
                NodeOverview(
                    node: "10.0.0.5", hostname: "worker-1", reachable: false, error: "unreachable: no route to host",
                    errorKind: "network"),
            ])
        XCTAssertEqual(overview.reachability(of: "10.0.0.4"), NodeReachability(reachable: true))
        XCTAssertEqual(
            overview.reachability(of: "10.0.0.5"),
            NodeReachability(reachable: false, errorKind: "network", error: "unreachable: no route to host"))
        XCTAssertNil(overview.reachability(of: "10.0.0.9"))
    }

    func testOnlyADialFailureMakesANodeUnreachable() {
        XCTAssertEqual(
            NodeReachability.from(failure: "unreachable: connection refused"),
            NodeReachability(reachable: false, errorKind: "network", error: "unreachable: connection refused"))
        XCTAssertNil(NodeReachability.from(failure: "TLS error: bad certificate"))
        XCTAssertNil(NodeReachability.from(failure: "permission denied (talosconfig role too limited): x"))
    }

    func testALiveFailureWinsOverAnOverviewThatSawTheNodeUp() {
        let live = NodeReachability.from(failure: "unreachable: no route to host")
        XCTAssertEqual(NodeReachability.merge(known: NodeReachability(reachable: true), live: live), live)
        XCTAssertEqual(NodeReachability.merge(known: nil, live: live), live)
        let known = NodeReachability(reachable: false, errorKind: "tls", lastSeen: 1_000)
        XCTAssertEqual(NodeReachability.merge(known: known, live: live), known)
        XCTAssertEqual(NodeReachability.merge(known: known, live: nil), known)
        XCTAssertNil(NodeReachability.merge(known: nil, live: nil))
    }

    func testUnknownIsNeverDown() {
        let unknown: NodeReachability? = nil
        XCTAssertFalse(unknown.isDown)
        XCTAssertFalse(Optional(NodeReachability(reachable: true)).isDown)
        XCTAssertTrue(Optional(NodeReachability(reachable: false)).isDown)
    }

    func testTheTailOfTheWindowWithoutACheckIsMeasured() {
        let hour = historyTailGapMillis
        let uptime = HistoryNodeUptime(
            node: "10.0.0.5", uptimePercent: 100, intervals: [HistoryInterval(state: "ready", from: 0, to: 10 * hour)])
        XCTAssertEqual(HistoryQueryResult(from: 0, to: 13 * hour).uncheckedTail(uptime), 3 * hour)
        XCTAssertNil(HistoryQueryResult(from: 0, to: 10 * hour + hour / 2).uncheckedTail(uptime))
        XCTAssertNil(HistoryQueryResult(from: 0, to: 13 * hour).uncheckedTail(HistoryNodeUptime(node: "10.0.0.6")))
    }
}
