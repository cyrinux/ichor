import XCTest
@testable import TalosdevMobileCore

final class EventsTests: XCTestCase {
    private func event(_ id: String, at: Int64, node: String = "10.0.0.2", kind: String = "address",
                       subject: String = "cp-1", action: String = "", message: String = "10.0.0.2",
                       severity: String = "info") -> NodeEvent {
        NodeEvent(node: node, eventId: id, at: at, kind: kind, subject: subject, action: action,
                  message: message, severity: severity)
    }

    func testDecodeGoJSON() throws {
        let json = #"{"node":"10.0.0.2","id":"d0abcdefghijklmnopqr","at":1800000000000,"kind":"service","subject":"kubelet","action":"running","message":"Health check successful","severity":"info"}"#
        let decoded = try TalosJSON.decode(NodeEvent.self, from: json)
        XCTAssertEqual(decoded.eventId, "d0abcdefghijklmnopqr")
        XCTAssertEqual(decoded.id, "10.0.0.2/d0abcdefghijklmnopqr")
        XCTAssertEqual(decoded.eventKind, .service)
        XCTAssertEqual(decoded.eventSeverity, .info)
        XCTAssertEqual(decoded.at, 1_800_000_000_000)
    }

    func testDecodeToleratesMissingFieldsAndUnknownKinds() throws {
        let decoded = try TalosJSON.decode(NodeEvent.self, from: #"{"node":"n","id":"x","at":5,"kind":"weird"}"#)
        XCTAssertEqual(decoded.eventKind, .other)
        XCTAssertEqual(decoded.eventSeverity, .info)
        XCTAssertEqual(decoded.message, "")
    }

    func testCollapseConsecutiveIdenticalEvents() {
        let events = [
            event("e5", at: 5_000),
            event("e4", at: 4_000),
            event("e3", at: 3_000),
            event("e2", at: 2_000, kind: "service", subject: "kubelet", action: "running", message: ""),
            event("e1", at: 1_000),
        ]
        let groups = collapseEvents(events)
        XCTAssertEqual(groups.map(\.count), [3, 1, 1])
        XCTAssertEqual(groups[0].event.eventId, "e5")
        XCTAssertEqual(groups[0].firstAt, 3_000)
        XCTAssertEqual(groups[2].event.eventId, "e1")
    }

    func testCollapseKeepsDifferentNodesAndMessagesApart() {
        let events = [
            event("a", at: 3_000),
            event("b", at: 2_000, node: "10.0.0.3"),
            event("c", at: 1_000, message: "10.0.0.9"),
        ]
        XCTAssertEqual(collapseEvents(events).map(\.count), [1, 1, 1])
        XCTAssertEqual(collapseEvents([]), [])
    }

    func testFilters() {
        let events = [
            event("1", at: 6, kind: "service", severity: "error"),
            event("2", at: 5, kind: "sequence"),
            event("3", at: 4, kind: "phase"),
            event("4", at: 3, kind: "task"),
            event("5", at: 2, kind: "machine", severity: "warning"),
            event("6", at: 1, kind: "address"),
        ]
        XCTAssertEqual(events.filter(EventFilter.all.matches).count, 6)
        XCTAssertEqual(events.filter(EventFilter.problems.matches).map(\.eventId), ["1", "5"])
        XCTAssertEqual(events.filter(EventFilter.services.matches).map(\.eventId), ["1"])
        XCTAssertEqual(events.filter(EventFilter.boot.matches).map(\.eventId), ["2", "3", "4"])
    }

    func testFilterThenCollapse() {
        let events = [
            event("3", at: 3),
            event("2", at: 2, kind: "service", subject: "kubelet", message: "x"),
            event("1", at: 1),
        ]
        XCTAssertEqual(eventGroups(events, filter: .all).count, 3)
        // Hiding the service event makes the two address events consecutive.
        let address = eventGroups(events, filter: .all).filter { $0.event.kind == "address" }
        XCTAssertEqual(address.count, 2)
        XCTAssertEqual(eventGroups(events.filter { $0.kind == "address" }, filter: .all).map(\.count), [2])
    }

    func testInsertKeepsNewestFirstAndSkipsDuplicates() {
        var list: [NodeEvent] = []
        list = insertEvent(event("b", at: 2_000), into: list)
        list = insertEvent(event("a", at: 1_000), into: list)
        list = insertEvent(event("c", at: 3_000), into: list)
        list = insertEvent(event("b2", at: 2_000), into: list) // tie: later arrival first
        XCTAssertEqual(list.map(\.eventId), ["c", "b2", "b", "a"])
        XCTAssertEqual(insertEvent(event("b", at: 2_000), into: list).map(\.eventId), ["c", "b2", "b", "a"])
        // Same id from another node is a different event.
        XCTAssertEqual(insertEvent(event("b", at: 2_000, node: "10.0.0.3"), into: list).count, 5)
    }

    func testInsertCapsDroppingOldest() {
        var list: [NodeEvent] = []
        for i in 0..<5 { list = insertEvent(event("e\(i)", at: Int64(i)), into: list, cap: 3) }
        XCTAssertEqual(list.map(\.eventId), ["e4", "e3", "e2"])
        // An event older than everything kept is dropped right away.
        XCTAssertEqual(insertEvent(event("old", at: -1), into: list, cap: 3).map(\.eventId), ["e4", "e3", "e2"])
    }
}
