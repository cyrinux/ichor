import XCTest
@testable import IchorCore

final class ClusterStatusSummaryTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func snap(_ health: [NodeHealth], alarms: [String] = [], etcdChecked: Bool = true,
                      age: TimeInterval = 240) -> ClusterSnapshot {
        var nodes: [String: NodeState] = [:]
        for (i, h) in health.enumerated() { nodes["10.0.0.\(i)"] = NodeState(hostname: "n\(i)", health: h) }
        return ClusterSnapshot(context: "demo-lab", takenAt: now.addingTimeInterval(-age), nodes: nodes,
                               etcdAlarms: alarms, etcdChecked: etcdChecked)
    }

    func testAllReadyWithHealthyEtcd() {
        let status = clusterStatusSummary(snap(Array(repeating: .ready, count: 8)), now: now)
        XCTAssertEqual(status?.parts, [.nodes(8), .allReady, .etcdHealthy])
        XCTAssertEqual(status?.checkedAt, now.addingTimeInterval(-240))
        XCTAssertEqual(status?.healthy, true)
        XCTAssertEqual(status?.stale, false)
    }

    func testNodeProblemsAndEtcdAlarms() {
        let status = clusterStatusSummary(snap([.ready, .notReady, .unreachable], alarms: ["1:NOSPACE"]), now: now)
        XCTAssertEqual(status?.parts, [.nodes(3), .notReady(1), .unreachable(1), .etcdAlarms(1)])
        XCTAssertEqual(status?.healthy, false)
    }

    func testEtcdLeftOutWhenNotChecked() {
        XCTAssertEqual(clusterStatusSummary(snap([.ready], etcdChecked: false), now: now)?.parts, [.nodes(1), .allReady])
    }

    func testAnOldSnapshotIsStale() {
        let status = clusterStatusSummary(snap([.ready], age: 3 * 3600), now: now)
        XCTAssertEqual(status?.parts.last, .stale)
        XCTAssertEqual(status?.stale, true)
        XCTAssertEqual(status?.healthy, true, "stale is not a problem of the cluster")
    }

    func testNoSnapshotNoStatus() {
        XCTAssertNil(clusterStatusSummary(nil, now: now))
    }

    func testIntentClusterFallsBackToTheActiveOne() {
        let clusters = [WidgetCluster(id: "a", name: "Lab"), WidgetCluster(id: "b", name: "Shop")]
        XCTAssertEqual(intentCluster("b", clusters: clusters, active: "a")?.name, "Shop")
        XCTAssertEqual(intentCluster(nil, clusters: clusters, active: "a")?.name, "Lab")
        XCTAssertEqual(intentCluster("gone", clusters: clusters, active: "a")?.name, "Lab")
        XCTAssertNil(intentCluster(nil, clusters: clusters, active: nil))
    }
}
