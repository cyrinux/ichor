import XCTest
@testable import IchorCore

final class GitOpsFollowTests: XCTestCase {
    func testArgoProgressDecoding() throws {
        let p = try TalosJSON.decode(ArgoSyncProgress.self, from: """
        {"phase":"Running","message":"waiting for healthy state of apps/Deployment/web","wave":2,"waves":4,"done":12,"total":30,"at":1700000000000}
        """)
        XCTAssertEqual(p.phase, ArgoSyncPhase.running)
        XCTAssertEqual(p.wave, 2)
        XCTAssertEqual(p.waves, 4)
        XCTAssertEqual(p.done, 12)
        XCTAssertEqual(p.total, 30)
        XCTAssertEqual(p.at, 1_700_000_000_000)
        XCTAssertTrue(p.hasWaves)
        XCTAssertFalse(p.pending)

        // Before the controller starts it: no wave yet, every field optional.
        let pending = try TalosJSON.decode(ArgoSyncProgress.self, from: #"{"phase":"Pending"}"#)
        XCTAssertTrue(pending.pending)
        XCTAssertFalse(pending.hasWaves)
        XCTAssertEqual(pending.message, "")
        XCTAssertEqual(ArgoSyncProgress(phase: "Running", wave: 7, waves: 4).shownWave, 4)
    }

    func testFluxProgressDecoding() throws {
        let p = try TalosJSON.decode(FluxReconcileProgress.self, from: """
        {"phase":"ready","message":"Applied revision: main@sha1:3f2a9c1","at":5}
        """)
        XCTAssertEqual(p.phase, FluxReconcilePhase.ready)
        XCTAssertEqual(p.message, "Applied revision: main@sha1:3f2a9c1")
        XCTAssertEqual(try TalosJSON.decode(FluxReconcileProgress.self, from: "{}").phase, "")
    }

    func testArgoOutcomes() {
        XCTAssertEqual(FollowOutcome.argo(result: "Succeeded", error: nil), .succeeded)
        XCTAssertEqual(FollowOutcome.argo(result: "Terminated", error: nil), .terminated)
        XCTAssertEqual(FollowOutcome.argo(result: "Failed", error: nil), .failed(""))
        XCTAssertEqual(FollowOutcome.argo(result: "Error", error: ""), .failed(""))
        // Cancel: both empty.
        XCTAssertEqual(FollowOutcome.argo(result: "", error: nil), .unfollowed)
        XCTAssertEqual(FollowOutcome.argo(result: "", error: "forbidden"), .failed("forbidden"))
    }

    func testFluxOutcomes() {
        XCTAssertEqual(FollowOutcome.flux(result: "ready", error: nil), .succeeded)
        XCTAssertEqual(FollowOutcome.flux(result: "failed", error: nil), .failed(""))
        XCTAssertEqual(FollowOutcome.flux(result: "still reconciling", error: nil), .stillRunning)
        XCTAssertEqual(FollowOutcome.flux(result: "", error: nil), .unfollowed)
        XCTAssertEqual(FollowOutcome.flux(result: "", error: "not found"), .failed("not found"))
    }

    func testOutcomePhases() {
        XCTAssertEqual(FollowOutcome.succeeded.phase, .succeeded)
        XCTAssertEqual(FollowOutcome.failed("x").phase, .failed)
        XCTAssertEqual(FollowOutcome.terminated.phase, .stopped)
        XCTAssertEqual(FollowOutcome.stillRunning.phase, .stopped)
        XCTAssertEqual(FollowOutcome.unfollowed.phase, .stopped)
    }

    func testRolloutRestartProgress() {
        let workload = KubeWorkload(kind: "Deployment", namespace: "shop", name: "web", desired: 5)
        let pods = [
            KubeRolloutPod(name: "web-a", healthy: true, updated: true),
            KubeRolloutPod(name: "web-b", healthy: true, updated: true),
            KubeRolloutPod(name: "web-c", healthy: false, updated: true),
            KubeRolloutPod(name: "web-old", healthy: true, updated: false),
        ]
        let rolling = KubeRolloutStatus(workload: workload, pods: pods)
        XCTAssertEqual(rolling.restartCount?.done, 2)
        XCTAssertEqual(rolling.restartCount?.total, 5)
        XCTAssertNil(FollowOutcome.rollout(rolling))
        XCTAssertEqual(FollowOutcome.rollout(KubeRolloutStatus(workload: workload, done: true)), .succeeded)
        XCTAssertEqual(FollowOutcome.rollout(KubeRolloutStatus(workload: workload, failed: true)), .failed(""))
        XCTAssertNil(KubeRolloutStatus(workload: KubeWorkload(kind: "Deployment", namespace: "shop", name: "web")).restartCount)
    }

    func testTheNewRunsOpenTheirScreen() {
        for kind in [OperationKind.argoSync, .fluxReconcile, .rollout, .netPerf] {
            XCTAssertTrue(kind.hasScreen)
            XCTAssertEqual(OperationKind(link: kind.link), kind)
        }
        XCTAssertEqual(OperationKind(link: URL(string: "ichor://open?op=argoSync")!), .argoSync)
        XCTAssertTrue(OperationButton.terminate.opensApp(for: .argoSync, locked: true))
        XCTAssertFalse(OperationButton.stopFollowing.opensApp(for: .rollout, locked: true))
    }
}
