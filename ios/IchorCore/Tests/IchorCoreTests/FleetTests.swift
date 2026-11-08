import XCTest
@testable import IchorCore

final class FleetTests: XCTestCase {
    private let lab = ContextSummary(name: "admin@lab", fingerprint: "fp-lab", clusterID: "c-lab")
    private let labReader = ContextSummary(name: "reader@lab", fingerprint: "fp-lab-reader", clusterID: "c-lab")
    private let dev = ContextSummary(name: "kind-dev", kind: ContextKind.kube, fingerprint: "fp-dev")
    private let demo = ContextSummary(name: "demo", fingerprint: "fp-demo", demo: true)

    private func snapshot() -> ClusterSnapshot {
        ClusterSnapshot(
            context: "admin@lab", takenAt: Date(timeIntervalSince1970: 1_800_000_000.5),
            nodes: [
                "10.0.0.1": NodeState(hostname: "node-a", health: .ready),
                "10.0.0.2": NodeState(hostname: "node-b", health: .unreachable),
            ],
            etcdChecked: true, amChecked: true, amIssues: ["f1": "warning|KubePodCrashLooping|shop/checkout"])
    }

    func testOneEntryPerClusterWithItsSnapshotAndMisses() {
        let entries = fleetEntries(
            snapshots: ["c-lab": snapshot()], reachability: ["c-lab": Reachability(misses: 2)],
            contexts: [lab, labReader, dev, demo], labels: ClusterLabels(names: ["fp-lab": "Home lab"]))

        XCTAssertEqual(entries.map(\.key), ["c-lab", "fp-dev"])
        XCTAssertEqual(entries[0].name, "Home lab")
        XCTAssertEqual(entries[0].context, "admin@lab")
        XCTAssertEqual(entries[0].takenAt, 1_800_000_000_500)
        XCTAssertEqual(entries[0].failures, 2)
        XCTAssertEqual(entries[0].nodes["10.0.0.2"], FleetNodeEntry(hostname: "node-b", health: "unreachable"))
        XCTAssertEqual(entries[0].amIssues["f1"], "warning|KubePodCrashLooping|shop/checkout")
        // Never checked yet: no time, a kubeconfig cluster.
        XCTAssertEqual(entries[1].takenAt, 0)
        XCTAssertTrue(entries[1].kube)
    }

    func testTagsComeFromEveryContextOfTheClusterEvenWhenMasked() {
        let entries = fleetEntries(
            snapshots: [:], reachability: [:], contexts: [lab, labReader, dev],
            labels: ClusterLabels(masked: true),
            tags: ["fp-lab": ["prod"], "fp-lab-reader": ["prod", "eu"], "gone": ["old"]])

        XCTAssertEqual(entries[0].tags, ["prod", "eu"])
        XCTAssertEqual(entries[1].tags, [])
    }

    func testScreenshotModeHidesTheGivenNamesAndHostnames() {
        let entries = fleetEntries(
            snapshots: ["c-lab": snapshot()], reachability: [:], contexts: [lab],
            labels: ClusterLabels(names: ["fp-lab": "Home lab"], masked: true))

        XCTAssertEqual(entries[0].name, "admin@lab")
        XCTAssertTrue(entries[0].nodes.values.allSatisfy { $0.hostname.isEmpty })
    }

    func testTheEntriesJSONCarriesUnixMillisAndRawHealth() throws {
        let entries = fleetEntries(
            snapshots: ["c-lab": snapshot()], reachability: [:], contexts: [lab], labels: ClusterLabels())
        let json = FleetEntries(clusters: entries).json
        let root = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(json.utf8)) as? [String: Any])
        let first = try XCTUnwrap((root["clusters"] as? [[String: Any]])?.first)
        XCTAssertEqual((first["takenAt"] as? NSNumber)?.int64Value, 1_800_000_000_500)
        let node = try XCTUnwrap((first["nodes"] as? [String: Any])?["10.0.0.2"] as? [String: Any])
        XCTAssertEqual(node["health"] as? String, "unreachable")
        XCTAssertEqual(first["pluginChecked"] as? Bool, false)
    }

    func testTheDemoAloneShowsTheDemoFleet() {
        XCTAssertTrue(demoFleetOnly([demo]))
        XCTAssertFalse(demoFleetOnly([demo, lab]))
        XCTAssertFalse(demoFleetOnly([]))
    }

    func testTheSummaryDecodes() throws {
        let json = #"""
            {"clusters":[{"key":"c-lab","name":"lab","context":"admin@lab","tone":"bad","checkedAt":1800000000000,
            "nodes":3,"ready":2,"unreachable":1,"critical":1,"tracksChecked":["etcd"],
            "topFinding":{"track":"nodes","severity":"critical","subject":"node-b unreachable"}},
            {"key":"x","name":"x","context":"x","tone":"purple"}],
            "counts":{"bad":1,"unknown":1},"worst":"bad","groups":[{"tag":"","counts":{"bad":1},"keys":["c-lab","x"]}]}
            """#
        let summary = try TalosJSON.decode(FleetSummary.self, from: json)

        XCTAssertEqual(summary.worst, .bad)
        XCTAssertEqual(summary.clusters[0].ready, 2)
        XCTAssertEqual(summary.clusters[0].topFinding?.subject, "node-b unreachable")
        XCTAssertEqual(summary.clusters[0].topFinding?.critical, true)
        XCTAssertNotNil(summary.clusters[0].checkedDate)
        // A tone a newer core names reads as unknown; an absent check has no date.
        XCTAssertEqual(summary.clusters[1].tone, .unknown)
        XCTAssertNil(summary.clusters[1].checkedDate)
        XCTAssertEqual(summary.counts.needAttention, 1)
        XCTAssertEqual(summary.groups.first?.keys, ["c-lab", "x"])
    }

    func testAMaskedRowShowsOnlyTheGenericSubjects() {
        let nodes = FleetFinding(track: "nodes", severity: "critical", subject: "2 of 3 nodes unreachable")
        let alert = FleetFinding(track: "alertmanager", subject: "KubePodCrashLooping · shop/checkout")

        XCTAssertTrue(nodes.subjectShown(masked: true))
        XCTAssertFalse(alert.subjectShown(masked: true))
        XCTAssertTrue(alert.subjectShown(masked: false))
        XCTAssertFalse(FleetFinding(track: "etcd").subjectShown(masked: false))
    }
}
