import XCTest

@testable import IchorCore

final class FleetGlanceTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func millis(_ date: Date) -> Int64 { Int64((date.timeIntervalSince1970 * 1000).rounded()) }

    private func row(
        _ key: String, _ tone: FleetTone, minutesAgo: Double? = 5, finding: String = ""
    ) -> FleetGlanceRow {
        let at = minutesAgo.map { millis(now.addingTimeInterval(-$0 * 60)) } ?? 0
        return FleetGlanceRow(key: key, name: key, tone: tone, checkedAt: at, finding: finding)
    }

    private var glance: FleetGlance {
        FleetGlance(rows: [
            row("prod-eu", .bad, finding: "Nodes: node-b unreachable"),
            row("staging", .warn, finding: "Argo CD"),
            row("edge", .unknown, minutesAgo: nil),
            row("lab", .ok), row("dev", .ok), row("ci", .ok),
        ])
    }

    func testCountsTheClustersNeedingAttentionWorstFirst() throws {
        let view = try XCTUnwrap(glance.view(at: now))
        XCTAssertEqual(view.attention, 2)
        XCTAssertEqual(view.total, 6)
        XCTAssertEqual(view.tone, .bad)
        XCTAssertEqual(view.rows.map(\.key), ["prod-eu", "staging"])
        XCTAssertEqual(view.unchecked, 1)
        XCTAssertFalse(view.allStale)
    }

    func testListsAtMostTheRowsAsked() throws {
        let many = FleetGlance(rows: (0..<5).map { row("bad-\($0)", .bad) } + [row("warn", .warn, minutesAgo: 1)])
        XCTAssertEqual(try XCTUnwrap(many.view(at: now)).rows.count, 3)
        XCTAssertEqual(try XCTUnwrap(many.view(at: now, maxRows: 1)).rows.map(\.key), ["bad-0"])
    }

    func testAFineFleetHasNoRows() throws {
        let view = try XCTUnwrap(FleetGlance(rows: [row("lab", .ok), row("dev", .ok)]).view(at: now))
        XCTAssertEqual(view.attention, 0)
        XCTAssertEqual(view.tone, .ok)
        XCTAssertTrue(view.rows.isEmpty)
    }

    func testACheckTurnedStaleSinceTheRankingReadsAsUnknown() throws {
        let later = now.addingTimeInterval(glanceStaleAfter)
        let view = try XCTUnwrap(glance.view(at: later))
        XCTAssertEqual(view.attention, 0)
        XCTAssertEqual(view.tone, .unknown)
        XCTAssertTrue(view.allStale)
        XCTAssertEqual(view.oldestCheck, now.addingTimeInterval(-5 * 60))
    }

    func testNoRankedCheckYetShowsNothing() {
        XCTAssertNil(FleetGlance(rows: []).view(at: now))
    }

    func testTheTallyCountsEachToneAtTheTime() {
        let tally = glance.tally(at: now)
        XCTAssertEqual(
            tally, FleetTally(bad: 1, warn: 1, unknown: 1, ok: 3, total: 6, takenAt: now.addingTimeInterval(-5 * 60)))
        XCTAssertEqual(tally.needAttention, 2)
        XCTAssertEqual(tally.tone(at: now), .bad)
        XCTAssertEqual(tally.tone(at: now.addingTimeInterval(glanceStaleAfter)), .unknown)
        XCTAssertEqual(FleetTally(unknown: 1, ok: 2, total: 3, takenAt: now).tone(at: now), .unknown)
        XCTAssertEqual(FleetTally(ok: 3, total: 3, takenAt: now).tone(at: now), .ok)
        XCTAssertEqual(FleetTally().tone(at: now), .unknown)
    }

    func testBuiltFromTheSummaryWithTheAppsWords() throws {
        let json = #"""
            {"clusters":[
              {"key":"a","name":"cluster-1","context":"c1","tone":"bad","checkedAt":1800000000000,
               "topFinding":{"track":"nodes","severity":"critical","subject":"node-b unreachable"}},
              {"key":"b","name":"cluster-2","context":"c2","tone":"ok","checkedAt":1800000000000}
            ],"counts":{"bad":1,"ok":1},"worst":"bad","groups":[]}
            """#
        let summary = try JSONDecoder().decode(FleetSummary.self, from: Data(json.utf8))
        let built = FleetGlance(summary: summary) { "\($0.track): \($0.subject)" }
        XCTAssertEqual(built.rows.map(\.key), ["a", "b"])
        XCTAssertEqual(built.rows[0].finding, "nodes: node-b unreachable")
        XCTAssertEqual(built.rows[1].finding, "")
        XCTAssertEqual(built.rows[0].tone, .bad)
    }

    func testARenameSinceTheCheckIsShown() {
        let renamed = glance.renamed([WidgetCluster(id: "lab", name: "Home lab"), WidgetCluster(id: "gone", name: "x")])
        XCTAssertEqual(renamed.rows.map(\.name), ["prod-eu", "staging", "edge", "Home lab", "dev", "ci"])
        XCTAssertEqual(renamed.rows.map(\.key), glance.rows.map(\.key))
    }

    func testStoredAndLoadedThroughTheAppGroup() throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "FleetGlanceTests"))
        defaults.removePersistentDomain(forName: "FleetGlanceTests")
        // An older version stored none: no glance, not a decoding error.
        XCTAssertNil(SharedFleet.load(from: defaults))
        defaults.set(try JSONEncoder().encode(glance), forKey: SharedFleet.key)
        XCTAssertEqual(SharedFleet.load(from: defaults), glance)
        // An unknown tone (a newer app) reads as unknown.
        let newer = #"{"rows":[{"key":"a","name":"a","tone":"purple","checkedAt":1}]}"#
        defaults.set(Data(newer.utf8), forKey: SharedFleet.key)
        XCTAssertEqual(SharedFleet.load(from: defaults)?.rows.first?.tone, .unknown)
    }

    func testTheWatchPayloadCarriesTheTallyAndReadsAnOlderOne() throws {
        let tally = FleetTally(bad: 1, ok: 2, total: 3, takenAt: now)
        let payload = WatchPayload(sentAt: now, masked: false, clusters: [], fleet: tally)
        XCTAssertEqual(WatchPayload.decode(payload.encoded())?.fleet, tally)
        // An older phone app sends no fleet.
        let older = WatchPayload(sentAt: now, masked: false, clusters: [])
        let encoded = try XCTUnwrap(older.encoded())
        XCTAssertFalse(String(decoding: encoded, as: UTF8.self).contains("fleet"))
        XCTAssertNil(WatchPayload.decode(encoded)?.fleet)
    }

    func testTheWidgetLinkOpensTheFleet() throws {
        XCTAssertTrue(isFleetLink(fleetWidgetURL))
        XCTAssertFalse(isFleetLink(try XCTUnwrap(URL(string: "ichor://open?c=x"))))
        XCTAssertFalse(isFleetLink(try XCTUnwrap(URL(string: "https://fleet"))))
        XCTAssertFalse(isFleetLink(try XCTUnwrap(URL(string: "ichor://fleet/other"))))
    }
}
