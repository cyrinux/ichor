import XCTest
@testable import IchorCore

final class LogTemplatesTests: XCTestCase {
    func testDecodesTheGoShape() throws {
        let json = #"""
            {"groups":[{"template":"connection to <*> refused after <*>","level":"error","source":"","count":1842,
             "share":0.61,"firstTs":1,"lastTs":2,"sample":"connection to 10.0.0.7:5432 refused after 30s","pods":2}],
             "lines":3000,"distinct":41}
            """#
        let templates = try TalosJSON.decode(LogTemplates.self, from: json)
        let group = try XCTUnwrap(templates.groups.first)
        XCTAssertEqual(templates.lines, 3000)
        XCTAssertEqual(group.sample, "connection to 10.0.0.7:5432 refused after 30s")
        XCTAssertEqual(group.pods, 2)
        XCTAssertEqual(group.shareText, "61 %")
        XCTAssertEqual(try TalosJSON.decode(LogTemplates.self, from: "{}"), LogTemplates())
    }

    func testPartsDimThePlaceholders() {
        let parts = LogTemplateGroup(template: "connection to <*> refused after <*>").parts
        XCTAssertEqual(parts.map(\.text), ["connection to ", "<*>", " refused after ", "<*>"])
        XCTAssertEqual(parts.map(\.hole), [false, true, false, true])
        XCTAssertEqual(LogTemplateGroup(template: "<*>").parts.map(\.hole), [true])
        XCTAssertEqual(LogTemplateGroup(template: "rare", count: 1, share: 0.0004).shareText, "<1 %")
    }

    func testInputsAreWhatGoReads() throws {
        XCTAssertEqual(logTemplatesInput(["a", "b \"c\""]), #"["a","b \"c\""]"#)
        let line = WorkloadLogLine(pod: "web-1", time: 1_780_000_000_000, text: "timeout", level: "warn")
        let decoded = try JSONSerialization.jsonObject(with: Data(logTemplatesInput([line]).utf8)) as? [[String: Any]]
        XCTAssertEqual(decoded?.first?["pod"] as? String, "web-1")
        XCTAssertEqual(decoded?.first?["time"] as? Int64, 1_780_000_000_000)
        XCTAssertEqual(decoded?.first?["text"] as? String, "timeout")
    }
}
