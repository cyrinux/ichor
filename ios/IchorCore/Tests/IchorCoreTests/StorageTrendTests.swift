import XCTest
@testable import IchorCore

final class StorageTrendTests: XCTestCase {
    /// The Go contract's sample (history_forecast.go).
    private let sample = #"""
    {"volumes":[
      {"key":"10.0.0.1|EPHEMERAL","name":"EPHEMERAL","node":"10.0.0.1",
       "usedPercent":56,"latestAt":1780000000000,"slopePerDay":2,
       "daysToFull":22,"daysToCritical":19.5,
       "confidence":"high","points":37,"spanHours":72},
      {"key":"10.0.0.1|STATE","name":"STATE","node":"10.0.0.1",
       "usedPercent":40,"latestAt":1780000000000,"slopePerDay":0,
       "confidence":"low","points":37,"spanHours":72}
    ]}
    """#

    private let eph = "192.0.2.20|EPHEMERAL"
    private let hosts = ["192.0.2.20": "demo-worker-1"]

    private func row(critical: Double?, full: Double? = nil, high: Bool = true, used: Double = 80,
                     slope: Double = 4) -> HistoryForecast {
        HistoryForecast(volumes: [
            VolumeForecast(key: eph, name: "EPHEMERAL", node: "192.0.2.20", usedPercent: used, slopePerDay: slope,
                           daysToFull: full ?? critical.map { $0 + 1.25 }, daysToCritical: critical,
                           confidence: high ? "high" : "low"),
        ])
    }

    private func run(_ forecast: HistoryForecast, open: [String: String] = [:],
                     fill: [String: String] = [:]) -> StorageTrendOutcome {
        evaluateStorageTrends(forecast, open: open, fillIssues: fill, hostnames: hosts)
    }

    // MARK: decoding

    func testDecodesTheGoShape() throws {
        let forecast = try TalosJSON.decode(HistoryForecast.self, from: sample)
        XCTAssertEqual(forecast.volumes.count, 2)
        let eph = forecast.volumes[0]
        XCTAssertEqual(eph.key, "10.0.0.1|EPHEMERAL")
        XCTAssertEqual(eph.usedPercent, 56)
        XCTAssertEqual(eph.latestAt, 1_780_000_000_000)
        XCTAssertEqual(eph.slopePerDay, 2)
        XCTAssertEqual(eph.daysToFull, 22)
        XCTAssertEqual(eph.daysToCritical, 19.5)
        XCTAssertTrue(eph.isHigh)
        XCTAssertEqual(eph.points, 37)
        let state = forecast.volumes[1]
        XCTAssertNil(state.daysToFull)
        XCTAssertNil(state.daysToCritical)
        XCTAssertFalse(state.isHigh)
    }

    func testDecodesAnEmptyAnswer() throws {
        XCTAssertEqual(try TalosJSON.decode(HistoryForecast.self, from: #"{"volumes":[]}"#).volumes, [])
        XCTAssertEqual(try TalosJSON.decode(HistoryForecast.self, from: "{}").volumes, [])
    }

    func testFindsAUserVolumeWithoutItsPrefix() {
        let forecast = HistoryForecast(volumes: [
            VolumeForecast(key: "n1|data", name: "data", node: "n1", usedPercent: 10),
            VolumeForecast(key: "n2|EPHEMERAL", name: "EPHEMERAL", node: "n2", usedPercent: 20),
        ])
        XCTAssertEqual(forecast.volume(node: "n1", name: "u-data")?.key, "n1|data")
        XCTAssertEqual(forecast.volume(node: "n2", name: "EPHEMERAL")?.usedPercent, 20)
        XCTAssertNil(forecast.volume(node: "n1", name: "EPHEMERAL"))
    }

    // MARK: wording

    func testDaysRoundAndSayUnderADay() {
        XCTAssertEqual(ForecastDays(0), .underADay)
        XCTAssertEqual(ForecastDays(0.9), .underADay)
        XCTAssertEqual(ForecastDays(1), .aboutADay)
        XCTAssertEqual(ForecastDays(1.4), .aboutADay)
        XCTAssertEqual(ForecastDays(1.5), .days(2))
        XCTAssertEqual(ForecastDays(19.5), .days(20))
        XCTAssertEqual(ForecastDays(0.5).english, "in < 1 day")
        XCTAssertEqual(ForecastDays(1.2).english, "in ~1 day")
        XCTAssertEqual(ForecastDays(22).english, "in ~22 days")
    }

    func testRowSaysFullAndCriticalWhenSooner() {
        let line = VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 56, slopePerDay: 2,
                                                     daysToFull: 22, daysToCritical: 19.5, confidence: "high"))
        XCTAssertEqual(line, .projected(full: .days(22), critical: .days(20)))
    }

    func testRowLeavesOutCriticalWhenItSaysNothingMore() {
        // Rounded alike.
        let alike = VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 98, slopePerDay: 40,
                                                      daysToFull: 0.05, daysToCritical: 0.01, confidence: "high"))
        XCTAssertEqual(alike, .projected(full: .underADay, critical: nil))
        // Already critical: no daysToCritical.
        let critical = VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 97, slopePerDay: 1,
                                                         daysToFull: 3, confidence: "high"))
        XCTAssertEqual(critical, .projected(full: .days(3), critical: nil))
        // Full beyond a year, critical within it.
        let far = VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 10, slopePerDay: 0.3,
                                                    daysToCritical: 300, confidence: "high"))
        XCTAssertEqual(far, .projected(full: nil, critical: .days(300)))
    }

    func testWeakFitShowsOnlyGrowth() {
        let growing = VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 40, slopePerDay: 0.46))
        XCTAssertEqual(growing, .growing("0.5"))
        XCTAssertNil(VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 40, slopePerDay: 0)))
        XCTAssertNil(VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 40, slopePerDay: -1)))
        XCTAssertNil(VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 40, slopePerDay: 0.02)))
        // High but beyond a year either way: nothing.
        XCTAssertNil(VolumeForecastLine(VolumeForecast(key: "k", name: "v", node: "n", usedPercent: 1, slopePerDay: 0.06,
                                                       confidence: "high")))
    }

    func testWithinAWeekStandsOut() {
        XCTAssertTrue(VolumeForecastLine.projected(full: .days(9), critical: .days(7)).isWithinAWeek)
        XCTAssertTrue(VolumeForecastLine.projected(full: .underADay, critical: nil).isWithinAWeek)
        XCTAssertFalse(VolumeForecastLine.projected(full: .days(8), critical: nil).isWithinAWeek)
        XCTAssertFalse(VolumeForecastLine.growing("3").isWithinAWeek)
    }

    func testIssueNamesCriticalOrFull() {
        let critical = StorageTrendIssue(row(critical: 2.5, full: 5).volumes[0], hostname: "w1")
        XCTAssertTrue(critical.critical)
        XCTAssertEqual(critical.when, .days(3))
        XCTAssertEqual(storageTrendTitle(critical), "EPHEMERAL on w1 critical in ~3 days")
        XCTAssertEqual(storageTrendText(critical), "growing ~4 % a day, now at 80 %")
        // A threshold of 100 %: critical and full come together, so it says full.
        let full = StorageTrendIssue(row(critical: 3, full: 3).volumes[0], hostname: "w1")
        XCTAssertFalse(full.critical)
        XCTAssertEqual(storageTrendTitle(full), "EPHEMERAL on w1 full in ~3 days")
    }

    func testIssueValueRoundTrips() {
        let issue = StorageTrendIssue(hostname: "w|1", name: "EPHEMERAL", critical: true, days: 2.5, slopePerDay: 4.25, usedPercent: 81.5)
        let back = StorageTrendIssue(value: issue.value)
        XCTAssertEqual(back.hostname, "w/1")
        XCTAssertEqual(back.name, "EPHEMERAL")
        XCTAssertTrue(back.critical)
        XCTAssertEqual(back.days, 2.5)
        XCTAssertEqual(back.slopePerDay, 4.25)
        XCTAssertEqual(back.usedPercent, 81.5)
        XCTAssertEqual(StorageTrendIssue(value: "").name, "")
    }

    func testTrendKeys() {
        XCTAssertEqual(storageTrendAlertKey(eph), "storage:192.0.2.20|EPHEMERAL:trend")
        XCTAssertEqual(storageTrendVolumeKey(subject: "192.0.2.20|EPHEMERAL:trend"), eph)
        XCTAssertNil(storageTrendVolumeKey(subject: eph))
        XCTAssertEqual(alertActions(key: storageTrendAlertKey(eph), problem: true, canWake: false), [.snooze])
        XCTAssertEqual(alertKind(key: storageTrendAlertKey(eph)), "storage")
    }

    // MARK: hysteresis

    func testOpensWithinThreeDaysAtOnce() {
        let out = run(row(critical: 2.5))
        XCTAssertEqual(out.alerts.count, 1)
        XCTAssertEqual(out.alerts[0].key, "storage:\(eph):trend")
        XCTAssertTrue(out.alerts[0].problem)
        XCTAssertEqual(out.alerts[0].title, "EPHEMERAL on demo-worker-1 critical in ~3 days")
        XCTAssertEqual(StorageTrendIssue(value: out.open[eph] ?? "").hostname, "demo-worker-1")
        XCTAssertEqual(run(row(critical: 3)).alerts.count, 1)
    }

    func testDoesNotOpenBetweenThreeAndSevenDays() {
        let out = run(row(critical: 5))
        XCTAssertEqual(out.alerts, [])
        XCTAssertEqual(out.open, [:])
    }

    func testStaysOpenQuietlyWithinSevenDays() {
        let open = run(row(critical: 2)).open
        for days in [1.0, 3.0, 5.0, 7.0] {
            let out = run(row(critical: days), open: open)
            XCTAssertEqual(out.alerts, [], "\(days)")
            XCTAssertNotNil(out.open[eph], "\(days)")
        }
    }

    func testResolvesOncePastSevenDays() {
        let open = run(row(critical: 2)).open
        let out = run(row(critical: 7.5), open: open)
        XCTAssertEqual(out.alerts.count, 1)
        XCTAssertFalse(out.alerts[0].problem)
        XCTAssertEqual(out.alerts[0].title, "EPHEMERAL on demo-worker-1 no longer filling up fast")
        XCTAssertEqual(out.open, [:])
        XCTAssertEqual(run(row(critical: 7.5), open: out.open).alerts, [])
    }

    func testResolvesWithoutAProjection() {
        let open = run(row(critical: 2)).open
        XCTAssertEqual(run(row(critical: 2, high: false), open: open).alerts.map(\.problem), [false])
        XCTAssertEqual(run(row(critical: nil, full: 2), open: open).alerts.map(\.problem), [false])
        XCTAssertEqual(run(HistoryForecast(), open: open).alerts.map(\.problem), [false])
    }

    func testLowConfidenceNeverOpens() {
        XCTAssertEqual(run(row(critical: 1, high: false)).alerts, [])
    }

    func testSkipsAVolumeInAFillAlert() {
        let fill = [eph: StorageIssue(severity: dataWarning, kind: StorageIssue.fill, hostname: "demo-worker-1", name: "EPHEMERAL").value]
        XCTAssertEqual(run(row(critical: 1), fill: fill), StorageTrendOutcome(alerts: [], open: [:]))
        // Open, then the fill alert takes over: dropped without a "resolved".
        let open = run(row(critical: 1)).open
        XCTAssertEqual(run(row(critical: 1), open: open, fill: fill), StorageTrendOutcome(alerts: [], open: [:]))
    }

    func testFallsBackToTheAddressWithoutAHostname() {
        let out = evaluateStorageTrends(row(critical: 1), open: [:], fillIssues: [:], hostnames: [:])
        XCTAssertEqual(StorageTrendIssue(value: out.open[eph] ?? "").hostname, "192.0.2.20")
    }

    // MARK: snapshot

    func testSnapshotCarriesTrendsAndOldOnesDecode() throws {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let nodes = ["192.0.2.20": NodeState(hostname: "demo-worker-1", health: .ready)]
        let trends = [eph: StorageTrendIssue(hostname: "demo-worker-1", name: "EPHEMERAL", critical: false, days: 2,
                                             slopePerDay: 3, usedPercent: 80).value]
        let previous = ClusterSnapshot(context: "lab", takenAt: now, nodes: nodes, storageWatched: true, storageChecked: true,
                                       storageTrends: trends)
        let current = ClusterSnapshot(context: "lab", takenAt: now, nodes: nodes, storageWatched: true, storageChecked: true)
        XCTAssertEqual(evaluate(previous: previous, current: current, now: now).next.storageTrends, trends)
        // Storage watch off, or another cluster: forgotten.
        let off = ClusterSnapshot(context: "lab", takenAt: now, nodes: nodes)
        XCTAssertEqual(evaluate(previous: previous, current: off, now: now).next.storageTrends, [:])
        let other = ClusterSnapshot(context: "other", takenAt: now, nodes: nodes, storageWatched: true, storageChecked: true)
        XCTAssertEqual(evaluate(previous: previous, current: other, now: now).next.storageTrends, [:])

        let encoded = try JSONEncoder().encode(previous)
        XCTAssertEqual(try JSONDecoder().decode(ClusterSnapshot.self, from: encoded).storageTrends, trends)
        let old = #"{"context":"lab","takenAt":0,"nodes":{}}"#
        XCTAssertEqual(try JSONDecoder().decode(ClusterSnapshot.self, from: Data(old.utf8)).storageTrends, [:])
    }

    func testTrendAlertLinksToTheNodeStorage() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let snapshot = ClusterSnapshot(context: "lab", takenAt: now, nodes: [:], storageTrends: [
            eph: StorageTrendIssue(hostname: "demo-worker-1", name: "EPHEMERAL", critical: true, days: 2, slopePerDay: 3,
                                   usedPercent: 80).value,
        ])
        XCTAssertEqual(ShareTarget.forAlert(key: storageTrendAlertKey(eph), snapshot: snapshot),
                       .storage(address: "192.0.2.20", hostname: "demo-worker-1"))
    }
}
