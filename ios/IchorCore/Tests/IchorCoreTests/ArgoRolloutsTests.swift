import XCTest
@testable import IchorCore

final class ArgoRolloutsTests: XCTestCase {
    // A KubeArgoRollouts answer (go/ichorgo/kube_argo_rollouts.go), cut down, with a field from the future.
    private let json = #"""
        {"installed":true,"rollouts":[
          {"namespace":"shop","name":"checkout","strategy":"canary","phase":"Degraded","message":"RolloutAborted",
           "paused":false,"pauseReasons":[],"step":0,"steps":[{"kind":"setWeight","label":"20 %"},{"kind":"pause","label":"∞"}],
           "weight":0,"aborted":true,"promoteFull":false,"stableHash":"5f7c9d","currentHash":"6b8d4e","replicas":4,
           "updated":0,"ready":4,"available":4,"autoPromotion":false,"workloadRef":"","images":["shop/checkout:1.3"],
           "level":"critical","updatedAt":1760000000000},
          {"namespace":"shop","name":"rollouts-demo","strategy":"canary","phase":"Paused","message":"CanaryPauseStep",
           "paused":true,"pauseReasons":["CanaryPauseStep"],"pausedSince":1760000100000,"step":2,
           "steps":[{"kind":"setWeight","label":"20 %"},{"kind":"pause","label":"1h"},{"kind":"analysis","label":"success-rate"},
                    {"kind":"setWeight","label":"60 %"},{"kind":"pause","label":"∞"}],
           "weight":20,"aborted":false,"stableHash":"5f7c9d8b6","currentHash":"6b8d4e2a1","replicas":5,"updated":1,"ready":5,
           "available":5,"analysis":{"name":"rollouts-demo-6b8d4e-2","status":"Running","message":""},
           "workloadRef":"Deployment/rollouts-demo","images":["argoproj/rollouts-demo:yellow"],"level":"warning","future":1},
          {"namespace":"api","name":"gateway","strategy":"blueGreen","phase":"Paused","paused":true,
           "pauseReasons":["BlueGreenPause"],"step":-1,"steps":[],"stableHash":"aa11","currentHash":"bb22","replicas":2,
           "autoPromotion":false,"level":"warning"},
          {"namespace":"api","name":"users","strategy":"canary","phase":"Healthy","step":3,
           "steps":[{"kind":"setWeight","label":"50 %"},{"kind":"pause","label":"10m"},{"kind":"setWeight","label":"100 %"}],
           "stableHash":"cc33","currentHash":"cc33","replicas":3,"updated":3,"ready":3,"available":3,"level":"ok"}]}
        """#

    private func status() throws -> ArgoRolloutsStatus { try TalosJSON.decode(ArgoRolloutsStatus.self, from: json) }
    private func rollout(_ name: String) throws -> ArgoRollout {
        try XCTUnwrap(status().rollouts.first { $0.name == name })
    }
    private func allowed(_ r: ArgoRollout) -> Set<ArgoRolloutAction> {
        Set(ArgoRolloutAction.allCases.filter(r.allows))
    }

    /// A rollout from its fields only, the rest left to the decoder's defaults.
    private func make(_ fields: String) throws -> ArgoRollout {
        try TalosJSON.decode(ArgoRollout.self, from: "{\(fields)}")
    }

    func testDecodesTheReport() throws {
        let s = try status()
        XCTAssertTrue(s.installed)
        XCTAssertEqual(s.rollouts.count, 4)
        let demo = try rollout("rollouts-demo")
        XCTAssertEqual(demo.id, "shop/rollouts-demo")
        XCTAssertFalse(demo.isBlueGreen)
        XCTAssertEqual(demo.step, 2)
        XCTAssertEqual(demo.steps.count, 5)
        XCTAssertEqual(demo.steps[2], ArgoRolloutStep(kind: "analysis", label: "success-rate"))
        XCTAssertEqual(demo.weight, 20)
        XCTAssertEqual(demo.pauseReasons, ["CanaryPauseStep"])
        XCTAssertEqual(demo.pausedSince, 1_760_000_100_000)
        XCTAssertEqual(demo.analysis?.status, "Running")
        XCTAssertEqual(demo.workloadRef, "Deployment/rollouts-demo")
        XCTAssertEqual(demo.level, .warning)
        XCTAssertTrue(try rollout("gateway").isBlueGreen)
        XCTAssertEqual(try rollout("checkout").level, .critical)
    }

    func testMissingFieldsDefault() throws {
        let empty = try TalosJSON.decode(ArgoRolloutsStatus.self, from: "{}")
        XCTAssertFalse(empty.installed)
        XCTAssertEqual(empty.rollouts, [])
        XCTAssertFalse(empty.needsPolling)
        let r = try make(#""name":"bare""#)
        XCTAssertEqual(r.id, "/bare")
        XCTAssertEqual(r.strategy, ArgoRollout.canary)
        XCTAssertEqual(r.step, -1)
        XCTAssertEqual(r.steps, [])
        XCTAssertEqual(r.pausedSince, 0)
        XCTAssertNil(r.analysis)
        XCTAssertEqual(r.level, .unknown)
        XCTAssertEqual(try TalosJSON.decode(ArgoRolloutStep.self, from: "{}"), ArgoRolloutStep(kind: "other"))
        let analysis = try TalosJSON.decode(ArgoRolloutAnalysis.self, from: #"{"status":"Failed"}"#)
        XCTAssertEqual(analysis.name, "")
        XCTAssertTrue(analysis.failed)
    }

    func testGroupsByNamespace() throws {
        let groups = try status().byNamespace
        XCTAssertEqual(groups.map(\.namespace), ["api", "shop"])
        XCTAssertEqual(groups[1].rollouts.map(\.name), ["checkout", "rollouts-demo"])
    }

    func testPausedCanaryPromotesAbortsButDoesNotPause() throws {
        XCTAssertEqual(allowed(try rollout("rollouts-demo")), [.promote, .promoteFull, .abort])
    }

    func testBlueGreenAwaitingPromotion() throws {
        XCTAssertEqual(allowed(try rollout("gateway")), [.promote, .promoteFull, .abort])
        // Not paused (the preview still scaling, or auto-promotion on its way): nothing to promote yet, pausable.
        let moving = try make(#""strategy":"blueGreen","stableHash":"aa","currentHash":"bb""#)
        XCTAssertFalse(moving.canPromote)
        XCTAssertFalse(moving.awaitsPromotion)
        XCTAssertTrue(moving.canPause)
    }

    func testAbortedOnlyRetries() throws {
        XCTAssertEqual(allowed(try rollout("checkout")), [.retry])
        // Even paused: an aborted rollout is not promoted.
        XCTAssertFalse(try make(#""paused":true,"aborted":true,"stableHash":"a","currentHash":"b""#).canPromote)
    }

    func testHealthyDoesNothing() throws {
        XCTAssertEqual(allowed(try rollout("users")), [])
    }

    func testProgressingCanaryAbortsAndPauses() throws {
        let r = try make(
            #""phase":"Progressing","stableHash":"a","currentHash":"b","step":1,"steps":[{"kind":"setWeight"},{"kind":"pause"}]"#
        )
        XCTAssertEqual(allowed(r), [.promoteFull, .abort, .pause])
    }

    func testNoCurrentHashPromotesNothingFully() throws {
        // A paused canary with no pod hash yet: promote moves the step, nothing new to switch or abort.
        let r = try make(#""paused":true,"stableHash":"a""#)
        XCTAssertTrue(r.canPromote)
        XCTAssertFalse(r.canPromoteFull)
        XCTAssertFalse(r.canAbort)
        XCTAssertTrue(try make(#""strategy":"blueGreen","stableHash":"a""#).canPause)
        XCTAssertFalse(try make(#""strategy":"blueGreen","stableHash":"a""#).canPromote)
    }

    func testProgress() throws {
        XCTAssertEqual(try rollout("rollouts-demo").progress, 0.4, accuracy: 1e-9)
        XCTAssertEqual(try rollout("users").progress, 1)
        XCTAssertEqual(try rollout("gateway").progress, 0)  // no steps
        XCTAssertEqual(try make(#""step":-1,"steps":[{"kind":"pause"}]"#).progress, 0)
        XCTAssertEqual(try make(#""step":9,"steps":[{"kind":"pause"},{"kind":"pause"}]"#).progress, 1)
    }

    func testDisplayStepAndAwaitingPromotion() throws {
        XCTAssertEqual(try rollout("rollouts-demo").displayStep, 3)
        XCTAssertEqual(try rollout("users").displayStep, 3)  // every step done
        XCTAssertEqual(try make(#""step":-1,"steps":[{"kind":"pause"}]"#).displayStep, 0)
        XCTAssertTrue(try rollout("gateway").awaitsPromotion)
        XCTAssertFalse(try rollout("rollouts-demo").awaitsPromotion)  // a canary
        XCTAssertFalse(
            try make(#""strategy":"blueGreen","paused":true,"stableHash":"a","currentHash":"a""#).awaitsPromotion)
    }

    func testPolling() throws {
        let s = try status()
        // A paused rollout with its analysis running.
        XCTAssertTrue(try rollout("rollouts-demo").needsPolling)
        XCTAssertFalse(try rollout("gateway").needsPolling)
        XCTAssertFalse(try rollout("users").needsPolling)
        XCTAssertTrue(s.needsPolling)
        XCTAssertTrue(try make(#""phase":"Progressing""#).needsPolling)
        XCTAssertFalse(try make(#""paused":true,"analysis":{"status":"Successful"}"#).needsPolling)
    }

    func testWireNamesAndShortHash() {
        XCTAssertEqual(
            ArgoRolloutAction.allCases.map(\.rawValue), ["promote", "promoteFull", "abort", "retry", "pause"])
        XCTAssertEqual(ArgoRollout.shortHash("6b8d4e2a1f"), "6b8d4e2")
        XCTAssertEqual(ArgoRollout.shortHash(""), "")
    }
}
