import XCTest
@testable import IchorCore

final class UpgradeExtensionsTests: XCTestCase {
    func testDecoding() throws {
        let check = try TalosJSON.decode(UpgradeExtensionCheck.self, from: """
        {"schematic":"abc","targetVersion":"v1.12.0","installed":[{"name":"iscsi-tools","version":"v0.2.0"}],
         "missing":["iscsi-tools"],"unknown":false,"error":""}
        """)
        XCTAssertEqual(check.missing, ["iscsi-tools"])
        XCTAssertEqual(check.installed.first?.version, "v0.2.0")
        XCTAssertFalse(check.unknown)

        let unknown = try TalosJSON.decode(UpgradeExtensionCheck.self, from: #"{"unknown":true,"missing":null}"#)
        XCTAssertTrue(unknown.unknown)
        XCTAssertEqual(unknown.missing, [])
    }
}
