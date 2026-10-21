import XCTest
@testable import IchorCore

final class OverviewColumnsTests: XCTestCase {
    func testAFullWidthIPadShowsTwoColumns() {
        XCTAssertEqual(OverviewColumns.count(width: 1366, regularPad: true), 2)
        XCTAssertEqual(OverviewColumns.count(width: 900, regularPad: true), 2)
    }

    func testAHalfScreenSplitViewShowsOne() {
        XCTAssertEqual(OverviewColumns.count(width: 678, regularPad: true), 1)
        XCTAssertEqual(OverviewColumns.count(width: 899, regularPad: true), 1)
    }

    func testAnIPhoneShowsOne() {
        XCTAssertEqual(OverviewColumns.count(width: 932, regularPad: false), 1)
    }

    func testTheSummaryHeadsTheLeadingColumn() {
        let split = OverviewColumns.split([.pinned, .summary, .nodes, .apps, .alerts])
        XCTAssertEqual(split.leading, [.summary, .nodes, .alerts])
        XCTAssertEqual(split.trailing, [.pinned, .apps])
    }

    func testWithoutTheSummaryTheCardsAlternateLeadingFirst() {
        let split = OverviewColumns.split([.nodes, .apps, .alerts])
        XCTAssertEqual(split.leading, [.nodes, .alerts])
        XCTAssertEqual(split.trailing, [.apps])
    }

    func testEveryCardIsShownOnce() {
        let cards = OverviewCard.allCases
        let split = OverviewColumns.split(cards)
        XCTAssertEqual(Set(split.leading + split.trailing), Set(cards))
        XCTAssertEqual(split.leading.count + split.trailing.count, cards.count)
    }
}
