import XCTest
@testable import IchorCore

/// Same cases as Android's AlertRulesTest, AlertRulePlanTest and AlertMutesTest.
final class AlertRulesTests: XCTestCase {
    private let night = QuietHours(start: "23:00", end: "07:00", tz: "", exceptCritical: true)
    private var rules: AlertRules {
        AlertRules(
            default: AlertRule(quietHours: night, minSeverity: alertSeverityWarning),
            clusters: [
                "fp-staging": AlertRule(minSeverity: alertSeverityAll, digest: true),
                "fp-lab": AlertRule(quietHours: .off, minSeverity: alertSeverityCritical),
            ])
    }

    private func encoded(_ rules: AlertRules) throws -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return String(decoding: try encoder.encode(rules), as: UTF8.self)
    }

    // MARK: - Rules

    func testTheRulesRoundTrip() throws {
        XCTAssertEqual(AlertRules.decode(Data(rules.json.utf8)), rules)
    }

    func testAnAbsentFieldIsLeftOutForGoToReadAsTheDefault() throws {
        XCTAssertEqual(
            try encoded(AlertRules(clusters: ["fp": AlertRule(digest: true)])), #"{"clusters":{"fp":{"digest":true}}}"#)
        XCTAssertEqual(try encoded(.none), "{}")
        // A cluster's "Off" is {}: Go reads it as no window.
        XCTAssertEqual(
            try encoded(AlertRules(clusters: ["fp": AlertRule(quietHours: .off)])),
            #"{"clusters":{"fp":{"quietHours":{}}}}"#)
    }

    func testTheSampleRulesGoShipsDecode() throws {
        let relative = "go/cmd/probe/testdata/alert-rules.json"
        let byFile = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent(relative)
        let byDirectory = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
            .appendingPathComponent("../../\(relative)").standardizedFileURL
        let url = try XCTUnwrap(
            [byFile, byDirectory].first { FileManager.default.fileExists(atPath: $0.path) }, "\(relative) not found")
        let sample = try JSONDecoder().decode(AlertRules.self, from: Data(contentsOf: url))
        XCTAssertEqual(sample.default.quietHours?.tz, "Europe/Paris")
        XCTAssertEqual(sample.clusters["0f1e2d3c4b5a6978"]?.digest, true)
        XCTAssertEqual(sample.clusters["9988776655443322"]?.quietHours?.isOff, true)
    }

    func testUnreadableStoredRulesAreNone() {
        XCTAssertEqual(AlertRules.decode(Data(#"{"default":{"quietHours":5}}"#.utf8)), .none)
        XCTAssertEqual(AlertRules.decode(nil), .none)
    }

    func testGoAnswerDecodes() throws {
        let answer = """
            {"decisions":[{"cluster":"fp","key":"am:x","action":"mute","reason":"quiet","until":1700000000000}],
            "digests":[{"cluster":"fp","count":3,"critical":1,"warning":2,"firstKeys":["am:x"]}]}
            """
        let decoded = try TalosJSON.decode(AlertDecisions.self, from: answer)
        XCTAssertEqual(
            decoded.decisions,
            [
                AlertDecision(
                    cluster: "fp", key: "am:x", action: alertActionMute, reason: alertReasonQuiet,
                    until: 1_700_000_000_000)
            ])
        XCTAssertEqual(
            decoded.digests, [AlertDigest(cluster: "fp", count: 3, critical: 1, warning: 2, firstKeys: ["am:x"])])
    }

    func testThePhoneZoneFillsTheWindowsWithoutOne() {
        var withNY = rules
        withNY.clusters["fp-ny"] = AlertRule(
            quietHours: QuietHours(start: "23:00", end: "07:00", tz: "America/New_York"))
        let zoned = withNY.withZone("Europe/Paris")
        XCTAssertEqual(zoned.default.quietHours?.tz, "Europe/Paris")
        XCTAssertEqual(zoned.clusters["fp-ny"]?.quietHours?.tz, "America/New_York")
        XCTAssertEqual(zoned.clusters["fp-lab"]?.quietHours?.tz, "Europe/Paris")
        XCTAssertNil(zoned.clusters["fp-staging"]?.quietHours)
    }

    func testGoGetsThePhoneZoneAndTheAlertsAsItReadsThem() throws {
        let alerts = [
            AlertInput(cluster: "fp", key: "node:10.0.0.2", kind: "node", problem: true),
            AlertInput(cluster: "fp", key: "am:x", kind: "am", severity: "warning", problem: true, snoozed: true),
        ]
        let rules = AlertRules(default: AlertRule(quietHours: night))
        let request = try alertRulesRequest(rules: rules, alerts: alerts, zone: "Europe/Paris")
        XCTAssertEqual(
            request.rules,
            #"{"default":{"quietHours":{"end":"07:00","exceptCritical":true,"start":"23:00","tz":"Europe/Paris"}}}"#)
        let sent = try TalosJSON.decode([AlertInput].self, from: request.alerts)
        XCTAssertEqual(sent, alerts)
    }

    func testAClusterOverrideIsSetDroppedWhenEmptyAndKept() {
        let set = AlertRules.none.withCluster("fp", AlertRule(minSeverity: alertSeverityCritical))
        XCTAssertEqual(set.clusterRule("fp"), AlertRule(minSeverity: alertSeverityCritical))
        XCTAssertEqual(set.withCluster("fp", AlertRule()), .none)
        XCTAssertEqual(Set(rules.keeping(["fp-lab", "fp-other"]).clusters.keys), ["fp-lab"])
    }

    func testABackupCarriesTheRulesOfItsClustersOnly() throws {
        XCTAssertNil(AlertRules.none.forBackup(fingerprints: ["fp"]))
        XCTAssertEqual(rules.forBackup(fingerprints: ["fp-staging"]).map { Set($0.clusters.keys) }, ["fp-staging"])
        var settings = BackupSettings(themeMode: "dark")
        settings.alertRules = rules
        let read = try JSONDecoder().decode(BackupSettings.self, from: JSONEncoder().encode(settings))
        XCTAssertEqual(read.alertRules, rules)
        // An older backup has none: the device keeps its own.
        let older = try JSONDecoder().decode(BackupSettings.self, from: Data(#"{"themeMode":"dark"}"#.utf8))
        XCTAssertNil(older.alertRules)
    }

    func testClockTextAndBack() {
        XCTAssertEqual(quietClockText(hour: 7, minute: 5), "07:05")
        XCTAssertEqual(quietClock("23:30").hour, 23)
        XCTAssertEqual(quietClock("23:30").minute, 30)
        XCTAssertEqual(quietClock("7").hour, 0)
        XCTAssertEqual(quietClock("25:00").hour, 0)
    }
}
