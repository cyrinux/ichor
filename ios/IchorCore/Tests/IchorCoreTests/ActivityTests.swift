import XCTest
@testable import IchorCore

final class ActivityTests: XCTestCase {
    private let entries = [
        ActivityEntry(at: 3, cluster: "prod", namespace: "web", object: "Deployment/api", action: "scale", params: "replicas=3"),
        ActivityEntry(at: 2, cluster: "lab", node: "10.0.0.2", action: "reboot", outcome: "failed", error: "unreachable"),
        ActivityEntry(at: 1, cluster: "prod", node: "10.0.0.3", action: "rollout-restart"),
    ]

    func testDecodeGoJSON() throws {
        let json = #"[{"at":1,"cluster":"prod","namespace":"web","object":"Pod/api-1","action":"delete-pod","outcome":"failed","error":"forbidden","demo":true}]"#
        let decoded = try TalosJSON.decode([ActivityEntry].self, from: json)
        XCTAssertEqual(decoded.count, 1)
        XCTAssertEqual(decoded[0].object, "Pod/api-1")
        XCTAssertTrue(decoded[0].failed)
        XCTAssertTrue(decoded[0].demo)
        XCTAssertEqual(decoded[0].target, "web/Pod/api-1")
        XCTAssertEqual(decoded[0].params, "")
    }

    func testActionLabel() {
        XCTAssertEqual(entries[2].actionLabel, "Rollout restart")
        XCTAssertEqual(entries[0].actionLabel, "Scale")
    }

    func testFilters() {
        XCTAssertEqual(entries.filter(ActivityFilter().matches).count, 3)
        XCTAssertEqual(entries.filter(ActivityFilter(cluster: "prod").matches).map(\.at), [3, 1])
        XCTAssertEqual(entries.filter(ActivityFilter(cluster: "prod", action: "rollout-restart").matches).map(\.at), [1])
        XCTAssertEqual(entries.filter(ActivityFilter(failedOnly: true).matches).map(\.at), [2])
    }

    func testDistinctValuesKeepTheNewestFirst() {
        XCTAssertEqual(entries.distinctValues(\.cluster), ["prod", "lab"])
        XCTAssertEqual(entries.distinctValues(\.action), ["scale", "reboot", "rollout-restart"])
    }
}
