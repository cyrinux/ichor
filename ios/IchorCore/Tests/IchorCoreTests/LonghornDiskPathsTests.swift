import XCTest
@testable import IchorCore

final class LonghornDiskPathsTests: XCTestCase {
    private func node(_ name: String, _ paths: [String]) throws -> LonghornNode {
        let disks = paths.map { #"{"path":"\#($0)","schedulable":true,"maximum":100,"scheduled":8}"# }.joined(separator: ",")
        let raw = #"{"name":"\#(name)","namespace":"storage","ready":true,"disks":[\#(disks)]}"#
        return try JSONDecoder().decode(LonghornNode.self, from: Data(raw.utf8))
    }

    func testOnePathOnEveryNodeIsShared() throws {
        let nodes = try ["n1", "n2", "n3"].map { try node($0, ["/var/mnt/storage/longhorn"]) }
        XCTAssertEqual(longhornSharedDiskPath(nodes), "/var/mnt/storage/longhorn")
    }

    func testDifferentPathsAreNotShared() throws {
        let nodes = [try node("n1", ["/var/lib/longhorn"]), try node("n2", ["/var/lib/longhorn", "/mnt/fast"])]
        XCTAssertNil(longhornSharedDiskPath(nodes))
    }

    func testNoDiskOrEmptyPathsShareNothing() throws {
        XCTAssertNil(longhornSharedDiskPath([]))
        XCTAssertNil(longhornSharedDiskPath([try node("n1", [])]))
        XCTAssertNil(longhornSharedDiskPath([try node("n1", [""])]))
        // A disk without a path does not hide the one the others share.
        XCTAssertEqual(longhornSharedDiskPath([try node("n1", [""]), try node("n2", ["/data"])]), "/data")
    }

    func testDiskLabelIsTheLastSegment() {
        XCTAssertEqual(longhornDiskLabel("/var/mnt/storage/longhorn"), "longhorn")
        XCTAssertEqual(longhornDiskLabel("/var/lib/longhorn/"), "longhorn")
        XCTAssertEqual(longhornDiskLabel("data"), "data")
        XCTAssertEqual(longhornDiskLabel("/"), "/")
        XCTAssertEqual(longhornDiskLabel(""), "")
    }
}
