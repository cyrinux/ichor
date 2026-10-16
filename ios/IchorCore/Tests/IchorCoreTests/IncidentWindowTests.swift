import XCTest
@testable import IchorCore

final class IncidentWindowTests: XCTestCase {
    private let now: Int64 = 1_800_000_000_000

    func testWindowsInOrder() {
        XCTAssertEqual(IncidentWindow.allCases, [.sixHours, .day, .week])
        XCTAssertEqual(IncidentWindow.allCases.map(\.hours), [6, 24, 168])
    }

    func testSinceMillis() {
        XCTAssertEqual(IncidentWindow.sixHours.since(now: now), now - 6 * 3_600_000)
        XCTAssertEqual(IncidentWindow.day.since(now: now), now - 86_400_000)
        XCTAssertEqual(IncidentWindow.week.since(now: now), now - 7 * 86_400_000)
    }

    func testSinceNeverBeforeEpoch() {
        XCTAssertEqual(IncidentWindow.week.since(now: 1_000), 0)
    }

    func testStandardIsADay() {
        XCTAssertEqual(IncidentWindow.standard, .day)
    }

    func testIdsAreUnique() {
        XCTAssertEqual(Set(IncidentWindow.allCases.map(\.id)).count, IncidentWindow.allCases.count)
    }
}
