import XCTest
@testable import IchorCore

final class RecentTargetsTests: XCTestCase {
    private let cluster = "aaaaaaaaaaaaaaaa"

    private func node(_ n: Int, tab: String = "", at: Int64 = 0) -> RecentTarget {
        var target = ShareTarget.node(address: "10.0.0.\(n)", hostname: "node-\(n)", tab: tab)
        target.cluster = cluster
        return RecentTarget(target: target, label: "node-\(n)", at: at)
    }

    func testNewestFirstWithoutDuplicates() {
        let list = RecentTargetList().adding(node(1, at: 1)).adding(node(2, at: 2)).adding(node(1, tab: "live", at: 3))
        XCTAssertEqual(list.targets.map(\.label), ["node-1", "node-2"])
        XCTAssertEqual(list.targets.first?.at, 3)
    }

    func testKeepsTwenty() {
        let list = (1...25).reduce(RecentTargetList()) { $0.adding(node($1, at: Int64($1))) }
        XCTAssertEqual(list.targets.count, RecentTargetList.limit)
        XCTAssertEqual(list.targets.first?.label, "node-25")
        XCTAssertEqual(list.targets.last?.label, "node-6")
    }

    func testRecordsOnlyObjectsOfAKnownCluster() {
        var etcd = ShareTarget.screen(.etcd)
        etcd.cluster = cluster
        let skipped = [
            RecentTarget(target: etcd, label: "etcd"),
            RecentTarget(target: .argoApp(namespace: "argocd", name: "web"), label: "web"),
        ]
        for recent in skipped { XCTAssertTrue(RecentTargetList().adding(recent).targets.isEmpty) }
    }

    func testRemovingAndRemovedClusters() {
        var flux = ShareTarget.fluxApp(kind: "Kustomization", namespace: "flux-system", name: "apps")
        flux.cluster = "bbbbbbbbbbbbbbbb"
        let list = RecentTargetList([node(1), RecentTarget(target: flux, label: "apps")])
        var other = node(1).target
        other.tab = "live"
        XCTAssertEqual(list.removing(other).targets.map(\.label), ["apps"])
        XCTAssertEqual(list.known(clusterIDs: [cluster]).map(\.label), ["node-1"])
    }

    func testRoundTripsAndReadsAndroidJSON() {
        let list = RecentTargetList([node(1, at: 5)])
        XCTAssertEqual(RecentTargetList.parse(list.encoded), list)
        let android = #"[{"target":{"cluster":"aaaaaaaaaaaaaaaa","target":"workload","kind":"Deployment","ns":"default","name":"api"},"label":"api","detail":"default","at":4}]"#
        XCTAssertEqual(RecentTargetList.parse(android).targets.first?.target, {
            var t = ShareTarget.workload(kind: "Deployment", namespace: "default", name: "api"); t.cluster = cluster; return t
        }())
        XCTAssertTrue(RecentTargetList.parse("not json").targets.isEmpty)
    }

    func testAFocusedKubernetesScreenNamesItsObject() {
        XCTAssertEqual(KubeFocus(tab: .workloads, id: "Deployment/default/api", namespace: "default", name: "api").shareTarget,
                       .workload(kind: "Deployment", namespace: "default", name: "api"))
        XCTAssertEqual(KubeFocus(tab: .pods, id: "default/api-0", namespace: "default", name: "api-0").shareTarget,
                       .pod(namespace: "default", name: "api-0"))
        XCTAssertNil(KubeFocus(tab: .workloads).shareTarget)
    }
}
