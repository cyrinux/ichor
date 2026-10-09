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

    func testDecodesWhatTheListenerGot() {
        let list: (String) throws -> [String] = { try TalosJSON.decode([String].self, from: $0) }
        XCTAssertEqual(KubeWatchEvent<String>.decode("SYNC", json: #"["a","b"]"#, list: list), .sync(["a", "b"]))
        XCTAssertEqual(KubeWatchEvent<String>.decode("ADDED", json: #""a""#, list: list), .added("a"))
        XCTAssertEqual(KubeWatchEvent<String>.decode("DELETED", json: #""a""#, list: list), .deleted("a"))
        XCTAssertNil(KubeWatchEvent<String>.decode("BOOKMARK", json: #""a""#, list: list))
        XCTAssertNil(KubeWatchEvent<String>.decode("ADDED", json: "{", list: list))
    }

    func testTableRowsComeWithTheColumnsOnSyncOnly() {
        let sync = KubeResourceWatchEvent.decode("SYNC", json: """
            {"columns":[{"name":"Name"},{"name":"Ready"}],"rows":[{"name":"web","namespace":"shop","cells":["web","1/2"]}]}
            """)
        XCTAssertEqual(sync?.columns?.map(\.name), ["Name", "Ready"])
        let modified = KubeResourceWatchEvent.decode("MODIFIED", json: #"{"name":"web","namespace":"shop","cells":["web","2/2"]}"#)
        XCTAssertNil(modified?.columns)
        let added = KubeResourceWatchEvent.decode("ADDED", json: #"{"name":"web","namespace":"lab","cells":["web","0/1"]}"#)
        guard let sync, let modified, let added else { return XCTFail("undecoded") }

        var load = PagedLoad<KubeResourceRow>.complete([]).applying(sync.change, key: \.id)
        load = load.applying(modified.change, key: \.id)
        XCTAssertEqual(load.items.map(\.cells), [["web", "2/2"]])
        // Same name in another namespace is another row.
        load = load.applying(added.change, key: \.id)
        XCTAssertEqual(load.items.map(\.id), ["shop/web", "lab/web"])
        XCTAssertNil(KubeResourceWatchEvent.decode("SYNC", json: "{"))
    }
}
