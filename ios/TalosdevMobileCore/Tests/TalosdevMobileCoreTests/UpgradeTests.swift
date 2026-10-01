import XCTest
@testable import TalosdevMobileCore

final class UpgradeTests: XCTestCase {
    func testPlanDecoding() throws {
        let plan = try TalosJSON.decode(UpgradePlan.self, from: """
        {"node":"192.0.2.10","hostname":"cp-1","controlPlane":true,"currentVersion":"v1.11.2",
         "currentImage":"factory.talos.dev/installer/abc:v1.11.2","schematic":"abc",
         "etcd":{"members":3,"healthy":2,"thisNodeMember":true,"quorumAfterLoss":false},
         "blockers":["etcd would lose quorum"],"warnings":null,"forceable":true}
        """)
        XCTAssertEqual(plan.forceable, true)
        XCTAssertEqual(plan.hostname, "cp-1")
        XCTAssertTrue(plan.controlPlane)
        XCTAssertEqual(plan.etcd, UpgradePlan.Etcd(members: 3, healthy: 2, thisNodeMember: true, quorumAfterLoss: false))
        XCTAssertEqual(plan.blockers, ["etcd would lose quorum"])
        XCTAssertEqual(plan.warnings, [])

        let worker = try TalosJSON.decode(UpgradePlan.self, from: #"{"node":"w","etcd":null}"#)
        XCTAssertNil(worker.etcd)
        XCTAssertFalse(worker.controlPlane)
    }

    func testReleasesDecoding() throws {
        let releases = try TalosJSON.decode([TalosRelease].self, from: """
        [{"version":"v1.12.0-beta.1","date":"2026-09-20","prerelease":true},{"version":"v1.11.3"}]
        """)
        XCTAssertEqual(releases, [TalosRelease(version: "v1.12.0-beta.1", date: "2026-09-20", prerelease: true),
                                  TalosRelease(version: "v1.11.3")])
        XCTAssertEqual(upgradeSuggestions(releases + [TalosRelease(version: "v1.11.2")], current: "1.11.2", includePrerelease: false)
            .map(\.version), ["v1.11.3"])
        XCTAssertEqual(upgradeSuggestions(releases, current: "v1.11.2", includePrerelease: true).count, 2)
    }

    func testVersions() {
        XCTAssertEqual(normalizedTalosVersion(" 1.11.3 "), "v1.11.3")
        XCTAssertEqual(normalizedTalosVersion("V1.12.0-alpha.2"), "v1.12.0-alpha.2")
        XCTAssertNil(normalizedTalosVersion("1.11"))
        XCTAssertNil(normalizedTalosVersion("latest"))
        XCTAssertNil(normalizedTalosVersion(""))
        XCTAssertTrue(isTalosDowngrade(from: "v1.11.2", to: "v1.10.9"))
        XCTAssertFalse(isTalosDowngrade(from: "v1.11.2", to: "v1.11.2"))
        XCTAssertFalse(isTalosDowngrade(from: "v1.9.9", to: "v1.10.0"))
        XCTAssertFalse(isTalosDowngrade(from: "", to: "v1.10.0"))
    }

    func testGate() {
        let clean = UpgradePlan(node: "n", currentVersion: "v1.11.2")
        XCTAssertTrue(UpgradeGate(plan: clean, targetVersion: "v1.11.3", force: false, busy: false).canStart)
        XCTAssertFalse(UpgradeGate(plan: clean, targetVersion: "v1.11.3", force: false, busy: true).canStart)
        let invalid = UpgradeGate(plan: clean, targetVersion: "next", force: false, busy: false)
        XCTAssertFalse(invalid.canStart)
        XCTAssertTrue(invalid.invalidVersion)
        XCTAssertTrue(UpgradeGate(plan: clean, targetVersion: "1.10.0", force: false, busy: false).downgrade)
        XCTAssertTrue(UpgradeGate(plan: clean, targetVersion: "1.11.2", force: false, busy: false).sameVersion)
        XCTAssertFalse(UpgradeGate(plan: clean, targetVersion: "v1.11.3", force: false, busy: false).forceAvailable)

        let etcd = UpgradePlan(node: "n", controlPlane: true, currentVersion: "v1.11.2", blockers: ["etcd is unhealthy", "Losing this member breaks quorum"])
        let blocked = UpgradeGate(plan: etcd, targetVersion: "v1.11.3", force: false, busy: false)
        XCTAssertFalse(blocked.canStart)
        XCTAssertTrue(blocked.forceAvailable)
        XCTAssertTrue(UpgradeGate(plan: etcd, targetVersion: "v1.11.3", force: true, busy: false).canStart)

        let other = UpgradePlan(node: "n", blockers: ["etcd is unhealthy", "node is not reachable"])
        let hard = UpgradeGate(plan: other, targetVersion: "v1.11.3", force: true, busy: false)
        XCTAssertFalse(hard.forceAvailable)
        XCTAssertFalse(hard.canStart)

        // Go's verdict wins over the texts.
        let goSays = UpgradePlan(node: "n", blockers: ["member cp-2 is down"], forceable: true)
        XCTAssertTrue(UpgradeGate(plan: goSays, targetVersion: "v1.11.3", force: true, busy: false).canStart)
        let goRefuses = UpgradePlan(node: "n", blockers: ["etcd is unhealthy"], forceable: false)
        XCTAssertFalse(UpgradeGate(plan: goRefuses, targetVersion: "v1.11.3", force: true, busy: false).forceAvailable)
    }

    func testPhaseNamesAndOrder() {
        XCTAssertEqual(UpgradePhase.allCases, [.requested, .installing, .rebooting, .waiting, .booted, .done])
        XCTAssertTrue(UpgradePhase.requested < .installing)
        XCTAssertTrue(UpgradePhase.booted < .done)
        XCTAssertEqual(UpgradePhase(go: "waiting for node"), .waiting)
        XCTAssertEqual(UpgradePhase(go: "waiting_for_node"), .waiting)
        XCTAssertEqual(UpgradePhase(go: "WaitingForNode"), .waiting)
        XCTAssertEqual(UpgradePhase(go: "Rebooting"), .rebooting)
        XCTAssertNil(UpgradePhase(go: "mystery"))
    }

    func testProgressDecoding() throws {
        let ms = try TalosJSON.decode(UpgradeProgress.self, from: #"{"phase":"installing","message":"pulling","at":1800000000000}"#)
        XCTAssertEqual(ms, UpgradeProgress(phase: "installing", message: "pulling", at: 1_800_000_000_000))
        let iso = try TalosJSON.decode(UpgradeProgress.self, from: #"{"phase":"done","at":"2027-01-15T08:00:00Z"}"#)
        XCTAssertEqual(iso.at, 1_800_000_000_000)
    }

    func testTimeline() {
        let events = [
            UpgradeProgress(phase: "requested", message: "sent", at: 1),
            UpgradeProgress(phase: "installing", message: "pulling image", at: 2),
            UpgradeProgress(phase: "installing", message: "installed", at: 3),
            UpgradeProgress(phase: "requested", message: "late", at: 4),
            UpgradeProgress(phase: "mystery", message: "still installing", at: 5),
        ]
        let steps = upgradeTimeline(events)
        XCTAssertEqual(steps.map(\.state), [.done, .current, .pending, .pending, .pending, .pending])
        XCTAssertEqual(steps[0].message, "late")
        XCTAssertEqual(steps[1].at, 2)
        XCTAssertEqual(steps[1].message, "still installing")

        let finished = upgradeTimeline(events + [UpgradeProgress(phase: "booted", at: 9)], finished: true)
        XCTAssertEqual(finished.map(\.state), Array(repeating: .done, count: 6))

        let failed = upgradeTimeline(events + [UpgradeProgress(phase: "rebooting", at: 6)], failure: "timed out")
        XCTAssertEqual(failed.map(\.state), [.done, .done, .failed, .pending, .pending, .pending])
        XCTAssertEqual(failed[2].message, "timed out")

        let early = upgradeTimeline([], failure: "permission denied")
        XCTAssertEqual(early.first?.state, .failed)
        XCTAssertEqual(upgradeTimeline([]).map(\.state), Array(repeating: .pending, count: 6))
    }
}
