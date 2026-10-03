import XCTest
@testable import IchorCore

final class WorkloadsTests: XCTestCase {
    private let workloads = [
        KubeWorkload(kind: "Deployment", namespace: "shop", name: "web", desired: 3, ready: 3, updated: 3, available: 3, state: "ready", images: ["nginx:1.27"]),
        KubeWorkload(kind: "StatefulSet", namespace: "shop", name: "db", desired: 1, state: "degraded", images: ["postgres:17"]),
        KubeWorkload(kind: "DaemonSet", namespace: "kube-system", name: "proxy", desired: 3, ready: 3, updated: 2, state: "progressing"),
        KubeWorkload(kind: "Deployment", namespace: "a", name: "frozen", desired: 1, ready: 1, state: "paused"),
    ]

    func testOwnersOfAnAppsPodsThroughReplicaSetsAndDirectOwners() {
        let kubePods = [
            KubePod(namespace: "shop", name: "web-5d8f-abcde", owner: "ReplicaSet/web-5d8f"),
            KubePod(namespace: "shop", name: "web-5d8f-fghij", owner: "ReplicaSet/web-5d8f"),
            KubePod(namespace: "shop", name: "db-0", owner: "StatefulSet/db"),
            KubePod(namespace: "kube-system", name: "proxy-xyz", owner: "DaemonSet/proxy"),
            KubePod(namespace: "shop", name: "migrate-1-q", owner: "Job/migrate-1"),
            KubePod(namespace: "kube-system", name: "apiserver-cp1", owner: "Node/cp1"),
        ]
        let app = [
            InventoryPod(namespace: "shop", pod: "web-5d8f-abcde", node: "n1"),
            InventoryPod(namespace: "shop", pod: "web-5d8f-fghij", node: "n2"),
            InventoryPod(namespace: "shop", pod: "db-0", node: "n1"),
            InventoryPod(namespace: "shop", pod: "migrate-1-q", node: "n1"),
        ]
        XCTAssertEqual(workloadOwners(workloads, pods: app, kubePods: kubePods).map(\.name), ["web", "db"])
        let proxy = [InventoryPod(namespace: "kube-system", pod: "proxy-xyz", node: "n1")]
        XCTAssertEqual(workloadOwners(workloads, pods: proxy, kubePods: kubePods).map(\.name), ["proxy"])
    }

    func testOwnersIgnoreUnknownPodsOtherNamespacesAndOddOwners() {
        let kubePods = [
            KubePod(namespace: "other", name: "web-5d8f-abcde", owner: "ReplicaSet/web-5d8f"),
            KubePod(namespace: "shop", name: "bare"),
            KubePod(namespace: "shop", name: "odd", owner: "ReplicaSet/nohash"),
        ]
        let app = ["bare", "odd", "gone"].map { InventoryPod(namespace: "shop", pod: $0, node: "n1") } +
            [InventoryPod(namespace: "other", pod: "web-5d8f-abcde", node: "n1")]
        XCTAssertTrue(workloadOwners(workloads, pods: app, kubePods: kubePods).isEmpty)
    }

    func testDecodesTheGoJSON() throws {
        let json = #"{"workloads":[{"kind":"Deployment","namespace":"shop","name":"web","desired":3,"ready":2,"updated":3,"available":2,"state":"degraded","restartedAt":1700000000000,"created":1,"images":["nginx:1.27"]}]}"#
        let w = try XCTUnwrap(TalosJSON.decode(KubeWorkloadList.self, from: json).workloads.first)
        XCTAssertEqual(w.workloadState, .degraded)
        XCTAssertEqual(w.restartedAt, 1_700_000_000_000)
        XCTAssertEqual(w.id, "Deployment/shop/web")
        XCTAssertEqual(try TalosJSON.decode(KubeWorkloadList.self, from: "{}"), KubeWorkloadList())
    }

    func testAttentionFirstThenNamespaceAndName() {
        XCTAssertEqual(filterWorkloads(workloads, namespace: nil, query: "").map(\.name), ["db", "proxy", "frozen", "web"])
    }

    func testFiltersByNamespaceAndQuery() {
        XCTAssertEqual(filterWorkloads(workloads, namespace: "shop", query: "").map(\.name), ["db", "web"])
        XCTAssertEqual(filterWorkloads(workloads, namespace: nil, query: " POSTGRES ").map(\.name), ["db"])
        XCTAssertEqual(filterWorkloads(workloads, namespace: nil, query: "daemonset").map(\.name), ["proxy"])
        XCTAssertTrue(filterWorkloads(workloads, namespace: "kube-system", query: "web").isEmpty)
    }

    func testNamespacesRestartabilityAndRole() {
        XCTAssertEqual(workloadNamespaces(workloads), ["a", "kube-system", "shop"])
        XCTAssertFalse(workloads[3].canRestart)
        XCTAssertTrue(workloads[0].canRestart)
        XCTAssertEqual(KubeWorkload(kind: "x", namespace: "y", name: "z", state: "weird").workloadState, .unknown)
        XCTAssertEqual(Feature.workloads.roles, ["os:admin"])
        XCTAssertFalse(ContextSummary(name: "x", roles: ["os:operator"]).allows(.workloads))
    }
}
