import XCTest
@testable import IchorCore

final class ContainersTests: XCTestCase {
    func testDecodeGoJSON() throws {
        let json = #"{"at":1800000000000,"containers":[{"id":"abc","podNamespace":"kube-system","pod":"coredns-1","name":"coredns","image":"registry.k8s.io/coredns:v1","status":"CONTAINER_RUNNING","pid":42,"memory":1048576,"cpuNanos":5000000000}]}"#
        let sample = try TalosJSON.decode(ContainerSample.self, from: json)
        XCTAssertEqual(sample.containers.first?.pod, "coredns-1")
        XCTAssertEqual(sample.containers.first?.cpuNanos, 5_000_000_000)
        XCTAssertEqual(sample.containers.first?.isRunning, true)
        XCTAssertEqual(sample.containers.first?.displayStatus, "running")
    }

    func testNamespaceDefaultsToKubernetes() throws {
        let json = #"{"at":1,"containers":[{"id":"apid","namespace":"system","name":"apid","status":"RUNNING"},{"id":"c1","pod":"web","name":"web"}]}"#
        let sample = try TalosJSON.decode(ContainerSample.self, from: json)
        XCTAssertEqual(sample.containers.map(\.namespace), [ContainerNamespace.system, ContainerNamespace.kubernetes])
        XCTAssertEqual(sample.containers.map(\.isSystem), [true, false])
        XCTAssertEqual(NodeContainer(id: "0123456789abcdef", pod: "p", name: "").displayName, "0123456789ab")
    }

    func testSystemContainersGroupFirst() {
        let rows = [
            ContainerRow(container: NodeContainer(id: "w", pod: "web", name: "web", memory: 500), cpuPercent: 50),
            ContainerRow(container: NodeContainer(id: "apid", namespace: ContainerNamespace.system, podNamespace: "", pod: "", name: "apid", memory: 1), cpuPercent: 1),
            ContainerRow(container: NodeContainer(id: "trustd", namespace: ContainerNamespace.system, podNamespace: "", pod: "", name: "trustd", memory: 1), cpuPercent: 1),
        ]
        let pods = sortPods(groupPods(rows), by: .cpu)
        XCTAssertEqual(pods.map(\.id), ["system", "default/web"])
        XCTAssertEqual(pods.map(\.isSystem), [true, false])
        XCTAssertEqual(pods[0].containers.map(\.id), ["apid", "trustd"])
        XCTAssertEqual(filterPods(pods, query: "trust").first?.isSystem, true)
    }

    func testNullContainersDecodeAsEmpty() throws {
        XCTAssertEqual(try TalosJSON.decode(ContainerSample.self, from: #"{"at":1,"containers":null}"#).containers, [])
    }

    func testStatus() {
        XCTAssertFalse(NodeContainer(id: "a", pod: "p", name: "c", status: "CONTAINER_EXITED").isRunning)
        XCTAssertEqual(NodeContainer(id: "a", pod: "p", name: "c", status: "CONTAINER_EXITED").displayStatus, "exited")
        XCTAssertTrue(NodeContainer(id: "a", pod: "p", name: "c", status: "RUNNING").isRunning)
    }

    func testCPUPercentFromNanosecondDeltas() {
        let before = ContainerSample(at: 0, containers: [
            NodeContainer(id: "a", pod: "p", name: "a", cpuNanos: 1_000_000_000),
            NodeContainer(id: "b", pod: "p", name: "b", cpuNanos: 5_000_000_000),
        ])
        let after = ContainerSample(at: 2_000, containers: [
            NodeContainer(id: "a", pod: "p", name: "a", cpuNanos: 2_000_000_000), // 1 s over 2 s
            NodeContainer(id: "b", pod: "p", name: "b", cpuNanos: 1_000_000_000), // restarted
            NodeContainer(id: "c", pod: "p", name: "c", cpuNanos: 9_000_000_000), // new id
        ])
        let rows = containerRows(previous: before, current: after)
        XCTAssertEqual(rows[0].cpuPercent ?? -1, 50, accuracy: 0.001)
        XCTAssertEqual(rows[1].cpuPercent, 0)
        XCTAssertNil(rows[2].cpuPercent)
        XCTAssertNil(containerRows(previous: nil, current: after)[0].cpuPercent)
        XCTAssertNil(containerRows(previous: after, current: after)[0].cpuPercent)
    }

    func testGroupSortAndFilter() {
        let rows = [
            ContainerRow(container: NodeContainer(id: "1", podNamespace: "kube-system", pod: "coredns", name: "coredns", image: "coredns:1", memory: 10), cpuPercent: 5),
            ContainerRow(container: NodeContainer(id: "2", podNamespace: "web", pod: "shop", name: "app", image: "shop:2", memory: 100), cpuPercent: 1),
            ContainerRow(container: NodeContainer(id: "3", podNamespace: "web", pod: "shop", name: "sidecar", image: "envoy:1", status: "CONTAINER_EXITED", memory: 50), cpuPercent: nil),
            ContainerRow(container: NodeContainer(id: "4", podNamespace: "web", pod: "new", name: "x", memory: 1), cpuPercent: nil),
        ]
        let pods = groupPods(rows)
        XCTAssertEqual(pods.map(\.id), ["kube-system/coredns", "web/shop", "web/new"])
        XCTAssertEqual(pods[1].memory, 150)
        XCTAssertEqual(pods[1].cpuPercent ?? -1, 1, accuracy: 0.001)
        XCTAssertFalse(pods[1].allRunning)
        XCTAssertNil(pods[2].cpuPercent)

        XCTAssertEqual(sortPods(pods, by: .cpu).map(\.id), ["kube-system/coredns", "web/shop", "web/new"])
        XCTAssertEqual(sortPods(pods, by: .memory).map(\.id), ["web/shop", "kube-system/coredns", "web/new"])

        XCTAssertEqual(filterPods(pods, query: "WEB").map(\.id), ["web/shop", "web/new"])
        let envoy = filterPods(pods, query: "envoy")
        XCTAssertEqual(envoy.map(\.id), ["web/shop"])
        XCTAssertEqual(envoy[0].containers.map(\.id), ["3"])
        XCTAssertEqual(filterPods(pods, query: " ").count, 3)
    }
}
