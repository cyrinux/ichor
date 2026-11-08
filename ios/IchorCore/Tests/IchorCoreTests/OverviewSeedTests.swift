import Foundation
import XCTest
@testable import IchorCore

final class OverviewSeedTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 100_000)

    private var talos: ClusterSnapshot {
        ClusterSnapshot(
            context: "lab", takenAt: now.addingTimeInterval(-60),
            nodes: [
                "192.0.2.12": NodeState(hostname: "worker-2", health: .unreachable, reason: "connection refused"),
                "192.0.2.10": NodeState(hostname: "cp-1", health: .ready),
                "192.0.2.11": NodeState(hostname: "worker-1", health: .notReady, reason: "Ready: KubeletNotReady"),
            ])
    }

    func testOverviewSeedHasEachNodeAndItsHealth() {
        let seed = talos.overviewSeed()
        XCTAssertEqual(seed.context, "lab")
        XCTAssertEqual(seed.nodes.map(\.hostname), ["cp-1", "worker-1", "worker-2"])
        XCTAssertEqual(seed.nodes.map(\.health), [.ready, .notReady, .unreachable])
        XCTAssertNil(seed.nodes[1].error)
        XCTAssertEqual(seed.nodes[2].error, "connection refused")
        XCTAssertEqual(seed.nodes[2].node, "192.0.2.12")
        // What the check does not keep stays unknown.
        XCTAssertEqual(seed.nodes[0].version, "")
        XCTAssertEqual(seed.nodes[0].cpuCount, 0)
        XCTAssertTrue(ClusterSnapshot(context: "lab", takenAt: now, nodes: [:]).overviewSeed().nodes.isEmpty)
    }

    func testKubeSeedHasEachNodeAndItsPressure() {
        let snapshot = ClusterSnapshot(
            context: "cloud", takenAt: now,
            nodes: [
                "node-b": NodeState(hostname: "node-b", health: .notReady, reason: "MemoryPressure; DiskPressure"),
                "node-a": NodeState(hostname: "node-a", health: .ready),
            ], kube: true)
        let seed = snapshot.kubeNodesSeed()
        XCTAssertEqual(seed.nodes.map(\.name), ["node-a", "node-b"])
        XCTAssertTrue(seed.nodes[0].ready)
        XCTAssertEqual(seed.nodes[0].pressure, [])
        XCTAssertEqual(seed.nodes[1].pressure, ["MemoryPressure", "DiskPressure"])
    }

    func testOnlyARecentCheckOfTheRightKindOutsideScreenshotModeSeeds() {
        XCTAssertTrue(talos.seedable(kube: false, masked: false, now: now))
        XCTAssertFalse(talos.seedable(kube: false, masked: true, now: now))
        XCTAssertFalse(talos.seedable(kube: true, masked: false, now: now))
        XCTAssertFalse(talos.seedable(kube: false, masked: false, now: now.addingTimeInterval(monitorSeedMaxAge + 60)))
        XCTAssertFalse(
            ClusterSnapshot(context: "lab", takenAt: now, nodes: [:]).seedable(kube: false, masked: false, now: now))
    }
}
