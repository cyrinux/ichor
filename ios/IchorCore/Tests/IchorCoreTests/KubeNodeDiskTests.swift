import XCTest
@testable import IchorCore

final class KubeNodeDiskTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    /// The Go core's answer: one node near full, one fine, one under DiskPressure at a low fill.
    private let wire = #"""
    {"nodes":[
      {"name":"worker-a","usedPercent":91,"availableBytes":9663676416,"capacityBytes":107374182400,"imageFsUsedPercent":40,"diskPressure":false},
      {"name":"worker-b","usedPercent":42,"availableBytes":62277025792,"capacityBytes":107374182400},
      {"name":"worker-c","usedPercent":60,"availableBytes":42949672960,"capacityBytes":107374182400,"diskPressure":true}],
     "forbidden":false,"measured":3}
    """#

    private func decoded() throws -> KubeNodeDisk { try TalosJSON.decode(KubeNodeDisk.self, from: wire) }

    private func disk(_ percent: Int, pressure: Bool = false, forbidden: Bool = false, skipped: Bool = false) -> KubeNodeDisk {
        KubeNodeDisk(nodes: [KubeNodeDiskNode(name: "worker-a", usedPercent: percent, availableBytes: 2 << 30,
                                              capacityBytes: 100 << 30, diskPressure: pressure)],
                     forbidden: forbidden, measured: 1, skipped: skipped)
    }

    private func issues(_ disk: KubeNodeDisk, warn: Int = 85, critical: Int = 95, listed: Set<String> = [],
                        known: [String: String] = [:], demo: Bool = false) -> [String: String]? {
        storageIssues(of: disk, warn: warn, critical: critical, listed: listed, known: known, demo: demo)
    }

    private func severity(_ out: [String: String]?, _ node: String = "worker-a") -> String? {
        out?[kubeNodeDiskKey(node)].map { StorageIssue(value: $0).severity }
    }

    func testDecodesTheGoShapeLeniently() throws {
        let disk = try decoded()
        XCTAssertEqual(disk.nodes.count, 3)
        XCTAssertEqual(disk.measured, 3)
        XCTAssertFalse(disk.skipped)
        XCTAssertEqual(disk.nodes[0].imageFsUsedPercent, 40)
        XCTAssertEqual(disk.nodes[1].imageFsUsedPercent, 0)
        XCTAssertFalse(disk.nodes[1].diskPressure)
        XCTAssertTrue(disk.nodes[2].diskPressure)
        XCTAssertTrue(disk.nodes[0].measured)
        let empty = try TalosJSON.decode(KubeNodeDisk.self, from: "{}")
        XCTAssertEqual(empty, KubeNodeDisk())
        XCTAssertFalse(try TalosJSON.decode(KubeNodeDiskNode.self, from: #"{"name":"x"}"#).measured)
    }

    func testWarningAtTheWarnThreshold() {
        XCTAssertEqual(issues(disk(84)), [:])
        let issue = StorageIssue(value: issues(disk(85))?[kubeNodeDiskKey("worker-a")] ?? "")
        XCTAssertEqual(issue, StorageIssue(severity: dataWarning, kind: StorageIssue.fill, hostname: "worker-a", name: "nodefs",
                                           percent: "85", freeBytes: 2 << 30, sizeBytes: 100 << 30))
    }

    func testCriticalPastTheCriticalThreshold() {
        XCTAssertEqual(severity(issues(disk(94))), dataWarning)
        XCTAssertEqual(severity(issues(disk(95))), dataCritical)
        XCTAssertEqual(severity(issues(disk(90), warn: 80, critical: 90)), dataCritical)
        // Out-of-range thresholds are clamped, as on Android.
        XCTAssertEqual(severity(issues(disk(50), warn: 10, critical: 20)), dataWarning)
        XCTAssertEqual(severity(issues(disk(51), warn: 10, critical: 20)), dataCritical)
    }

    func testDiskPressureIsCriticalUnderTheThresholds() throws {
        let out = issues(try decoded())
        XCTAssertEqual(Set(out.map { Array($0.keys) } ?? []), ["worker-a|nodefs", "worker-c|nodefs"])
        let pressure = StorageIssue(value: out?["worker-c|nodefs"] ?? "")
        XCTAssertEqual(pressure.severity, dataCritical)
        XCTAssertEqual(pressure.percent, "60")
        XCTAssertEqual(pressure.detail, "DiskPressure")
        XCTAssertEqual(severity(out), dataWarning)
        XCTAssertEqual(severity(issues(disk(10, pressure: true))), dataCritical)
        // Pressure reported by a node whose kubelet gave no size still counts.
        let unsized = KubeNodeDisk(nodes: [KubeNodeDiskNode(name: "worker-a", usedPercent: 0, diskPressure: true)], measured: 0)
        XCTAssertEqual(severity(issues(unsized)), dataCritical)
    }

    func testUnreadableReadsAreAbsentNotClear() {
        XCTAssertNil(issues(disk(99, pressure: true, skipped: true)))
        XCTAssertNil(issues(KubeNodeDisk(forbidden: true, measured: 0)))
        XCTAssertNil(issues(KubeNodeDisk(measured: 0)))
        // Unsized nodes without pressure are not judged: nothing measured.
        XCTAssertNil(issues(KubeNodeDisk(nodes: [KubeNodeDiskNode(name: "worker-a", usedPercent: 99)], measured: 0)))
        // Refused on some kubelets only: the measured ones still count.
        XCTAssertEqual(severity(issues(disk(99, forbidden: true))), dataCritical)
        XCTAssertEqual(historyVolumes(disk(99, skipped: true)), [])
    }

    func testANodeListedButSilentKeepsItsIssuesAndAGoneNodeDropsThem() {
        let kept = StorageIssue(severity: dataCritical, kind: StorageIssue.fill, hostname: "worker-b", name: "nodefs", percent: "97").value
        let gone = StorageIssue(severity: dataWarning, kind: StorageIssue.fill, hostname: "worker-z", name: "nodefs", percent: "88").value
        let known = ["worker-a|nodefs": kept, "worker-b|nodefs": kept, "worker-z|nodefs": gone]
        let out = issues(disk(50), listed: ["worker-a", "worker-b"], known: known)
        // worker-a answered (now fine): cleared; worker-b listed, silent: kept; worker-z no longer listed: dropped.
        XCTAssertEqual(out, ["worker-b|nodefs": kept])
    }

    func testTheDemoNeverOpensAnIssue() {
        XCTAssertEqual(issues(disk(99, pressure: true), demo: true), [:])
        XCTAssertEqual(historyVolumes(disk(99)).map(\.usedPercent), [99])
    }

    func testKeysAreStableAndMatchTheTalosFormat() throws {
        let first = issues(try decoded())
        XCTAssertEqual(first, issues(try decoded()))
        XCTAssertEqual(kubeNodeDiskKey("worker-a"), "worker-a|nodefs")
        XCTAssertEqual(storageIssueNode("worker-a|nodefs"), "worker-a")
        // The same value the Talos path stores for a volume "nodefs" of host worker-a at that fill.
        let talos = storageIssuesOf(ClusterStorageHealth(nodes: [StorageNode(node: "worker-a", hostname: "worker-a", volumes: [
            StorageVolume(key: "worker-a|nodefs", name: "nodefs", usedPercent: 91, freeBytes: 9_663_676_416, sizeBytes: 107_374_182_400),
        ])]), thresholds: StorageThresholds())
        XCTAssertEqual(first?["worker-a|nodefs"], talos["worker-a|nodefs"])
        let history = historyVolumes(try decoded())
        XCTAssertEqual(history.map(\.key), ["worker-a|nodefs", "worker-b|nodefs", "worker-c|nodefs"])
        XCTAssertEqual(history.first, HistoryVolumeRecord(key: "worker-a|nodefs", name: "nodefs", node: "worker-a", usedPercent: 91))
    }

    private func kubeSnap(_ storage: [String: String]?) throws -> ClusterSnapshot {
        let nodes = try TalosJSON.decode(KubeNodesOverview.self, from: #"{"nodes":[{"name":"worker-a","ready":true}]}"#)
        return kubeSnapshotOf(nodes, context: "lab", certNotAfter: farCert, takenAt: now,
                              storageWatched: true, storageIssues: storage)
    }

    func testKubeSnapshotAlertsAndClears() throws {
        let base = evaluate(previous: nil, current: try kubeSnap(issues(disk(50))), now: now).next
        XCTAssertTrue(base.storageChecked)
        // A warning alerts when seen twice in a row, like the Talos path.
        let once = evaluate(previous: base, current: try kubeSnap(issues(disk(91))), now: now)
        XCTAssertEqual(once.alerts, [])
        let full = evaluate(previous: once.next, current: try kubeSnap(issues(disk(91))), now: now)
        XCTAssertEqual(full.alerts.map(\.key), ["storage:worker-a|nodefs"])
        XCTAssertEqual(full.alerts.first?.title, "nodefs on worker-a at 91 %")
        // DiskPressure alerts at once.
        let pressure = evaluate(previous: base, current: try kubeSnap(issues(disk(40, pressure: true))), now: now)
        XCTAssertEqual(pressure.alerts.map(\.title), ["nodefs on worker-a at 40 %, almost full"])
        let back = evaluate(previous: full.next, current: try kubeSnap(issues(disk(50))), now: now)
        XCTAssertEqual(back.alerts.map(\.title), ["nodefs on worker-a back under 85 %"])
        XCTAssertEqual(back.alerts.first?.problem, false)
        // Unread (refused, skipped, timed out): the open issue stands, no false OK.
        let unread = try kubeSnap(issues(KubeNodeDisk(forbidden: true)))
        XCTAssertFalse(unread.storageChecked)
        let still = evaluate(previous: full.next, current: unread, now: now)
        XCTAssertEqual(still.alerts, [])
        XCTAssertEqual(still.next.storageIssues.keys.sorted(), ["worker-a|nodefs"])
    }

    func testAKubeDiskAlertOpensTheNode() throws {
        let snapshot = try kubeSnap([:])
        let node = ShareTarget.node(address: "worker-a", hostname: "worker-a", tab: "")
        XCTAssertEqual(ShareTarget.forAlert(key: "storage:worker-a|nodefs", snapshot: snapshot), node)
        XCTAssertEqual(ShareTarget.forAlert(key: "storage:worker-a|nodefs:trend", snapshot: snapshot), node)
        // A Talos volume of the same name still opens the Storage screen.
        var talos = snapshot
        talos.kube = false
        XCTAssertEqual(ShareTarget.forAlert(key: "storage:worker-a|nodefs", snapshot: talos)?.target, .storage)
    }
}
