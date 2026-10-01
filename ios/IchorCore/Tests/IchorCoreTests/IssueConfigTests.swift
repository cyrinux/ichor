import XCTest
@testable import IchorCore

final class IssueConfigTests: XCTestCase {
    func testRolesArgumentAndPresets() {
        XCTAssertEqual(rolesArgument(["os:reader", "os:admin", "bogus"]), "os:admin,os:reader")
        XCTAssertNil(rolesArgument(["bogus"]))
        XCTAssertNil(rolesArgument([String]()))
        XCTAssertEqual(rolesArgument(defaultIssuedRoles), "os:reader")
        XCTAssertEqual(ConfigValidity.default.hours, 8_760)
        XCTAssertEqual(ConfigValidity.allCases.map(\.hours), [720, 2_160, 8_760])
        XCTAssertEqual(issuedFileName(context: "lab", roles: "os:reader"), "talosconfig-lab-reader.yaml")
        XCTAssertEqual(issuedFileName(context: "my lab/1", roles: "os:admin,os:etcd:backup"), "talosconfig-my_lab_1-admin-etcd-backup.yaml")
        XCTAssertEqual(issuedFileName(context: "", roles: ""), "talosconfig-cluster.yaml")
    }

    func testQRCapacity() {
        XCTAssertTrue(fitsInQRCode(String(repeating: "a", count: maxQRPayloadBytes)))
        XCTAssertFalse(fitsInQRCode(String(repeating: "a", count: maxQRPayloadBytes + 1)))
        XCTAssertFalse(fitsInQRCode(String(repeating: "é", count: 1_500)))
    }

    func testIssueConfigIsAdminOnly() {
        XCTAssertEqual(Feature.issueConfig.roles, ["os:admin"])
        XCTAssertFalse(ContextSummary(name: "x", roles: ["os:operator"]).allows(.issueConfig))
        XCTAssertTrue(ContextSummary(name: "x", roles: ["os:admin"]).allows(.issueConfig))
    }
}
