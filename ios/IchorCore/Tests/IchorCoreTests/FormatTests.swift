import XCTest
@testable import IchorCore

final class FormatTests: XCTestCase {
    func testBytes() {
        XCTAssertEqual(formatBytes(0), "0 B")
        XCTAssertEqual(formatBytes(1023), "1023 B")
        XCTAssertEqual(formatBytes(1536), "1.5 KiB")
        XCTAssertEqual(formatBytes(UInt64(33_347_887_104)), "31.1 GiB")
        XCTAssertEqual(formatBytes(UInt64(509_534_134_272)), "474.5 GiB")
    }

    func testDuration() {
        XCTAssertEqual(formatDuration(59), "<1m")
        XCTAssertEqual(formatDuration(42 * 60), "42m")
        XCTAssertEqual(formatDuration(5 * 3600 + 12 * 60), "5h 12m")
        XCTAssertEqual(formatDuration(3 * 86_400 + 4 * 3600 + 59), "3d 4h")
    }

    func testFraction() {
        XCTAssertEqual(usedFraction(total: 0, available: 0), 0)
        XCTAssertEqual(usedFraction(total: 100, available: 40), 0.6, accuracy: 0.0001)
        XCTAssertEqual(usedFraction(total: 100, available: 500), 0)
    }

    func testDaysUntil() {
        let now = Date(timeIntervalSince1970: 1_000_000_000)
        XCTAssertEqual(daysUntil(1_000_000_000 + 10 * 86_400, now: now), 10)
        XCTAssertEqual(daysUntil(1_000_000_000 - 60, now: now), -1)
    }
}
