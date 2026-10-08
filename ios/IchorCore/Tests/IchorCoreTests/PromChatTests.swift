import XCTest
@testable import IchorCore

final class PromChatTests: XCTestCase {
    func testDecodesWhatGoSends() throws {
        let s = try TalosJSON.decode(PanelSuggestion.self, from: """
        {"id":"","title":"CPU","query":"sum(rate(node_cpu_seconds_total[5m]))","unit":"cores","legend":"{{instance}}","verified":true,"empty":false,"attempts":2,"notice":"","future":1}
        """)
        XCTAssertEqual(s.title, "CPU")
        XCTAssertEqual(s.unit, "cores")
        XCTAssertTrue(s.verified)
        XCTAssertFalse(s.empty)
        XCTAssertEqual(s.attempts, 2)

        // A panel that was not checked carries why; the rest keeps its defaults.
        let unchecked = try TalosJSON.decode(PanelSuggestion.self, from: #"{"title":"T","query":"up","notice":"not checked: timeout"}"#)
        XCTAssertFalse(unchecked.verified)
        XCTAssertEqual(unchecked.attempts, 1)
        XCTAssertEqual(unchecked.notice, "not checked: timeout")
    }

    func testUsedPanelKeepsTheEditedId() {
        let s = PanelSuggestion(title: "T", query: "up", unit: "count", legend: "{{job}}")
        XCTAssertEqual(s.panel(id: "abc"), PromPanel(id: "abc", title: "T", query: "up", unit: "count", legend: "{{job}}"))
        XCTAssertEqual(s.panel(id: "").id, "")
    }

    func testCurrentPanelGoesToGoWithoutItsId() throws {
        let json = try promChatPanelJSON(PromPanel(id: "abc", title: "T", query: "up", legend: "{{job}}"))
        XCTAssertFalse(json.contains("abc"))
        XCTAssertTrue(json.contains(#""query":"up""#))
        XCTAssertTrue(json.contains(#""unit":"""#))
    }

    func testAnswerBubbleStartsEmpty() {
        var m = PanelChatMessage(fromUser: false, text: "")
        XCTAssertNil(m.panel)
        m.text = "Here"
        m.panel = PanelSuggestion(query: "up")
        XCTAssertEqual(m.panel?.query, "up")
        XCTAssertNotEqual(PanelChatMessage(fromUser: true, text: "a").id, PanelChatMessage(fromUser: true, text: "a").id)
    }
}
