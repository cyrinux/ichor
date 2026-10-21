import XCTest
@testable import IchorCore

final class OrientationPolicyTests: XCTestCase {
    func testAnIPhoneHoldsPortrait() {
        XCTAssertEqual(OrientationPolicy.allowed(landscapeHeld: false, pad: false), .portrait)
    }

    func testAWideScreenUnlocksLandscapeOnAnIPhone() {
        XCTAssertEqual(OrientationPolicy.allowed(landscapeHeld: true, pad: false), .allButUpsideDown)
    }

    func testAnIPadAlwaysRotates() {
        XCTAssertEqual(OrientationPolicy.allowed(landscapeHeld: false, pad: true), .all)
        XCTAssertEqual(OrientationPolicy.allowed(landscapeHeld: true, pad: true), .all)
    }

    func testAChartKeepsItsHeightInPortrait() {
        XCTAssertEqual(OrientationPolicy.chartHeight(min: 140, compactHeight: false, available: 700), 140)
    }

    func testAChartGrowsInLandscape() {
        XCTAssertEqual(OrientationPolicy.chartHeight(min: 140, compactHeight: true, available: 390), 176)
    }

    func testAChartNeverShrinksUnderItsMinimum() {
        XCTAssertEqual(OrientationPolicy.chartHeight(min: 140, compactHeight: true, available: 250), 140)
    }
}
