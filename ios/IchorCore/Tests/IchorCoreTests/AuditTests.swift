import XCTest
@testable import IchorCore

final class AuditTests: XCTestCase {
    // A KubeAuditAnalysis answer (go/ichorgo/kube_audit.go), cut down from the demo's.
    private let json = #"""
    {"nodes":[{"node":"192.0.2.10","bytes":6400000,"events":512000,"last":1791234598267},{"node":"192.0.2.12","error":"permission denied"}],
     "from":1791233698267,"to":1791234598267,"seconds":900,"requests":1324,
     "findings":[
      {"kind":"listLoop","severity":"critical","actor":{"user":"system:serviceaccount:monitoring:pod-exporter","agent":"pod-exporter","kind":"serviceAccount","namespace":"monitoring","name":"pod-exporter"},"count":450,"rate":0.5,"verb":"list","resource":"pods","value":2},
      {"kind":"hotObject","severity":"warning","actor":{"user":"system:serviceaccount:cnpg:manager","agent":"manager","kind":"serviceAccount","namespace":"cnpg","name":"manager"},"count":300,"verb":"update","resource":"clusters/status","value":9,"objects":14,"examples":["a/db","b/db"]},
      {"kind":"staleLog","severity":"critical","actor":{},"count":0,"name":"192.0.2.12","value":86400},
      {"kind":"fromTheFuture","severity":"new"}],
     "actors":[{"actor":{"user":"system:node:w1","agent":"kubelet","kind":"node","name":"w1"},"requests":200,"rate":0.2,"share":0.15,"topVerb":"get","topResource":"nodes","topCount":90}]}
    """#

    private func decode() throws -> AuditReport {
        try JSONDecoder().decode(AuditReport.self, from: Data(json.utf8))
    }

    func testDecodesAndRanks() throws {
        let r = try decode()
        XCTAssertEqual(r.findings.count, 4)
        XCTAssertEqual(r.findings[0].kind, .listLoop)
        XCTAssertEqual(r.findings[0].severity, .critical)
        XCTAssertNil(r.findings[3].kind)
        XCTAssertEqual(r.findings[3].severity, .info)
        XCTAssertEqual(r.nodes[1].error, "permission denied")
        XCTAssertEqual(r.bytesRead, 6_400_000)
        XCTAssertEqual(r.findings[1].examples, ["a/db", "b/db"])
    }

    func testNamesActorsAndTargets() throws {
        let r = try decode()
        XCTAssertEqual(r.findings[0].actor?.label, "monitoring/pod-exporter")
        XCTAssertNil(r.findings[0].actor?.agentDetail)
        XCTAssertEqual(r.actors[0].actor.label, "w1")
        XCTAssertEqual(r.actors[0].actor.agentDetail, "kubelet")
        XCTAssertEqual(r.findings[0].target, "pods")
        XCTAssertTrue(r.findings[2].aboutServer)
        XCTAssertFalse(r.findings[0].aboutServer)
    }

    func testFormatsIntervals() {
        XCTAssertEqual(formatSeconds(0.5), "0.5 s")
        XCTAssertEqual(formatSeconds(26.2), "26 s")
        XCTAssertEqual(formatSeconds(240), "4 min")
        XCTAssertEqual(formatSeconds(90400), "25.1 h")
        XCTAssertEqual(formatMegabytes(6_400_000), "6.4 MB")
    }
}
