import XCTest
@testable import IchorCore

final class NodeFilesTests: XCTestCase {
    func testCrumbsGoFromTheRootToTheDirectory() {
        XCTAssertEqual(
            NodePaths.crumbs("/var/log"),
            [
                NodePathCrumb(name: "/", path: "/"), NodePathCrumb(name: "var", path: "/var"),
                NodePathCrumb(name: "log", path: "/var/log"),
            ])
        XCTAssertEqual(NodePaths.crumbs("/"), [NodePathCrumb(name: "/", path: "/")])
    }

    func testATypedPathMustBeAbsoluteWithoutClimbing() {
        XCTAssertEqual(NodePaths.typed("  /etc/resolv.conf "), "/etc/resolv.conf")
        XCTAssertNil(NodePaths.typed("etc/hosts"))
        XCTAssertNil(NodePaths.typed("/var/log/../../etc"))
        XCTAssertNil(NodePaths.typed(""))
        XCTAssertEqual(NodePaths.typed("/run/..data"), "/run/..data")
    }

    func testEntriesDecodeWithTheirKindsAndDates() throws {
        let json = """
            {"dir":"/var/log","roots":["/var/log"],"entries":[
              {"name":"pods","path":"/var/log/pods","type":"dir","size":0,
               "modified":"2026-01-02T03:04:05Z","target":""},
              {"name":"cur","path":"/var/log/cur","type":"link","size":0,"modified":"","target":"/x"}
            ],"truncated":true}
            """
        let listing = try TalosJSON.decode(NodeDirListing.self, from: json)
        XCTAssertTrue(listing.truncated)
        XCTAssertTrue(listing.entries[0].isDir)
        XCTAssertEqual(listing.entries[0].modifiedDate, Date(timeIntervalSince1970: 1_767_323_045))
        XCTAssertTrue(listing.entries[1].isLink)
        XCTAssertNil(listing.entries[1].modifiedDate)

        let file = try TalosJSON.decode(NodeFileText.self, from: #"{"path":"/var/log/lastlog","binary":true}"#)
        XCTAssertTrue(file.binary)
        XCTAssertEqual(file.text, "")
    }
}
