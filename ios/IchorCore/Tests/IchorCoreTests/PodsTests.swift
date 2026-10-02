import XCTest
@testable import IchorCore

final class PodsTests: XCTestCase {
    private let pods = [
        KubePod(namespace: "shop", name: "web-1", status: "Running", healthy: true, ready: 1, containers: 1, node: "w1", owner: "ReplicaSet/web-5d8f", images: ["nginx:1.27"]),
        KubePod(namespace: "shop", name: "worker-1", status: "CrashLoopBackOff", containers: 1, restarts: 14, node: "w2"),
        KubePod(namespace: "kube-system", name: "coredns-1", status: "Running", healthy: true, ready: 1, containers: 1, node: "cp1"),
        KubePod(namespace: "a", name: "job-1", status: "Pending"),
    ]

    func testDecodesTheGoJSON() throws {
        let json = #"{"pods":[{"namespace":"shop","name":"web-1","status":"Init:1/2","healthy":false,"ready":0,"containers":2,"restarts":3,"node":"w1","owner":"ReplicaSet/web","created":1,"images":["nginx"]}]}"#
        let p = try XCTUnwrap(TalosJSON.decode(KubePodList.self, from: json).pods.first)
        XCTAssertEqual(p.restarts, 3)
        XCTAssertEqual(p.id, "shop/web-1")
        XCTAssertTrue(p.transitional)
        XCTAssertEqual(try TalosJSON.decode(KubePodList.self, from: "{}"), KubePodList())
    }

    func testUnhealthyFirstThenNamespaceAndName() {
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "").map(\.name), ["job-1", "worker-1", "coredns-1", "web-1"])
    }

    func testFiltersByNamespaceStatusNodeOwnerOrImage() {
        XCTAssertEqual(filterPods(pods, namespace: "shop", query: "").map(\.name), ["worker-1", "web-1"])
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "crashloop").map(\.name), ["worker-1"])
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "CP1").map(\.name), ["coredns-1"])
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "replicaset").map(\.name), ["web-1"])
        XCTAssertEqual(filterPods(pods, namespace: nil, query: "nginx").map(\.name), ["web-1"])
        XCTAssertEqual(podNamespaces(pods), ["a", "kube-system", "shop"])
    }

    func testTransitionalStates() {
        XCTAssertTrue(KubePod(namespace: "a", name: "b", status: "Terminating").transitional)
        XCTAssertFalse(KubePod(namespace: "a", name: "b", status: "Init:Error").transitional)
        XCTAssertFalse(KubePod(namespace: "a", name: "b", status: "CrashLoopBackOff").transitional)
    }
}
