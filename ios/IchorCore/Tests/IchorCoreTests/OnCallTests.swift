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

    func testAcknowledgePageTakesTheAcknowledgePlace() {
        let paged = alertActions(key: "am:f00d", problem: true, canWake: false, canAcknowledgePage: true)
        XCTAssertEqual(paged, [.silence, .acknowledgePage, .snooze])
        XCTAssertEqual(withAcknowledge(paged), paged)
        XCTAssertEqual(alertActions(key: "am:f00d", problem: false, canWake: false, canAcknowledgePage: true), [])
        XCTAssertEqual(alertActions(key: "cert", problem: true, canWake: false, canAcknowledgePage: true), [.snooze])
        XCTAssertFalse(AlertAction.acknowledgePage.changesCluster)
    }

    func testEveryAlertmanagerActionSetHasOneCategory() {
        let sets = alertActionSets(kind: "alertmanager")
        XCTAssertTrue(sets.contains([.silence, .acknowledgePage, .snooze]))
        XCTAssertEqual(
            Set(sets.map { alertCategory(kind: "alertmanager", actions: $0, hideDetails: false) }).count, sets.count)
    }

    func testABackupOnlySaysAnIntegrationWasSet() throws {
        let clusters = backupClusters(
            fingerprints: ["a", "b"], names: [:], colors: [:], kubeServers: [:], oncallSet: ["a"])
        XCTAssertEqual(clusters["a"]?.oncallSet, true)
        XCTAssertNil(clusters["b"]?.oncallSet)
        let json = String(decoding: try JSONEncoder().encode(clusters), as: UTF8.self)
        XCTAssertFalse(json.contains("token"))
        XCTAssertTrue(onCallTokenNeeded(clusters, fingerprints: ["a", "b"]) { _ in false })
        XCTAssertFalse(onCallTokenNeeded(clusters, fingerprints: ["a", "b"]) { $0 == "a" })
        XCTAssertFalse(onCallTokenNeeded(clusters, fingerprints: ["b"]) { _ in false })
        XCTAssertFalse(onCallTokenNeeded(nil, fingerprints: ["a"]) { _ in false })
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
