import XCTest
@testable import IchorCore

final class HistoryTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var nowMillis: Int64 { 1_800_000_000_000 }
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    private func snapshot(_ nodes: [String: NodeState] = ["10.0.0.11": NodeState(hostname: "cp-1", health: .ready),
                                                         "10.0.0.21": NodeState(hostname: "worker-1", health: .notReady)],
                          etcdChecked: Bool = true, alarms: [String] = [],
                          data: [String: String]? = nil, storage: [String: String]? = nil,
                          am: [String: String]? = nil, cert: Int64? = nil, kube: Bool = false) -> ClusterSnapshot {
        ClusterSnapshot(context: "lab", takenAt: now, nodes: nodes, etcdAlarms: alarms, etcdChecked: etcdChecked,
                        certNotAfter: cert ?? farCert,
                        dataWatched: data != nil, dataChecked: data != nil, dataIssues: data ?? [:],
                        amWatched: am != nil, amChecked: am != nil, amIssues: am ?? [:],
                        storageWatched: storage != nil, storageChecked: storage != nil, storageIssues: storage ?? [:],
                        kube: kube)
    }

    // MARK: record

    func testRecordFromAReadSnapshot() throws {
        let extras = HistoryExtras(versions: ["10.0.0.11": "v1.11.2"], memory: ["10.0.0.11": 41.25],
                                   volumes: [HistoryVolumeRecord(key: "10.0.0.11|EPHEMERAL", name: "EPHEMERAL", node: "10.0.0.11", usedPercent: 62.4)])
        let snap = snapshot(data: ["longhorn|pvc-data": dataWarning])
        let record = historyRecord(at: now, current: snap, evaluated: snap, extras: extras, previousAlerts: [], now: now)
        XCTAssertEqual(record.at, nowMillis)
        XCTAssertTrue(record.reachable)
        XCTAssertEqual(record.nodes.map(\.node), ["10.0.0.11", "10.0.0.21"])
        XCTAssertEqual(record.nodes[0].version, "v1.11.2")
        XCTAssertEqual(record.nodes[0].memUsedPercent, 41.25)
        XCTAssertEqual(record.nodes[1].health, "notReady")
        XCTAssertNil(record.nodes[1].version)
        XCTAssertEqual(record.volumes.count, 1)
        XCTAssertEqual(record.alerts, [HistoryAlertRecord(key: "longhorn|pvc-data", track: "data", severity: "warning", title: "pvc-data")])
    }

    func testRecordJSONHasTheContractShape() throws {
        let snap = snapshot(["10.0.0.21": NodeState(hostname: "worker-1", health: .notReady)])
        let json = try historyRecord(at: now, current: snap, evaluated: snap, extras: HistoryExtras(), previousAlerts: [], now: now).json()
        XCTAssertEqual(json, #"{"alerts":[],"at":1800000000000,"nodes":[{"health":"notReady","hostname":"worker-1","node":"10.0.0.21"}],"reachable":true,"volumes":[]}"#)
    }

    func testUnansweredClusterIsAGapThatResendsTheOpenAlerts() {
        let open = [HistoryAlertRecord(key: "fp1", track: "am", title: "KubePodCrashLooping")]
        let none = historyRecord(at: now, current: nil, evaluated: nil, extras: HistoryExtras(), previousAlerts: open, now: now)
        XCTAssertFalse(none.reachable)
        XCTAssertTrue(none.nodes.isEmpty)
        XCTAssertEqual(none.alerts, open)

        let down = snapshot(["10.0.0.11": NodeState(hostname: "cp-1", health: .unreachable)])
        let whole = historyRecord(at: now, current: down, evaluated: down, extras: HistoryExtras(), previousAlerts: open, now: now)
        XCTAssertFalse(whole.reachable)
        XCTAssertEqual(whole.nodes.map(\.health), ["unreachable"])
        XCTAssertEqual(whole.alerts, open)
    }

    func testMaskedRunsRecordNothing() {
        XCTAssertFalse(historyRecordingAllowed(privacyMasked: true))
        XCTAssertTrue(historyRecordingAllowed(privacyMasked: false))
    }

    // MARK: ring

    private struct NewerVersion: Error {}

    func testNextRingKeepsWhatCannotBeAppendedTo() {
        let stored = Data([1, 2, 3])
        var calls: [Data?] = []
        XCTAssertNil(historyNextRing(.unreadable) { calls.append($0); return Data([9]) })
        XCTAssertTrue(calls.isEmpty) // a locked ring is never replaced
        XCTAssertNil(historyNextRing(.ring(stored)) { _ in throw NewerVersion() })
        XCTAssertNil(historyNextRing(.ring(stored)) { _ in Data() })
        XCTAssertEqual(historyNextRing(.ring(stored)) { calls.append($0); return Data([4]) }, Data([4]))
        XCTAssertEqual(historyNextRing(.none) { calls.append($0); return Data([5]) }, Data([5]))
        XCTAssertEqual(calls, [stored, nil])
    }

    // MARK: open alerts

    func testOpenAlertsAcrossTracks() {
        let storage = StorageIssue(severity: dataCritical, kind: StorageIssue.fill, hostname: "worker-1", name: "EPHEMERAL", percent: "97").value
        let am = AMIssue(severity: dataWarning, alertname: "KubePodCrashLooping", subject: "ns/pod").value
        let snap = snapshot(alarms: ["1234:NOSPACE"], storage: ["10.0.0.21|EPHEMERAL": storage], am: ["abc": am],
                            cert: Int64(now.timeIntervalSince1970) - 86_400)
        let alerts = historyOpenAlerts(snap, previous: [], now: now)
        XCTAssertEqual(alerts.map(\.track), ["am", "cert", "etcd", "storage"])
        XCTAssertEqual(alerts[0].title, "KubePodCrashLooping")
        XCTAssertEqual(alerts[1].severity, dataCritical)
        XCTAssertEqual(alerts[2], HistoryAlertRecord(key: "1234:NOSPACE", track: "etcd", severity: dataCritical, title: "NOSPACE"))
        XCTAssertEqual(alerts[3].title, "EPHEMERAL (worker-1)")
        XCTAssertEqual(alerts[3].severity, dataCritical)
    }

    func testUnreadTracksResendTheirPreviousKeys() {
        let previous = [HistoryAlertRecord(key: "1234:NOSPACE", track: "etcd"),
                        HistoryAlertRecord(key: "longhorn|vol", track: "data", severity: dataWarning),
                        HistoryAlertRecord(key: "abc", track: "am")]
        // etcd and data not read; Alertmanager read with nothing open; storage not watched.
        var snap = snapshot(etcdChecked: false, data: [:], am: [:])
        snap.dataChecked = false
        let alerts = historyOpenAlerts(snap, previous: previous, now: now)
        XCTAssertEqual(alerts.map(\.key), ["longhorn|vol", "1234:NOSPACE"])
    }

    func testKubeClustersHaveNoEtcdTrack() {
        let snap = snapshot(etcdChecked: false, kube: true)
        XCTAssertTrue(historyOpenAlerts(snap, previous: [HistoryAlertRecord(key: "x:y", track: "etcd")], now: now).isEmpty)
    }

    func testCertWarnsOnlyWithinTheWarningDays() {
        XCTAssertTrue(historyOpenAlerts(snapshot(), previous: [], now: now).isEmpty)
        let soon = Int64(now.timeIntervalSince1970) + 3 * 86_400
        XCTAssertEqual(historyOpenAlerts(snapshot(cert: soon), previous: [], now: now).first?.severity, dataWarning)
    }

    // MARK: extras

    func testExtrasFromATalosOverview() {
        let overview = ClusterOverview(context: "lab", nodes: [
            NodeOverview(node: "10.0.0.11", hostname: "cp-1", reachable: true, version: "v1.11.2", ready: true,
                         memTotal: 1000, memAvailable: 250),
            NodeOverview(node: "10.0.0.21", hostname: "worker-1", reachable: false),
        ])
        let extras = historyExtras(overview)
        XCTAssertEqual(extras.versions, ["10.0.0.11": "v1.11.2"])
        XCTAssertEqual(extras.memory, ["10.0.0.11": 75])
    }

    func testExtrasFromAKubeNodeList() throws {
        let json = #"{"nodes":[{"name":"node-a","ready":true,"kubelet":"v1.31.2","cpu":2,"memory":4096,"podLimit":110},{"name":"node-b","ready":false,"cpu":2,"memory":4096,"podLimit":110}]}"#
        let extras = historyExtras(try TalosJSON.decode(KubeNodesOverview.self, from: json))
        XCTAssertEqual(extras.versions, ["node-a": "v1.31.2"])
        XCTAssertTrue(extras.memory.isEmpty)
    }

    func testVolumesOfEveryNodeThatAnswered() {
        let health = ClusterStorageHealth(context: "lab", nodes: [
            StorageNode(node: "10.0.0.11", hostname: "cp-1", volumes: [StorageVolume(key: "10.0.0.11|STATE", name: "STATE", usedPercent: 6)]),
            StorageNode(node: "10.0.0.12", hostname: "cp-2", error: "connection refused"),
        ])
        XCTAssertEqual(historyVolumes(health), [HistoryVolumeRecord(key: "10.0.0.11|STATE", name: "STATE", node: "10.0.0.11", usedPercent: 6)])
    }

    // MARK: query and since

    private let query = #"""
    {"from":1780000000000,"to":1780086400000,"records":96,"resetAt":1779990000000,"resetReason":"history cannot be read",
     "nodes":[{"node":"10.0.0.21","hostname":"worker-1","uptimePercent":97.92,
               "intervals":[{"state":"ready","from":1780000000000,"to":1780040000000},
                            {"state":"notReady","from":1780040000000,"to":1780041800000}]}],
     "alerts":[{"key":"longhorn|pvc-data","track":"data","severity":"warning","title":"pvc-data degraded","openedAt":1780010000000,"closedAt":1780012700000},
               {"key":"abc","track":"am","title":"KubePodCrashLooping","openedAt":1780020000000}],
     "volumes":[{"key":"10.0.0.11|EPHEMERAL","name":"EPHEMERAL","node":"10.0.0.11","series":[[1780000000000,61.9],[1780000900000,62.4]]}],
     "memory":[{"node":"10.0.0.11","series":[[1780000000000,41.3]]}],
     "gaps":[{"from":1780050000000,"to":1780063000000}]}
    """#

    func testDecodesTheQuery() throws {
        let result = try TalosJSON.decode(HistoryQueryResult.self, from: query)
        XCTAssertEqual(result.records, 96)
        XCTAssertEqual(result.resetAt, 1_779_990_000_000)
        XCTAssertEqual(result.node("10.0.0.21")?.uptimePercent, 97.92)
        XCTAssertEqual(result.node("10.0.0.21")?.intervals.count, 2)
        XCTAssertEqual(result.volumes(of: "10.0.0.11")["EPHEMERAL"], [HistoryPoint(at: 1_780_000_000_000, value: 61.9),
                                                                     HistoryPoint(at: 1_780_000_900_000, value: 62.4)])
        XCTAssertEqual(result.memory(of: "10.0.0.11"), [HistoryPoint(at: 1_780_000_000_000, value: 41.3)])
        XCTAssertEqual(result.gaps, [HistorySpan(from: 1_780_050_000_000, to: 1_780_063_000_000)])
        XCTAssertEqual(result.openAlerts, [HistoryAlertRecord(key: "abc", track: "am", title: "KubePodCrashLooping")])
    }

    func testDecodesAnEmptyQuery() throws {
        let result = try TalosJSON.decode(HistoryQueryResult.self, from: #"{"from":1,"to":2,"records":0}"#)
        XCTAssertNil(result.resetAt)
        XCTAssertTrue(result.nodes.isEmpty && result.openAlerts.isEmpty && result.memory(of: "x").isEmpty)
    }

    func testSinceHasNewsOnlyForWhatChangedAfterTheLastLook() throws {
        let json = #"""
        {"from":1780000000000,"to":1780086400000,"records":12,
         "nodesRecovered":[],"nodesDown":[{"node":"10.0.0.22","hostname":"worker-2","state":"notReady","downAt":1779000000000}],
         "alertsResolved":[],"alertsOpen":[{"key":"abc","track":"am","openedAt":1779900000000}],"upgrades":[]}
        """#
        let seen = try TalosJSON.decode(HistorySinceSummary.self, from: json)
        XCTAssertFalse(seen.hasNews) // down and open before the last look: already seen
        XCTAssertEqual(seen.nodesDown.first?.label, "worker-2")

        let fresh = HistorySinceSummary(from: 1_780_000_000_000, to: 1_780_086_400_000,
                                        alertsOpen: [HistoryAlertSpan(key: "abc", track: "am", openedAt: 1_780_000_000_001)])
        XCTAssertTrue(fresh.hasNews)
        XCTAssertTrue(fresh.isNew(fresh.alertsOpen[0]))
        let recovered = HistorySinceSummary(from: 1, to: 2, nodesRecovered: [HistoryNodeOutage(node: "n", hostname: "", state: "unreachable", downAt: 0, upAt: 2)])
        XCTAssertTrue(recovered.hasNews)
        XCTAssertEqual(recovered.nodesRecovered[0].label, "n")
        XCTAssertTrue(HistorySinceSummary(from: 1, to: 2, upgrades: [HistoryUpgrade(node: "n", hostname: "h", from: "a", to: "b", at: 2)]).hasNews)
    }

    func testLastLookedRules() {
        XCTAssertNil(HistoryLastLooked.since(stored: nil))
        XCTAssertNil(HistoryLastLooked.since(stored: 0))
        XCTAssertEqual(HistoryLastLooked.since(stored: 42), 42)
        XCTAssertEqual(HistoryLastLooked.advanced(stored: nil, to: 10), 10)
        XCTAssertEqual(HistoryLastLooked.advanced(stored: 20, to: 10), 20)
        XCTAssertEqual(HistoryLastLooked.keep(["a": 1, "b": 2], clusters: ["b", "c"]), ["b": 2])
    }

    // MARK: strip

    func testStripFillsWhatNoIntervalCovers() {
        let intervals = [HistoryInterval(state: "notReady", from: 50, to: 60), HistoryInterval(state: "ready", from: 20, to: 50),
                         HistoryInterval(state: "ready", from: 70, to: 150)]
        let strip = historyStrip(intervals, from: 0, to: 100)
        XCTAssertEqual(strip, [
            HistoryStripSegment(state: .noData, start: 0, end: 0.2),
            HistoryStripSegment(state: .ready, start: 0.2, end: 0.5),
            HistoryStripSegment(state: .notReady, start: 0.5, end: 0.6),
            HistoryStripSegment(state: .noData, start: 0.6, end: 0.7),
            HistoryStripSegment(state: .ready, start: 0.7, end: 1),
        ])
        XCTAssertEqual(historyStrip([], from: 0, to: 10), [HistoryStripSegment(state: .noData, start: 0, end: 1)])
        XCTAssertTrue(historyStrip(intervals, from: 10, to: 10).isEmpty)
    }

    func testStripMergesNeighboursOfOneState() {
        let strip = historyStrip([HistoryInterval(state: "ready", from: 0, to: 5), HistoryInterval(state: "ready", from: 5, to: 10)], from: 0, to: 10)
        XCTAssertEqual(strip, [HistoryStripSegment(state: .ready, start: 0, end: 1)])
    }

    func testPeriodsAndUptimeText() {
        XCTAssertEqual(HistoryPeriod.week.since(now: 7 * 86_400_000), 0)
        XCTAssertEqual(HistoryPeriod.month.since(now: 30 * 86_400_000), 0)
        let posix = Locale(identifier: "en_US_POSIX")
        XCTAssertEqual(historyUptimeText(97.92, locale: posix), "97.9 %")
        XCTAssertEqual(historyUptimeText(100, locale: posix), "100 %")
        XCTAssertEqual(historyUptimeText(97.92, locale: Locale(identifier: "fr_FR")), "97,9 %")
        XCTAssertEqual(historyUptimeText(nil), "")
    }
}
