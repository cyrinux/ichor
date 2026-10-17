import XCTest
@testable import IchorCore

final class IncidentTimelineTests: XCTestCase {
    private func entry(_ id: String, node: String = "10.0.0.2", kind: String = "service",
                       detail: String = #"{"state":"Running","health":"healthy"}"#,
                       severity: String = "info", at: Int64 = 0) -> IncidentEntry {
        IncidentEntry(id: id, at: at, node: node, kind: kind, subject: "udevd", detail: detail,
                      severity: severity, metrics: nil, metricsOmitted: nil)
    }

    func testHealthCriticalForUnreachableAndErrors() {
        XCTAssertEqual(entry("a", kind: "status", detail: #"{"ready":false,"reachable":false}"#).timelineHealth, .critical)
        XCTAssertEqual(entry("b", kind: "error", detail: "").timelineHealth, .critical)
        XCTAssertEqual(entry("c", severity: "error").timelineHealth, .critical)
    }

    func testHealthWarningForChangedCondition() {
        XCTAssertEqual(entry("a", kind: "status", detail: #"{"ready":false,"reachable":true}"#).timelineHealth, .warning)
        XCTAssertEqual(entry("b", kind: "link", detail: #"{"before":"up","after":"down"}"#).timelineHealth, .warning)
        XCTAssertEqual(entry("c", kind: "event", detail: "", severity: "warning").timelineHealth, .warning)
        XCTAssertEqual(entry("d", detail: #"{"state":"Running","health":"unhealthy"}"#).timelineHealth, .warning)
        XCTAssertEqual(entry("e", detail: #"{"state":"absent"}"#).timelineHealth, .warning)
        XCTAssertEqual(entry("f", kind: "status", detail: #"{"ready":true,"reachable":true,"conditions":[{"name":"x","reason":"y"}]}"#).timelineHealth, .warning)
    }

    func testHealthOkOtherwise() {
        XCTAssertEqual(entry("a").timelineHealth, .ok)
        XCTAssertEqual(entry("b", kind: "status", detail: #"{"ready":true,"reachable":true,"conditions":[]}"#).timelineHealth, .ok)
        XCTAssertEqual(entry("c", kind: "event", detail: "{…").timelineHealth, .ok)
    }

    func testConsecutiveHealthyServicesOfOneNodeCollapse() {
        let items = incidentTimeline([entry("1"), entry("2"), entry("3")])
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items.first?.entries.map(\.id), ["1", "2", "3"])
        XCTAssertEqual(items.first?.id, "run/1")
    }

    func testSingleHealthyRowStaysAnEntry() {
        XCTAssertEqual(incidentTimeline([entry("1")]), [.entry(entry("1"))])
    }

    func testHealthyStatusRowsDoNotCollapse() {
        let ready = #"{"ready":true,"reachable":true}"#
        let items = incidentTimeline([entry("1", kind: "status", detail: ready), entry("2", kind: "status", detail: ready)])
        XCTAssertEqual(items.map(\.id), ["1", "2"])
    }

    func testRunsBreakOnNodeAndProblems() {
        let bad = entry("x", kind: "status", detail: #"{"reachable":false}"#)
        let items = incidentTimeline([
            entry("1"), entry("2"),
            entry("3", node: "10.0.0.3"),
            bad,
            entry("4", node: "10.0.0.3"), entry("5", node: "10.0.0.3"),
            entry("6", node: "10.0.0.3", detail: #"{"state":"Running","health":"unhealthy"}"#),
        ])
        XCTAssertEqual(items.map(\.id), ["run/1", "3", "x", "run/4", "6"])
    }

    func testEmptyTimeline() {
        XCTAssertEqual(incidentTimeline([]), [])
    }

    func testOffsetFromStart() {
        XCTAssertEqual(incidentOffset(at: 7_000, startedAt: 0), "+00:07")
        XCTAssertEqual(incidentOffset(at: 1_000 + 750_999, startedAt: 1_000), "+12:30")
        XCTAssertEqual(incidentOffset(at: 3_723_000, startedAt: 0), "+1:02:03")
    }

    func testOffsetBeforeStartIsNegative() {
        XCTAssertEqual(incidentOffset(at: 8_000, startedAt: 10_000), "-00:02")
    }
}
