import XCTest
@testable import IchorCore

final class StartupTraceTests: XCTestCase {
    func testLineHasAbsoluteAndDeltaMillis() {
        XCTAssertEqual(StartupTrace.line(step: "config parsed 23 contexts", sinceStart: 3120, delta: 410),
                       "config parsed 23 contexts +3120ms (Δ410ms)")
    }

    func testGoStepsKeepCountsOnly() {
        XCTAssertEqual(StartupTrace.goStep(json: #"{"step":"overview.all_nodes","ms":8003,"at":1791750233034,"count":8,"unreachable":1}"#),
                       "go:overview.all_nodes 8003ms count=8 unreachable=1")
        XCTAssertEqual(StartupTrace.goStep(json: #"{"step":"config.parse","ms":12,"at":1,"count":23}"#), "go:config.parse 12ms count=23")
        XCTAssertEqual(StartupTrace.goStep(json: #"{"step":"session.reused","ms":0,"at":1}"#), "go:session.reused 0ms")
    }

    func testAnUnexpectedGoStepIsKeptAsItCame() {
        XCTAssertEqual(StartupTrace.goStep(json: "not json"), "go:not json")
    }

    func testCallStep() {
        XCTAssertEqual(StartupTrace.callStep(name: "overview()", ms: 812), "call overview() 812ms")
    }

    func testOffCostsNoSpan() {
        StartupTrace.setEnabled(false)
        XCTAssertNil(StartupTrace.begin("overview()"))
        XCTAssertFalse(StartupTrace.isEnabled)
    }
}
