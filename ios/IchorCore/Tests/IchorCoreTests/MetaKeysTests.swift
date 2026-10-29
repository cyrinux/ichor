import XCTest
@testable import IchorCore

final class MetaKeysTests: XCTestCase {
    func testListDecodes() throws {
        let list = try TalosJSON.decode(
            MetaKeyList.self,
            from: """
                {"keys":[{"key":10,"hex":"0x0a","name":"metal network config","value":"addresses: []\\n","size":14,"binary":false,"secret":false,"writable":true},
                         {"key":16,"hex":"0x10","name":"unique machine token","value":"","size":11,"binary":false,"secret":true,"writable":true},
                         {"key":12,"hex":"0x0c","name":"user reserved 1","value":"0001","size":2,"binary":true,"secret":false,"writable":true}],
                 "loaded":true,"known":null}
                """)
        XCTAssertTrue(list.loaded)
        XCTAssertEqual(list.keys.map(\.editable), [true, false, false])
        XCTAssertTrue(list.known.isEmpty)
    }

    func testTypedKeysAreHex() {
        XCTAssertEqual(parseMetaKey("0x0a"), 10)
        XCTAssertEqual(parseMetaKey(" 0A "), 10)
        XCTAssertEqual(parseMetaKey("ff"), 255)
        XCTAssertNil(parseMetaKey("0x100"))
        XCTAssertNil(parseMetaKey("zz"))
        XCTAssertNil(parseMetaKey(""))
        XCTAssertEqual(metaHex(10), "0x0a")
    }
}
