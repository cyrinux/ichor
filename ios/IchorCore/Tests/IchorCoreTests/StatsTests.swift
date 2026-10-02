import XCTest
@testable import IchorCore

final class StatsTests: XCTestCase {
    private func sample(_ at: Int64, busy: Double, total: Double, rx: UInt64, read: UInt64) -> NodeStats {
        NodeStats(at: at, cpuBusy: busy, cpuTotal: total, cpuCount: 8, memTotal: 1000, memAvailable: 250,
                  load1: 1.5, netRx: rx, netTx: rx / 2, diskRead: read, diskWrite: read * 2)
    }

    func testRatesOverTwoSeconds() throws {
        let p = try XCTUnwrap(ratesBetween(sample(0, busy: 10, total: 100, rx: 1_000, read: 0),
                                           sample(2_000, busy: 30, total: 180, rx: 5_000, read: 4_096)))
        XCTAssertEqual(p.cpuPercent, 25, accuracy: 0.001)
        XCTAssertEqual(p.memPercent, 75, accuracy: 0.001)
        XCTAssertEqual(p.rxPerSec, 2_000, accuracy: 0.001)
        XCTAssertEqual(p.writePerSec, 4_096, accuracy: 0.001)
    }

    func testCounterResetGivesZero() throws {
        let p = try XCTUnwrap(ratesBetween(sample(0, busy: 500, total: 900, rx: 9_000, read: 9_000),
                                           sample(2_000, busy: 5, total: 10, rx: 100, read: 100)))
        XCTAssertEqual(p.rxPerSec, 0)
        XCTAssertEqual(p.cpuPercent, 0)
    }

    func testNoElapsedTime() {
        XCTAssertNil(ratesBetween(sample(5, busy: 1, total: 2, rx: 1, read: 1), sample(5, busy: 1, total: 2, rx: 1, read: 1)))
    }

    func testSupportPromptTiming() {
        let start = Date(timeIntervalSince1970: 1_000_000_000)
        let day: TimeInterval = 86_400
        XCTAssertFalse(SupportState(firstSeen: start, launches: 20).shouldAsk(now: start.addingTimeInterval(13 * day)))
        XCTAssertFalse(SupportState(firstSeen: start, launches: 9).shouldAsk(now: start.addingTimeInterval(30 * day)))
        XCTAssertTrue(SupportState(firstSeen: start, launches: 20).shouldAsk(now: start.addingTimeInterval(14 * day)))
        let asked = SupportState(firstSeen: start, launches: 20, lastAsked: start.addingTimeInterval(20 * day))
        XCTAssertFalse(asked.shouldAsk(now: start.addingTimeInterval(100 * day)))
        XCTAssertTrue(asked.shouldAsk(now: start.addingTimeInterval(110 * day)))
        XCTAssertFalse(SupportState(firstSeen: start, launches: 99, never: true).shouldAsk(now: start.addingTimeInterval(999 * day)))
    }

    func testAccessLabel() {
        XCTAssertEqual(ContextSummary(name: "a", roles: ["os:admin"]).accessLabel, "admin")
        XCTAssertEqual(ContextSummary(name: "o", roles: ["os:operator"]).accessLabel, "operator")
        XCTAssertEqual(ContextSummary(name: "r", roles: ["os:reader"]).accessLabel, "read-only")
    }
}
