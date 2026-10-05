import XCTest
@testable import IchorCore

final class OpenSourceLicensesTests: XCTestCase {
    func testEveryLibraryHasALicenseAndAWebsite() {
        XCTAssertFalse(openSourceLibraries.isEmpty)
        XCTAssertEqual(Set(openSourceLibraries.map(\.id)).count, openSourceLibraries.count)
        for library in openSourceLibraries {
            XCTAssertFalse(library.licenses.isEmpty, library.name)
            XCTAssertTrue(library.website.hasPrefix("https://"), library.name)
        }
    }

    func testLicensesLinkToSpdx() {
        XCTAssertEqual(licenseURL("MPL-2.0"), "https://spdx.org/licenses/MPL-2.0.html")
    }
}
