import XCTest
@testable import IchorCore

final class OpenSourceLicensesTests: XCTestCase {
    func testDecodesTheGeneratedFile() {
        let json = """
        {"notice": "Ichor\\nCopyright", "libraries": [
          {"name": "github.com/siderolabs/talos/pkg/machinery", "version": "v1.14.2",
           "website": "https://pkg.go.dev/github.com/siderolabs/talos/pkg/machinery",
           "licenses": ["MPL-2.0"], "text": "Mozilla Public License Version 2.0"},
          {"name": "SwiftTerm", "version": "", "website": "", "licenses": ["MIT"], "text": "MIT"}
        ]}
        """
        let licenses = decodeOpenSourceLicenses(Data(json.utf8))
        XCTAssertEqual(licenses.notice, "Ichor\nCopyright")
        XCTAssertEqual(licenses.libraries.map(\.summary), ["v1.14.2 · MPL-2.0", "MIT"])
        XCTAssertEqual(licenses.libraries.first?.id, "github.com/siderolabs/talos/pkg/machinery")
    }

    func testMissingOrCorruptFileIsEmpty() {
        XCTAssertEqual(decodeOpenSourceLicenses(nil), OpenSourceLicenses())
        XCTAssertEqual(decodeOpenSourceLicenses(Data("{".utf8)), OpenSourceLicenses())
    }
}
