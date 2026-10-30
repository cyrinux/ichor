import XCTest

@testable import IchorCore

final class OnCallTests: XCTestCase {
    func testBadgeNamesWhoAcknowledged() {
        XCTAssertEqual(OnCallBadge(OnCallIncident(status: .triggered)), .triggered)
        XCTAssertEqual(
            OnCallBadge(OnCallIncident(status: .acknowledged, assignees: ["Sam"], acknowledgedBy: "Alex")),
            .acknowledged(by: "Alex"))
        XCTAssertEqual(OnCallBadge(OnCallIncident(status: .acknowledged, assignees: ["Sam"])), .acknowledged(by: "Sam"))
        XCTAssertEqual(OnCallBadge(OnCallIncident(status: .acknowledged)), .acknowledged(by: ""))
        XCTAssertEqual(OnCallBadge(OnCallIncident(status: .unknown)), .hidden)
    }

    func testConfigIsSetNeedsATokenAndAService() {
        XCTAssertFalse(OnCallConfig().isSet)
        XCTAssertFalse(OnCallConfig(token: "sample").isSet)
        XCTAssertFalse(OnCallConfig(token: " ", serviceIds: ["P1"]).isSet)
        XCTAssertTrue(OnCallConfig(token: "sample", serviceIds: ["P1"]).isSet)
    }

    func testAnUnknownStatusDecodes() throws {
        let decoded = try TalosJSON.decode(
            OnCallIncidents.self,
            from: #"{"incidents":[{"id":"P1","status":"resolved"}],"matches":{"f":{"incidentId":"P1"}}}"#)
        XCTAssertEqual(decoded.incidents.first?.status, .unknown)
        XCTAssertEqual(decoded.incident(of: "f")?.id, "P1")
        XCTAssertNil(decoded.incident(of: "other"))
    }
}
