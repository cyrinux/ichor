import XCTest
@testable import IchorCore

final class DriftDiffTests: XCTestCase {
    func testDecodesItemisedChange() throws {
        let json =
            #"{"node":"192.0.2.21","reference":"192.0.2.20","nodeName":"demo-worker-2","referenceName":"demo-worker-1","key":"extensions","before":"a=1, b=2","after":"a=1, c=3","added":["btrfs=v1.14.1"],"removed":["qemu-guest-agent=11.1.1"],"changed":[{"name":"schematic","before":"0123abcd0123abcd0123abcd","after":"4567cdef4567cdef4567cdef"}]}"#
        let change = try TalosJSON.decode(DriftChange.self, from: json)
        XCTAssertEqual(change.nodeName, "demo-worker-2")
        XCTAssertEqual(change.referenceName, "demo-worker-1")
        XCTAssertEqual(change.added, ["btrfs=v1.14.1"])
        XCTAssertEqual(change.removed, ["qemu-guest-agent=11.1.1"])
        XCTAssertEqual(
            change.changed,
            [DriftPair(name: "schematic", before: "0123abcd0123abcd0123abcd", after: "4567cdef4567cdef4567cdef")])
        XCTAssertEqual(change.itemCount, 3)
        XCTAssertTrue(change.isItemised)
        XCTAssertEqual(change.before, "a=1, b=2")
    }

    func testOlderCoreWithoutNewKeysFallsBack() throws {
        let change = try TalosJSON.decode(
            DriftChange.self,
            from: #"{"node":"192.0.2.21","reference":"192.0.2.20","key":"secureBoot","before":"true","after":"false"}"#)
        XCTAssertEqual(change.nodeName, "")
        XCTAssertEqual(change.referenceName, "")
        XCTAssertEqual(change.added, [])
        XCTAssertEqual(change.removed, [])
        XCTAssertEqual(change.changed, [])
        XCTAssertFalse(change.isItemised)
        XCTAssertEqual(change.after, "false")
    }

    func testNullListsDecodeAsEmpty() throws {
        let change = try TalosJSON.decode(
            DriftChange.self,
            from:
                #"{"node":"192.0.2.21","reference":"192.0.2.20","nodeName":"demo-worker-2","referenceName":"demo-worker-1","key":"uki","before":"true","after":"false","added":null,"removed":null,"changed":null}"#
        )
        XCTAssertEqual(change.itemCount, 0)
        XCTAssertEqual(change.nodeName, "demo-worker-2")
    }

    func testRoundTrip() throws {
        let change = DriftChange(
            node: "192.0.2.21", reference: "192.0.2.20", key: "mtu", before: "ens3=1500", after: "enp1s0=1500",
            nodeName: "demo-worker-2", added: ["enp1s0=1500"], removed: ["ens3=1500"])
        let decoded = try JSONDecoder().decode(DriftChange.self, from: JSONEncoder().encode(change))
        XCTAssertEqual(decoded, change)
    }

    func testShortDriftValue() {
        XCTAssertEqual(shortDriftValue("2.41.2"), "2.41.2")
        XCTAssertEqual(shortDriftValue("0123abcd0123abcd"), "0123abcd0123abcd")
        XCTAssertEqual(shortDriftValue("0123abcd0123abcd0"), "0123abcd…")
        XCTAssertEqual(shortDriftValue(""), "")
    }
}
