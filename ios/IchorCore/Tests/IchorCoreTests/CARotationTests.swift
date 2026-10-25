import XCTest
@testable import IchorCore

final class CARotationTests: XCTestCase {
    func testPlanDecodesAsTheCoreWritesIt() throws {
        let plan = try TalosJSON.decode(CARotationPlan.self, from: """
        {"context":"admin@prod","endpoints":["10.0.0.1"],"ca":{"subject":"O=talos","notBefore":1,"notAfter":2,"fingerprint":"3f9a1c7e2b8d4650"},
         "nodes":[{"node":"10.0.0.1","hostname":"cp-1","role":"controlplane","version":"v1.14.2","reachable":true,"acceptedCAs":0},
                  {"node":"10.0.0.9","hostname":"","role":"unknown","version":"","reachable":false,"acceptedCAs":0,"problem":"does not answer"}],
         "discovered":true,"blockers":["10.0.0.9 does not answer"],"warnings":null,"ready":false}
        """)
        XCTAssertFalse(plan.ready)
        XCTAssertEqual(plan.ca?.fingerprint, "3f9a1c7e2b8d4650")
        XCTAssertEqual(plan.nodes.map(\.health), [.ok, .critical])
        XCTAssertEqual(plan.nodes.map(\.label), ["cp-1", "10.0.0.9"])
        XCTAssertTrue(plan.warnings.isEmpty)
    }

    func testTimelineMovesForwardAndEndsDone() {
        let running = caRotationTimeline([
            CARotationProgress(phase: "check", at: 1),
            CARotationProgress(phase: "accept", message: "accepting on cp-1", at: 2),
            CARotationProgress(phase: "accept", message: "every node applied it", at: 3),
        ])
        XCTAssertEqual(running.map(\.phase), CARotationPhase.steps)
        XCTAssertEqual(running.map(\.state), [.done, .current, .pending, .pending, .pending])
        XCTAssertEqual(running[1].message, "every node applied it")

        let done = caRotationTimeline([CARotationProgress(phase: "check"), CARotationProgress(phase: "done")])
        XCTAssertTrue(done.allSatisfy { $0.state == .done }, "reaching done ends every step")

        let failed = caRotationTimeline([CARotationProgress(phase: "check"), CARotationProgress(phase: "handover")], failure: "not stored in time")
        XCTAssertEqual(failed[2].state, .failed)
        XCTAssertEqual(failed[2].message, "not stored in time")
    }

    func testProgressNodeStatesReadAsHealth() throws {
        let progress = try TalosJSON.decode(CARotationProgress.self, from: """
        {"phase":"swap","step":4,"total":5,"node":"10.0.0.1","hostname":"cp-1","message":"","at":5,
         "nodes":[{"node":"10.0.0.1","hostname":"cp-1","role":"controlplane","state":"patching"},
                  {"node":"10.0.0.2","hostname":"cp-2","role":"controlplane","state":"done"},
                  {"node":"10.0.0.3","hostname":"w-1","role":"worker","state":"failed"},
                  {"node":"10.0.0.4","hostname":"w-2","role":"worker","state":"pending"}]}
        """)
        XCTAssertEqual(progress.nodes.map(\.health), [.warning, .ok, .critical, .idle])
    }

    func testBackupCarriesTheLineage() throws {
        let payload = BackupPayload(talosconfig: "context: a\n", fingerprintLineage: ["newfp": "oldfp"])
        let decoded = try JSONDecoder().decode(BackupPayload.self, from: JSONEncoder().encode(payload))
        XCTAssertEqual(decoded.fingerprintLineage, ["newfp": "oldfp"])

        let older = try JSONDecoder().decode(BackupPayload.self, from: Data(#"{"format":3,"talosconfig":"context: a\n"}"#.utf8))
        XCTAssertNil(older.fingerprintLineage)
    }

    func testTheRotationOffersStop() {
        XCTAssertEqual(OperationButton.offered(for: .caRotation), [.stop])
        XCTAssertEqual(OperationButton.offered(for: .caRotation, stopping: true), [])
        XCTAssertTrue(OperationKind.caRotation.hasScreen)
        XCTAssertEqual(OperationKind(link: OperationKind.caRotation.link), .caRotation)
    }
}
