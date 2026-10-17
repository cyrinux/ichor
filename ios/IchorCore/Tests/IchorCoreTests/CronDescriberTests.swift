import XCTest
@testable import IchorCore

final class CronDescriberTests: XCTestCase {
    func testDaily() {
        XCTAssertEqual(CronDescriber.describe("0 9 * * *"), .daily(hour: 9, minute: 0))
        XCTAssertEqual(CronDescriber.describe("  30 23 * * * "), .daily(hour: 23, minute: 30))
        XCTAssertEqual(CronDescriber.describe("@daily"), .daily(hour: 0, minute: 0))
        XCTAssertEqual(CronDescriber.describe("@midnight"), .daily(hour: 0, minute: 0))
    }

    func testWeeklyCountsSundayAsZeroOrSeven() {
        XCTAssertEqual(CronDescriber.describe("30 2 * * 1"), .weekly(weekday: 2, hour: 2, minute: 30))
        XCTAssertEqual(CronDescriber.describe("0 4 * * 0"), .weekly(weekday: 1, hour: 4, minute: 0))
        XCTAssertEqual(CronDescriber.describe("0 4 * * 7"), .weekly(weekday: 1, hour: 4, minute: 0))
        XCTAssertEqual(CronDescriber.describe("0 4 * * SAT"), .weekly(weekday: 7, hour: 4, minute: 0))
        XCTAssertEqual(CronDescriber.describe("@weekly"), .weekly(weekday: 1, hour: 0, minute: 0))
    }

    func testMonthly() {
        XCTAssertEqual(CronDescriber.describe("0 9 1 * *"), .monthly(day: 1, hour: 9, minute: 0))
        XCTAssertEqual(CronDescriber.describe("15 3 28 * *"), .monthly(day: 28, hour: 3, minute: 15))
        XCTAssertEqual(CronDescriber.describe("@monthly"), .monthly(day: 1, hour: 0, minute: 0))
    }

    func testAnythingElseStaysRaw() {
        for schedule in ["*/15 * * * *", "0 */6 * * *", "0 9 * * 1-5", "0 9 1,15 * *", "0 9 1 1 *", "0 9 1 * 1",
                         "0 24 * * *", "60 1 * * *", "0 9 32 * *", "0 9 * * 8", "+5 9 * * *", "@hourly",
                         "CRON_TZ=UTC 0 9 * * *", "0 9 * *", ""] {
            XCTAssertEqual(CronDescriber.describe(schedule), .raw(schedule), schedule)
        }
    }

    func testClockIsZeroPadded() {
        XCTAssertEqual(CronDescription.clock(hour: 9, minute: 5), "09:05")
        XCTAssertEqual(CronDescription.clock(hour: 23, minute: 30), "23:30")
    }
}
