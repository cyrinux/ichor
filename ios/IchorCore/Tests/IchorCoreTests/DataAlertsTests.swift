import XCTest
@testable import IchorCore

final class DataAlertsTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)
    private var farCert: Int64 { Int64(now.timeIntervalSince1970) + 365 * 86_400 }

    private func snap(_ issues: [String: String]?, watched: Bool = true, context: String = "lab") -> ClusterSnapshot {
        ClusterSnapshot(context: context, takenAt: now, nodes: ["a": NodeState(hostname: "host-a", health: .ready)],
                        etcdChecked: true, certNotAfter: farCert,
                        dataWatched: watched, dataChecked: watched && issues != nil, dataIssues: issues ?? [:])
    }

    func testIssuesOfEachSystem() throws {
        let json = #"""
        {"longhorn":{"volumes":[{"name":"pvc-1","pvcNamespace":"app","pvcName":"search","health":"critical"},
                                {"name":"pvc-2","pvcNamespace":"app","pvcName":"db","health":"warning"},{"name":"pvc-3","health":"idle"}]},
         "garage":{"instances":[{"namespace":"s3","name":"main","status":"degraded"},{"namespace":"nas","name":"garage","status":"healthy","resyncErrors":0},
                                {"namespace":"old","name":"garage","status":"unavailable"}]},
         "cnpg":{"clusters":[{"namespace":"db","name":"down","health":"critical","reasons":["noInstance"]},
                             {"namespace":"db","name":"backups","health":"warning","reasons":["backupFailed"]},
                             {"namespace":"db","name":"moving","health":"warning","reasons":["switchover","instances"]}]}}
        """#
        let services = try TalosJSON.decode(DataServices.self, from: json)
        XCTAssertEqual(dataIssuesOf(services), [
            "cnpg|db/backups": dataWarning, "cnpg|db/down": dataCritical,
            "garage|old/garage": dataCritical, "garage|s3/main": dataWarning,
            "longhorn|app/db": dataWarning, "longhorn|app/search": dataCritical,
        ])
    }

    func testFirstCheckIsASilentBaseline() {
        let result = evaluate(previous: nil, current: snap(["longhorn|app/db": dataCritical]), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
        XCTAssertEqual(result.next.dataIssues, ["longhorn|app/db": dataCritical])
    }

    func testCriticalAlertsAtOnce() throws {
        let result = evaluate(previous: snap([:]), current: snap(["cnpg|db/down": dataCritical]), now: now)
        let alert = try XCTUnwrap(result.alerts.first)
        XCTAssertEqual(result.alerts.count, 1)
        XCTAssertEqual(alert.key, "data:cnpg|db/down")
        XCTAssertEqual(alert.title, "db/down needs attention")
        XCTAssertEqual(alert.text, "CloudNativePG · critical")
        XCTAssertTrue(alert.problem)
    }

    func testWarningNeedsTwoChecksInARow() {
        let first = evaluate(previous: snap([:]), current: snap(["longhorn|app/db": dataWarning]), now: now)
        XCTAssertTrue(first.alerts.isEmpty)
        XCTAssertEqual(first.next.dataPending, ["longhorn|app/db"])

        let second = evaluate(previous: first.next, current: snap(["longhorn|app/db": dataWarning]), now: now)
        XCTAssertEqual(second.alerts.map(\.key), ["data:longhorn|app/db"])
        XCTAssertTrue(second.next.dataPending.isEmpty)

        XCTAssertTrue(evaluate(previous: second.next, current: snap(["longhorn|app/db": dataWarning]), now: now).alerts.isEmpty)
    }

    func testAShortDegradationNeverAlerts() {
        let first = evaluate(previous: snap([:]), current: snap(["longhorn|app/db": dataWarning]), now: now)
        let healed = evaluate(previous: first.next, current: snap([:]), now: now)
        XCTAssertTrue(healed.alerts.isEmpty)
        XCTAssertTrue(healed.next.dataPending.isEmpty)
    }

    func testEscalationAlertsAgainButDeescalationDoesNot() {
        var warned = snap([:])
        warned.dataIssues = ["garage|s3/main": dataWarning]
        let worse = evaluate(previous: warned, current: snap(["garage|s3/main": dataCritical]), now: now)
        XCTAssertEqual(worse.alerts.map(\.problem), [true])
        XCTAssertTrue(evaluate(previous: worse.next, current: snap(["garage|s3/main": dataWarning]), now: now).alerts.isEmpty)
    }

    func testRecoveryIsNotifiedOnce() throws {
        var known = snap([:])
        known.dataIssues = ["cnpg|db/down": dataCritical]
        let result = evaluate(previous: known, current: snap([:]), now: now)
        let alert = try XCTUnwrap(result.alerts.first)
        XCTAssertEqual(alert.title, "db/down is healthy again")
        XCTAssertFalse(alert.problem)
        XCTAssertTrue(result.next.dataIssues.isEmpty)
    }

    func testAnUnreadableCheckKeepsWhatWasKnown() {
        var known = snap([:])
        known.dataIssues = ["cnpg|db/down": dataCritical]
        known.dataPending = ["longhorn|app/db"]
        let result = evaluate(previous: known, current: snap(nil), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
        XCTAssertEqual(result.next.dataIssues, known.dataIssues)
        XCTAssertEqual(result.next.dataPending, known.dataPending)
        XCTAssertTrue(result.next.dataChecked)
    }

    func testTurningWatchingOffForgetsAndOnAgainIsABaseline() {
        var known = snap([:])
        known.dataIssues = ["cnpg|db/down": dataCritical]
        let off = evaluate(previous: known, current: snap(nil, watched: false), now: now)
        XCTAssertTrue(off.alerts.isEmpty)
        XCTAssertTrue(off.next.dataIssues.isEmpty)
        XCTAssertTrue(evaluate(previous: off.next, current: snap(["longhorn|app/search": dataCritical]), now: now).alerts.isEmpty)
    }

    func testAnotherClusterIsABaseline() {
        let result = evaluate(previous: snap([:], context: "lab"), current: snap(["cnpg|db/down": dataCritical], context: "prod"), now: now)
        XCTAssertTrue(result.alerts.isEmpty)
    }

    func testOlderSnapshotsStillDecode() throws {
        let old = #"{"context":"lab","takenAt":0,"nodes":{},"etcdAlarms":[],"etcdChecked":true,"certNotAfter":0,"lastCertWarnDay":-1}"#
        let decoded = try JSONDecoder().decode(ClusterSnapshot.self, from: Data(old.utf8))
        XCTAssertFalse(decoded.dataWatched)
        XCTAssertTrue(decoded.dataIssues.isEmpty)

        let roundTrip = try JSONDecoder().decode(ClusterSnapshot.self, from: JSONEncoder().encode(snap(["cnpg|db/down": dataCritical])))
        XCTAssertEqual(roundTrip.dataIssues, ["cnpg|db/down": dataCritical])
    }
}
