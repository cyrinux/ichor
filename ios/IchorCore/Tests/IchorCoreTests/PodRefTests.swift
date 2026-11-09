import XCTest
@testable import IchorCore

final class PodRefTests: XCTestCase {
    func testSplitsOnTheFirstSlash() {
        XCTAssertEqual(PodRef(path: "shop/cart-7d9f-x2k"), PodRef(namespace: "shop", name: "cart-7d9f-x2k"))
    }

    func testAValueWithoutASlashIsNoPod() {
        XCTAssertNil(PodRef(path: "cart-7d9f-x2k"))
        XCTAssertNil(PodRef(path: ""))
    }

    func testAnEmptySideIsNoPod() {
        XCTAssertNil(PodRef(path: "shop/"))
        XCTAssertNil(PodRef(path: "/cart-7d9f-x2k"))
        XCTAssertNil(PodRef(path: "/"))
    }

    func testANameWithAnotherSlashIsNoPod() {
        XCTAssertNil(PodRef(path: "shop/cart/extra"))
    }
}
