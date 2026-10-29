import XCTest
@testable import IchorCore

final class StartupTraceTests: XCTestCase {
    func testLineHasAbsoluteAndDeltaMillis() {
        XCTAssertEqual(
            StartupTrace.line(step: "config parsed 23 contexts", sinceStart: 3120, delta: 410),
            "config parsed 23 contexts +3120ms (Δ410ms)")
    }

    func testGoStepsKeepCountsOnly() {
        XCTAssertEqual(
            StartupTrace.goStep(
                json: #"{"step":"overview.all_nodes","ms":8003,"at":1791750233034,"count":8,"unreachable":1}"#),
            "go:overview.all_nodes 8003ms count=8 unreachable=1")
        XCTAssertEqual(
            StartupTrace.goStep(json: #"{"step":"config.parse","ms":12,"at":1,"count":23}"#),
            "go:config.parse 12ms count=23")
        XCTAssertEqual(StartupTrace.goStep(json: #"{"step":"session.reused","ms":0,"at":1}"#), "go:session.reused 0ms")
    }

    func testAnUnexpectedGoStepIsKeptAsItCame() {
        XCTAssertEqual(StartupTrace.goStep(json: "not json"), "go:not json")
    }

    func testCallStep() {
        XCTAssertEqual(StartupTrace.callStep(name: "overview()", ms: 812), "call overview() 812ms")
    }

    func testAppStepsReachTheRingWithTheTraceOff() {
        let seen = StepBox()
        StartupTrace.setEnabled(false)
        StartupTrace.setRingSink { step, sinceStart, delta in seen.add(step, sinceStart >= 0 && delta >= 0) }
        defer { StartupTrace.setRingSink(nil) }
        StartupTrace.mark("app.init start")
        StartupTrace.markOnce("ring-test once")
        StartupTrace.markOnce("ring-test once")
        StartupTrace.markOnce("ring-test detail", detail: "3 contexts")
        // The core writes its own steps to the ring.
        StartupTrace.markGo("go:config.parse 12ms")
        XCTAssertEqual(seen.steps, ["app.init start", "ring-test once", "ring-test detail 3 contexts"])
        XCTAssertTrue(seen.allTimed)
    }

    func testOffCostsNoSpan() {
        StartupTrace.setEnabled(false)
        XCTAssertNil(StartupTrace.begin("overview()"))
        XCTAssertFalse(StartupTrace.isEnabled)
    }
}

/// The steps a test's ring sink received, behind a lock.
private final class StepBox: @unchecked Sendable {
    private let lock = NSLock()
    private var received: [String] = []
    private var timed = true

    func add(_ step: String, _ ok: Bool) {
        lock.lock()
        defer { lock.unlock() }
        received.append(step)
        timed = timed && ok
    }

    var steps: [String] {
        lock.lock()
        defer { lock.unlock() }
        return received
    }

    var allTimed: Bool {
        lock.lock()
        defer { lock.unlock() }
        return timed
    }
}
