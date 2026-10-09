import XCTest
@testable import IchorCore

final class KubeWatchTests: XCTestCase {
    private func key(_ row: String) -> String { String(row.prefix(while: { $0 != ":" })) }

    func testAppliesChangesInPlaceAndNewRowsLast() {
        let rows = ["a:1", "b:1"]
        XCTAssertEqual(rows.applying(.added("c:1"), key: key), ["a:1", "b:1", "c:1"])
        XCTAssertEqual(rows.applying(.modified("a:2"), key: key), ["a:2", "b:1"])
        // A change to a row the list did not have yet (added while the list loaded) joins it.
        XCTAssertEqual(rows.applying(.modified("d:1"), key: key), ["a:1", "b:1", "d:1"])
        XCTAssertEqual(rows.applying(.deleted("a:9"), key: key), ["b:1"])
        XCTAssertEqual(rows.applying(.sync(["z:1"]), key: key), ["z:1"])
    }

    func testASyncMakesTheLoadCompleteAndAChangeKeepsItsPages() {
        let load = PagedLoad(items: ["a:1"], continueToken: "t", remaining: 5, pages: 1, done: false, capped: true)
        let synced = load.applying(.sync(["a:1", "b:1"]), key: key)
        XCTAssertTrue(synced.done)
        XCTAssertEqual(synced.items, ["a:1", "b:1"])
        let changed = load.applying(.added("b:1"), key: key)
        XCTAssertTrue(changed.hasMore)
        XCTAssertEqual(changed.continueToken, "t")
        XCTAssertEqual(changed.items, ["a:1", "b:1"])
    }

    func testDecodesAChangeSignal() throws {
        let change = try TalosJSON.decode(KubeChange.self, from: #"{"changed":3,"at":"2026-10-09T21:00:00Z"}"#)
        XCTAssertEqual(change, KubeChange(changed: 3, at: "2026-10-09T21:00:00Z"))
        XCTAssertEqual(try TalosJSON.decode(KubeChange.self, from: "{}"), KubeChange())
        XCTAssertEqual(KubeChange.workloadKinds.count, 3)
    }

    func testDecodesWhatTheListenerGot() {
        let list: (String) throws -> [String] = { try TalosJSON.decode([String].self, from: $0) }
        XCTAssertEqual(KubeWatchEvent<String>.decode("SYNC", json: #"["a","b"]"#, list: list), .sync(["a", "b"]))
        XCTAssertEqual(KubeWatchEvent<String>.decode("ADDED", json: #""a""#, list: list), .added("a"))
        XCTAssertEqual(KubeWatchEvent<String>.decode("DELETED", json: #""a""#, list: list), .deleted("a"))
        XCTAssertNil(KubeWatchEvent<String>.decode("BOOKMARK", json: #""a""#, list: list))
        XCTAssertNil(KubeWatchEvent<String>.decode("ADDED", json: "{", list: list))
    }
}
