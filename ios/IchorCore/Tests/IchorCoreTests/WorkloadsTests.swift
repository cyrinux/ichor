import XCTest
@testable import IchorCore

final class WorkloadsTests: XCTestCase {
    private let workloads = [
        KubeWorkload(kind: "Deployment", namespace: "shop", name: "web", desired: 3, ready: 3, updated: 3, available: 3, state: "ready", images: ["nginx:1.27"]),
        KubeWorkload(kind: "StatefulSet", namespace: "shop", name: "db", desired: 1, state: "degraded", images: ["postgres:17"]),
        KubeWorkload(kind: "DaemonSet", namespace: "kube-system", name: "proxy", desired: 3, ready: 3, updated: 2, state: "progressing"),
        KubeWorkload(kind: "Deployment", namespace: "a", name: "frozen", desired: 1, ready: 1, state: "paused"),
    ]

    func testRefEncodesAsKubeWorkloadsNamedTakesIt() throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        let json = String(decoding: try encoder.encode(workloads[0].ref), as: UTF8.self)
        XCTAssertEqual(json, #"{"kind":"Deployment","name":"web","namespace":"shop"}"#)
    }

    func testDecodesTheGoJSON() throws {
        let json = #"{"workloads":[{"kind":"Deployment","namespace":"shop","name":"web","desired":3,"ready":2,"updated":3,"available":2,"state":"degraded","restartedAt":1700000000000,"created":1,"images":["nginx:1.27"]}]}"#
        let w = try XCTUnwrap(TalosJSON.decode(KubeWorkloadList.self, from: json).workloads.first)
        XCTAssertEqual(w.workloadState, .degraded)
        XCTAssertEqual(w.restartedAt, 1_700_000_000_000)
        XCTAssertEqual(w.id, "Deployment/shop/web")
        XCTAssertEqual(try TalosJSON.decode(KubeWorkloadList.self, from: "{}"), KubeWorkloadList())
    }

    func testDecodesTheRolloutStatus() throws {
        let json = #"{"workload":{"kind":"Deployment","namespace":"shop","name":"web","desired":2,"updated":1,"state":"progressing"},"failed":true,"# +
            #""pods":[{"name":"web-new-a","status":"Running","healthy":true,"ready":1,"containers":1,"updated":true},"# +
            #"{"name":"web-old-a","status":"Running","healthy":true,"restarts":2}]}"#
        let st = try TalosJSON.decode(KubeRolloutStatus.self, from: json)
        XCTAssertEqual(st.workload.workloadState, .progressing)
        XCTAssertFalse(st.done)
        XCTAssertTrue(st.failed)
        XCTAssertFalse(st.manual)
        XCTAssertEqual(st.pods.map(\.updated), [true, false])
        XCTAssertEqual(st.pods[1].restarts, 2)
        XCTAssertEqual(st.newReady, 1)
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
