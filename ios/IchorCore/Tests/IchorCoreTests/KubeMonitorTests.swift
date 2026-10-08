import XCTest
@testable import IchorCore

final class KubeMonitorTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private let overview = KubeNodesOverview(serverVersion: "v1.33.1", nodes: [
        KubeNodeInfo(name: "ip-10-0-1-10", roles: ["worker"], ready: true),
        KubeNodeInfo(name: "ip-10-0-1-11", roles: ["worker"], ready: false, pressure: ["MemoryPressure", "DiskPressure"]),
    ])

    func testNodesComeFromTheKubernetesList() {
        let s = kubeSnapshotOf(overview, context: "eks", certNotAfter: 0, takenAt: now)
        XCTAssertTrue(s.kube)
        XCTAssertEqual(s.context, "eks")
        XCTAssertEqual(s.nodes["ip-10-0-1-10"], NodeState(hostname: "ip-10-0-1-10", health: .ready))
        XCTAssertEqual(s.nodes["ip-10-0-1-11"], NodeState(hostname: "ip-10-0-1-11", health: .notReady, reason: "MemoryPressure; DiskPressure"))
        XCTAssertFalse(s.etcdChecked)
        XCTAssertFalse(s.unreachableAsAWhole)
    }

    func testExpiringKubeconfigCredentialsAreWordedForAKubeconfig() {
        let soon = Int64(now.timeIntervalSince1970) + 3 * 86_400
        let alerts = evaluate(previous: nil, current: kubeSnapshotOf(overview, context: "eks", certNotAfter: soon, takenAt: now), now: now).alerts
        XCTAssertEqual(alerts.map(\.key), ["cert"])
        XCTAssertEqual(alerts[0].title, "kubeconfig credentials")
        XCTAssertEqual(alerts[0].text, "The kubeconfig credentials expire in 3 days. Import a new kubeconfig.")
    }

    func testASnapshotSavedBeforeTheKubeFieldReadsAsTalos() throws {
        let saved = ClusterSnapshot(context: "lab", takenAt: now, nodes: [:])
        var json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(saved)) as! [String: Any]
        json.removeValue(forKey: "kube")
        let decoded = try JSONDecoder().decode(ClusterSnapshot.self, from: JSONSerialization.data(withJSONObject: json))
        XCTAssertFalse(decoded.kube)
    }
}
