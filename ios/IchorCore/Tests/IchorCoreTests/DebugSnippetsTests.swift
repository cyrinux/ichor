import XCTest
@testable import IchorCore

final class DebugSnippetsTests: XCTestCase {
    private let json = """
    [{"group":"interfaces","label":"addresses","command":"ip -br -c a","run":true},
     {"group":"interfaces","label":"routes","command":"ip r","run":true},
     {"group":"reachability","label":"ping","command":"ping -c3 ","run":false}]
    """

    func testDecodesAndGroupsInOrder() throws {
        let snippets = try TalosJSON.decode([DebugSnippet].self, from: json)
        let sections = groupedDebugSnippets(snippets)
        XCTAssertEqual(sections.map(\.group), ["interfaces", "reachability"])
        XCTAssertEqual(sections[0].snippets.map(\.label), ["addresses", "routes"])
    }

    func testRunSnippetsEndWithEnterTypedOnesDoNot() throws {
        let snippets = try TalosJSON.decode([DebugSnippet].self, from: json)
        XCTAssertEqual(String(decoding: snippets[0].bytes, as: UTF8.self), "ip -br -c a\r")
        XCTAssertEqual(String(decoding: snippets[2].bytes, as: UTF8.self), "ping -c3 ")
        XCTAssertEqual(snippets[0].display, "ip -br -c a")
        XCTAssertEqual(snippets[2].display, "ping -c3 …")
    }
}
