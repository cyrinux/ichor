import XCTest
@testable import IchorCore

final class ObservabilityTests: XCTestCase {
    func testLegacyStatsDecodeAndRoundTrip() throws {
        let raw = Data(#"{"at":1000,"cpuBusy":10,"cpuTotal":20,"cpuCount":2,"memTotal":100,"memAvailable":50,"load1":1,"netRx":10,"netTx":20,"diskRead":30,"diskWrite":40}"#.utf8)
        let sample = try JSONDecoder().decode(NodeStats.self, from: raw)
        XCTAssertNil(sample.networkDevices)
        XCTAssertEqual(try JSONDecoder().decode(NodeStats.self, from: JSONEncoder().encode(sample)), sample)
    }
    func testIncidentIgnoresStoredInternalSnapshot() throws {
        let raw = Data(#"{"scope":"cluster","startedAt":1000,"updatedAt":2000,"dropped":3,"entries":[{"id":"event/1","at":1500,"node":"cp-a","kind":"event","subject":"kubelet","detail":"ready","severity":"info"}],"last":{"nodes":[]}}"#.utf8)
        let doc = try JSONDecoder().decode(IncidentDocument.self, from: raw)
        XCTAssertEqual(doc.entries.first?.id, "event/1")
        XCTAssertEqual(doc.dropped, 3)
    }
    func testFailedReadsAndRebootInvalidateAggregateRates() throws {
        let raw = Data(#"{"at":1000,"cpuBusy":10,"cpuTotal":20,"cpuCount":2,"memTotal":100,"memAvailable":50,"load1":1,"netRx":10,"netTx":20,"diskRead":30,"diskWrite":40}"#.utf8)
        var previous = try JSONDecoder().decode(NodeStats.self, from: raw)
        var current = try JSONDecoder().decode(NodeStats.self, from: raw)
        previous.bootTime = 10
        current.bootTime = 20
        XCTAssertNil(ratesBetween(previous, current))
        current.bootTime = 10
        current.errors = ["network": "unavailable"]
        XCTAssertNil(ratesBetween(previous, current))
    }
    func testEventEncodingPreservesTalosID() throws {
        let event = NodeEvent(node: "cp-a", eventId: "talos-id", at: 1000, kind: "service")
        let encoded = try JSONEncoder().encode(event)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
        XCTAssertEqual(object["id"] as? String, "talos-id")
        XCTAssertNil(object["eventId"])
    }
    func testStructuredEvidenceSurvivesTruncatedDetails() throws {
        let raw = Data(#"{"id":"sample","at":1,"node":"cp-a","kind":"metrics","subject":"node","severity":"info","detail":"{…","metrics":{"wait":0.45,"steal":0,"network":[],"disks":[],"errors":{"disk":"unavailable"}},"metricsOmitted":2}"#.utf8)
        let entry = try JSONDecoder().decode(IncidentEntry.self, from: raw)
        XCTAssertNil(entry.evidenceDetail)
        XCTAssertEqual(entry.evidenceMetrics?.wait, 0.45)
        XCTAssertEqual(entry.evidenceMetrics?.errors["disk"], "unavailable")
        XCTAssertEqual(entry.metricsOmitted, 2)
    }
    func testLegacyTruncatedMetricsAreUnavailable() throws {
        let raw = Data(#"{"id":"sample","at":1,"node":"cp-a","kind":"metrics","subject":"node","severity":"info","detail":"{…"}"#.utf8)
        let entry = try JSONDecoder().decode(IncidentEntry.self, from: raw)
        XCTAssertNil(entry.evidenceMetrics)
    }
}
