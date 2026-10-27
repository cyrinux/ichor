import XCTest
@testable import IchorCore

final class KubeClusterSummaryTests: XCTestCase {
    private let gib = Double(1 << 30)

    private var nodes: KubeNodesOverview {
        KubeNodesOverview(nodes: [
            KubeNodeInfo(name: "worker-1", ready: true, cpu: 4, memory: 16 * gib),
            KubeNodeInfo(name: "worker-2", ready: true, cpu: 8, memory: 8 * gib),
            KubeNodeInfo(name: "worker-3", ready: false, cpu: 4, memory: 8 * gib),
        ])
    }

    func testCountsTheNodesAndTheirCapacity() {
        let summary = nodes.clusterSummary()
        XCTAssertEqual(summary.total, 3)
        XCTAssertEqual(summary.ready, 2)
        XCTAssertEqual(summary.notReady, 1)
        XCTAssertEqual(summary.status, .degraded)
        XCTAssertEqual(summary.cpuCount, 16)
        XCTAssertEqual(summary.memTotal, Int64(32 * gib))
        // No metrics-server: no share in use, never a misleading 0 %.
        XCTAssertNil(summary.cpuFraction)
        XCTAssertNil(summary.memFraction)
    }

    func testTheShareInUseIsOfTheAllocatableCapacity() throws {
        let top = try TalosJSON.decode(KubeTopNodes.self, from: """
        {"available":true,"nodes":[
          {"name":"worker-1","cpu":1,"cpuAllocatable":4,"memory":\(4 * gib),"memoryAllocatable":\(15 * gib)},
          {"name":"worker-2","cpu":3,"cpuAllocatable":8,"memory":\(2 * gib),"memoryAllocatable":\(7.5 * gib)}
        ]}
        """)
        let summary = nodes.clusterSummary(usage: top.nodes)
        XCTAssertEqual(try XCTUnwrap(summary.cpuFraction), 4.0 / 12.0, accuracy: 1e-9)
        XCTAssertEqual(try XCTUnwrap(summary.memFraction), 6.0 / 22.5, accuracy: 1e-9)
    }

    func testEveryNodeReadyIsHealthyAndNoneIsDown() {
        let ready = KubeNodesOverview(nodes: nodes.nodes.map { KubeNodeInfo(name: $0.name, ready: true) })
        XCTAssertEqual(ready.clusterSummary().status, .healthy)
        let down = KubeNodesOverview(nodes: nodes.nodes.map { KubeNodeInfo(name: $0.name, ready: false) })
        XCTAssertEqual(down.clusterSummary().status, .down)
    }
}
