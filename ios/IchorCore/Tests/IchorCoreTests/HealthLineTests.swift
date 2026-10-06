import XCTest
@testable import IchorCore

final class HealthLineTests: XCTestCase {
    private let unmet = "waiting for etcd to be healthy: 10.0.0.3: service is not healthy: etcd"

    func testPassedConditionIsOK() {
        XCTAssertEqual(healthLineStatus("waiting for etcd to be healthy: OK"), .ok)
        XCTAssertEqual(healthLineStatus(" waiting for all k8s nodes to report ready: OK \n"), .ok)
    }

    func testUnevaluatedConditionIsPending() {
        XCTAssertEqual(healthLineStatus("waiting for etcd to be healthy: ..."), .pending)
        XCTAssertEqual(healthLineStatus("waiting for kubelet"), .pending)
    }

    func testUnmetConditionIsWarn() {
        XCTAssertEqual(healthLineStatus(unmet), .warn)
    }

    func testLastLineOfFailedRunIsBad() {
        XCTAssertEqual(healthLineStatus(unmet, failed: true), .bad)
        XCTAssertEqual(healthLineStatus("waiting for etcd to be healthy: ...", failed: true), .bad)
    }

    func testFailedRunKeepsPassedAndInfoLines() {
        XCTAssertEqual(healthLineStatus("waiting for etcd to be healthy: OK", failed: true), .ok)
        XCTAssertEqual(healthLineStatus(#"discovered nodes: ["10.0.0.1"]"#, failed: true), .info)
    }

    func testOtherLinesAreInfo() {
        XCTAssertEqual(healthLineStatus(#"discovered nodes: ["10.0.0.1" "10.0.0.2"]"#), .info)
        XCTAssertEqual(healthLineStatus(""), .info)
    }
}
