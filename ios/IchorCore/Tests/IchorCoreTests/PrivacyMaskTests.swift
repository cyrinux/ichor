import XCTest
@testable import IchorCore

final class PrivacyMaskTests: XCTestCase {
    func testNormalizedMaskWords() {
        XCTAssertEqual(normalizedMaskWords(" acme.com , prod-eu,,acme.com, "), "acme.com,prod-eu")
        XCTAssertEqual(normalizedMaskWords(""), "")
        XCTAssertEqual(normalizedMaskWords(" , ,"), "")
        XCTAssertEqual(normalizedMaskWords("Acme,acme"), "Acme,acme")
    }
}
