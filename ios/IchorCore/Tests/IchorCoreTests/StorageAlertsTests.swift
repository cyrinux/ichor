import XCTest
@testable import IchorCore

final class StorageAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    /// The Go demo's answer, trimmed (storagehealth.go).
    private let demo = #"""
    {"context":"Demo cluster","nodes":[
      {"node":"192.0.2.20","hostname":"demo-worker-1",
       "volumes":[
         {"key":"192.0.2.20|STATE","name":"STATE","mount":"/system/state","usedPercent":6,"freeBytes":98566144,"sizeBytes":104857600,"level":"ok"},
         {"key":"192.0.2.20|EPHEMERAL","name":"EPHEMERAL","mount":"/var","usedPercent":91,"freeBytes":9663676416,"sizeBytes":107374182400,"level":"warning"}],
       "disks":[{"key":"192.0.2.20|smart|nvme0n1","device":"nvme0n1","model":"Demo NVMe","health":"ok"}]},
      {"node":"192.0.2.21","hostname":"demo-worker-2","volumes":[],
       "disks":[
         {"key":"192.0.2.21|smart|nvme0n1","device":"nvme0n1","model":"Demo NVMe","health":"failing","reason":"available spare below threshold"},
         {"key":"192.0.2.21|smart|vda","device":"vda","health":"unknown"}]},
      {"node":"192.0.2.30","hostname":"192.0.2.30","volumes":[],"disks":[],"error":"connection refused"}]}
    """#

    private let eph = "192.0.2.20|EPHEMERAL"
    private let smart = "192.0.2.21|smart|nvme0n1"

    private func decoded() throws -> ClusterStorageHealth { try TalosJSON.decode(ClusterStorageHealth.self, from: demo) }

    private func health(ephemeral percent: Double) -> ClusterStorageHealth {
        ClusterStorageHealth(context: "lab", nodes: [
            StorageNode(node: "192.0.2.20", hostname: "demo-worker-1", volumes: [
                StorageVolume(key: eph, name: "EPHEMERAL", usedPercent: percent, freeBytes: 2 << 30, sizeBytes: 100 << 30),
            ]),
        ])
    }

    private func snap(_ issues: [String: String]?, watched: Bool = true, context: String = "lab", warn: Int = 85) -> ClusterSnapshot {
        ClusterSnapshot(context: context, takenAt: now, nodes: ["192.0.2.20": NodeState(hostname: "demo-worker-1", health: .ready)],
                        etcdChecked: true, certNotAfter: farCert,
                        storageWatched: watched, storageChecked: watched && issues != nil, storageIssues: issues ?? [:],
                        storageWarn: warn)
    }

    private func issues(_ percent: Double, _ thresholds: StorageThresholds = StorageThresholds()) -> [String: String] {
        storageIssuesOf(health(ephemeral: percent), thresholds: thresholds)
    }

    // MARK: issues

    func testDecodesTheGoShape() throws {
        let health = try decoded()
        XCTAssertEqual(health.nodes.count, 3)
        XCTAssertEqual(health.nodes[0].volumes[1].usedPercent, 91)
        XCTAssertEqual(health.nodes[0].volumes[1].sizeBytes, 107_374_182_400)
        XCTAssertEqual(health.nodes[1].disks[0].reason, "available spare below threshold")
        XCTAssertEqual(health.nodes[1].disks[1].model, "")
        XCTAssertEqual(health.nodes[2].error, "connection refused")
        XCTAssertNil(health.nodes[0].error)
    }

    func testIssuesAtTheDefaultThresholds() throws {
        let out = storageIssuesOf(try decoded(), thresholds: StorageThresholds())
        XCTAssertEqual(Set(out.keys), [eph, smart])
        let fill = StorageIssue(value: out[eph] ?? "")
        XCTAssertEqual(fill.severity, dataWarning)
        XCTAssertEqual(fill.hostname, "demo-worker-1")
        XCTAssertEqual(fill.name, "EPHEMERAL")
        XCTAssertEqual(fill.percent, "91")
        XCTAssertFalse(fill.isSmart)
        let disk = StorageIssue(value: out[smart] ?? "")
        XCTAssertEqual(disk.severity, dataCritical)
        XCTAssertTrue(disk.isSmart)
        XCTAssertEqual(disk.name, "nvme0n1")
        XCTAssertEqual(disk.detail, "available spare below threshold")
    }

    func testThresholdsApplyToTheFill() {
        XCTAssertTrue(issues(84.9).isEmpty)
        XCTAssertEqual(StorageIssue(value: issues(85)[eph] ?? "").severity, dataWarning)
        XCTAssertEqual(StorageIssue(value: issues(95)[eph] ?? "").severity, dataCritical)
        // Custom: warning from 70 %, critical from 80 %.
        let custom = StorageThresholds(warn: 70, crit: 80)
        XCTAssertTrue(issues(69, custom).isEmpty)
        XCTAssertEqual(StorageIssue(value: issues(75, custom)[eph] ?? "").severity, dataWarning)
        XCTAssertEqual(StorageIssue(value: issues(80, custom)[eph] ?? "").severity, dataCritical)
    }

    func testThresholdsAreClamped() {
        XCTAssertEqual(StorageThresholds(warn: 10, crit: 20), StorageThresholds(warn: 85, crit: 95))
        XCTAssertEqual(StorageThresholds(warn: 90, crit: 90).crit, 95)
        XCTAssertEqual(StorageThresholds(warn: 97, crit: 50).crit, 98)
        XCTAssertEqual(StorageThresholds(warn: 98, crit: 99).crit, 99)
        XCTAssertEqual(storageCritRange(warn: 85), 86...99)
        XCTAssertEqual(storageCritRange(warn: 98), 99...99)
    }

    func testANodeThatDidNotAnswerKeepsItsIssues() throws {
        let gone = "192.0.2.30|EPHEMERAL"
        let goneValue = StorageIssue(severity: dataCritical, kind: StorageIssue.fill, hostname: "demo-cp-1", name: "EPHEMERAL",
                                     percent: "97").value
        let other = "192.0.2.31|STATE"
        let out = storageIssuesOf(try decoded(), thresholds: StorageThresholds(), known: [gone: goneValue, other: goneValue])
        XCTAssertEqual(out[gone], goneValue)
        // A node that answered is judged on what it says, one no longer listed resolves.
        XCTAssertNil(out[other])

        let result = evaluate(previous: snap([gone: goneValue]), current: snap(out), now: now)
        XCTAssertEqual(result.alerts.map(\.key), ["storage:\(smart)"])
        XCTAssertEqual(result.next.storageIssues[gone], goneValue)
    }

    func testKnownIssuesOnlyFromTheSameWatchedCluster() {
        let known = issues(96)
        XCTAssertEqual(knownStorageIssues(snap(known), context: "lab"), known)
        XCTAssertEqual(knownStorageIssues(snap(known, context: "other"), context: "lab"), [:])
        XCTAssertEqual(knownStorageIssues(snap(nil), context: "lab"), [:])
        XCTAssertEqual(knownStorageIssues(nil, context: "lab"), [:])
    }

    func testIssueValueRoundTrips() {
        let issue = StorageIssue(severity: dataCritical, kind: StorageIssue.smart, hostname: "w|1", name: "sda",
                                 detail: "media errors | 3")
        let back = StorageIssue(value: issue.value)
        XCTAssertEqual(back.hostname, "w/1")
        XCTAssertEqual(back.detail, "media errors | 3")
        XCTAssertTrue(back.isSmart)
        XCTAssertEqual(StorageIssue(value: "").severity, dataWarning)
        XCTAssertEqual(storagePercentText(91), "91")
        XCTAssertEqual(storagePercentText(91.46), "91.5")
        XCTAssertEqual(storageIssueNode("fd00::7|smart|sda"), "fd00::7")
    }

    // MARK: monitor

    func testFirstCheckIsASilentBaseline() {
        let result = evaluate(previous: nil, current: snap(issues(97)), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
        XCTAssertEqual(result.next.storageIssues.keys.sorted(), [eph])
    }

    func testCriticalFillAndSmartAlertAtOnce() throws {
        let result = evaluate(previous: snap([:]), current: snap(issues(96)), now: now)
        let alert = try XCTUnwrap(result.alerts.first)
        XCTAssertEqual(result.alerts.count, 1)
        XCTAssertEqual(alert.key, "storage:\(eph)")
        XCTAssertEqual(alert.title, "EPHEMERAL on demo-worker-1 at 96 %, almost full")
        XCTAssertEqual(alert.text, "\(formatBytes(UInt64(2 << 30))) free of \(formatBytes(UInt64(100 << 30)))")
        XCTAssertTrue(alert.problem)

        let disk = try storageIssuesOf(decoded(), thresholds: StorageThresholds()).filter { $0.key == smart }
        let failing = try XCTUnwrap(evaluate(previous: snap([:]), current: snap(disk), now: now).alerts.first)
        XCTAssertEqual(failing.title, "SMART failing on nvme0n1 (demo-worker-2)")
        XCTAssertEqual(failing.text, "available spare below threshold")
    }

    func testWarningNeedsTwoChecksInARow() {
        let first = evaluate(previous: snap([:]), current: snap(issues(91)), now: now)
        XCTAssertTrue(first.alerts.isEmpty)
        XCTAssertEqual(first.next.storagePending, [eph])
        let second = evaluate(previous: first.next, current: snap(issues(92)), now: now)
        XCTAssertEqual(second.alerts.map(\.title), ["EPHEMERAL on demo-worker-1 at 92 %"])
        XCTAssertTrue(evaluate(previous: second.next, current: snap(issues(93)), now: now).alerts.isEmpty)
        // Worse: critical alerts again.
        XCTAssertEqual(evaluate(previous: second.next, current: snap(issues(96)), now: now).alerts.count, 1)
        // Seen once then gone: never alerts.
        XCTAssertTrue(evaluate(previous: first.next, current: snap(issues(50)), now: now).alerts.isEmpty)
    }

    func testResolvedSaysSoOnce() {
        let full = snap(issues(96), warn: 80)
        let result = evaluate(previous: full, current: snap(issues(60), warn: 80), now: now)
        XCTAssertEqual(result.alerts.map(\.title), ["EPHEMERAL on demo-worker-1 back under 80 %"])
        XCTAssertEqual(result.alerts.map(\.problem), [false])
        XCTAssertTrue(evaluate(previous: result.next, current: snap(issues(60)), now: now).alerts.isEmpty)

        let disk = StorageIssue(severity: dataCritical, kind: StorageIssue.smart, hostname: "demo-worker-2", name: "sda").value
        let passed = evaluate(previous: snap([smart: disk]), current: snap([:]), now: now)
        XCTAssertEqual(passed.alerts.map(\.title), ["sda on demo-worker-2 passes SMART again"])
    }

    func testAnUnreadableCheckKeepsWhatWasKnownAndOffForgets() {
        let known = snap(issues(96))
        let unread = evaluate(previous: known, current: snap(nil), now: now)
        XCTAssertTrue(unread.alerts.isEmpty)
        XCTAssertEqual(unread.next.storageIssues, known.storageIssues)
        let off = evaluate(previous: known, current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.next.storageIssues.isEmpty)
        XCTAssertTrue(evaluate(previous: off.next, current: snap(issues(96)), now: now).alerts.isEmpty)
    }

    func testSnapshotsCarryTheTrack() throws {
        let overview = try TalosJSON.decode(ClusterOverview.self, from: #"{"context":"lab","nodes":[]}"#)
        let unread = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, storageWatched: true, storageIssues: nil, storageWarn: 70)
        XCTAssertTrue(unread.storageWatched)
        XCTAssertFalse(unread.storageChecked)
        XCTAssertEqual(unread.storageWarn, 70)
        let read = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, storageWatched: true, storageIssues: issues(96))
        XCTAssertTrue(read.storageChecked)
        let off = snapshotOf(overview, etcd: nil, certNotAfter: 0, takenAt: now, storageWatched: false, storageIssues: issues(96))
        XCTAssertTrue(off.storageIssues.isEmpty)
        // An older snapshot without the fields decodes.
        let old = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(#"{"context":"lab","takenAt":0,"nodes":{}}"#.utf8))
        XCTAssertFalse(old.storageWatched)
        XCTAssertEqual(old.storageWarn, storageWarnDefault)
        let back = try JSONDecoder().decode(ClusterSnapshot.self, from: JSONEncoder().encode(read))
        XCTAssertEqual(back, read)
    }

    // MARK: links and actions

    func testKeysLinksAndActions() {
        let snapshot = snap(issues(96))
        XCTAssertEqual(ShareTarget.forAlert(key: "storage:\(eph)", snapshot: snapshot),
                       .storage(address: "192.0.2.20", hostname: "demo-worker-1"))
        // Resolved: the hostname from the node's state.
        XCTAssertEqual(ShareTarget.forAlert(key: "storage:\(eph)", snapshot: snap([:])),
                       ShareTarget(target: .storage, host: "demo-worker-1", addr: "192.0.2.20"))
        XCTAssertNil(ShareTarget.forAlert(key: "storage:", snapshot: snapshot))
        XCTAssertEqual(alertKind(key: "storage:\(smart)"), "storage")
        XCTAssertEqual(alertActions(key: "storage:\(smart)", problem: true, canWake: true), [.snooze])
        XCTAssertEqual(alertActions(key: "storage:\(smart)", problem: false, canWake: false), [])
        XCTAssertEqual(alertCategory(kind: "storage", actions: [.snooze], hideDetails: true), "alert.storage.snooze.private")
    }

    func testStorageLinksRoundTrip() throws {
        let json = String(decoding: try JSONEncoder().encode(ShareTarget.storage(address: "10.0.0.5", hostname: "worker-1")), as: UTF8.self)
        XCTAssertEqual(try TalosJSON.decode(ShareTarget.self, from: json), .storage(address: "10.0.0.5", hostname: "worker-1"))
        XCTAssertEqual(try TalosJSON.decode(ShareTarget.self, from: #"{"target":"storage","host":"worker-1","addr":"10.0.0.5"}"#).target,
                       .storage)
    }
}
