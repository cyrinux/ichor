import XCTest
@testable import IchorCore

final class ActionOutcomeTests: XCTestCase {
    func testFailureSetsTheMessageOnly() {
        var message: String?
        var succeeded = 2
        XCTAssertFalse(recordActionOutcome("denied", message: &message, succeeded: &succeeded))
        XCTAssertEqual(message, "denied")
        XCTAssertEqual(succeeded, 2)
    }

    func testSuccessCountsAndKeepsTheMessage() {
        var message: String? = "earlier"
        var succeeded = 0
        XCTAssertTrue(recordActionOutcome(nil, message: &message, succeeded: &succeeded))
        XCTAssertEqual(message, "earlier")
        XCTAssertEqual(succeeded, 1)
    }
}
