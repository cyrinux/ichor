import XCTest
@testable import IchorCore

final class ConfigSnippetsTests: XCTestCase {
    func testListDecodesByGroup() throws {
        let list = try TalosJSON.decode(ConfigSnippetList.self, from: """
        {"snippets":[{"id":"ntp","group":"network","params":[{"name":"servers","type":"list","hint":"time.cloudflare.com"}],"staged":false},
                     {"id":"kernel_args","group":"kernel","params":[{"name":"args","type":"list","hint":"console=ttyS0"}],"staged":true},
                     {"id":"dns","group":"network","params":null,"staged":false}]}
        """)
        XCTAssertEqual(list.byGroup.map(\.group), ["network", "kernel"])
        XCTAssertEqual(list.byGroup.first?.snippets.map(\.id), ["ntp", "dns"])
        XCTAssertTrue(list.snippets[1].staged)
        XCTAssertTrue(list.snippets[2].params.isEmpty)
    }

    func testEditsDecodeAndEncodeBack() throws {
        let result = try TalosJSON.decode(SnippetEdits.self, from: """
        {"fixable":true,"edits":[{"doc":0,"path":["machine","time","servers"],"op":"put","key":"","type":"yaml","value":"[\\"a.example.test\\"]"}],"staged":false}
        """)
        XCTAssertTrue(result.fixable)
        XCTAssertEqual(result.edits.first?.op, "put")
        XCTAssertTrue(result.edits.first?.json().contains("\"op\":\"put\"") ?? false)
    }

    func testOnlyConfigSettingsCanBeFixed() throws {
        func change(_ key: String, before: String = "a") throws -> DriftChange {
            try TalosJSON.decode(DriftChange.self, from: #"{"node":"192.0.2.11","reference":"192.0.2.10","key":"\#(key)","before":"\#(before)","after":"b"}"#)
        }
        XCTAssertEqual(try change("ntp").fix, DriftFix(key: "ntp", before: "a", after: "b"))
        XCTAssertNotNil(try change("mtu").fix)
        XCTAssertNil(try change("version").fix)
        XCTAssertNil(try change("dns", before: "").fix)
    }
}
