import XCTest
@testable import IchorCore

final class TalosUpdateTests: XCTestCase {
    func testDecoding() throws {
        let info = try TalosJSON.decode(TalosUpdateInfo.self, from: """
        {"latest":"v1.14.2","latestDate":"2026-09-30T10:00:00Z","newer":true,"outdated":8,"oldest":"v1.13.0","notes":"https://github.com/siderolabs/talos/releases/tag/v1.14.2"}
        """)
        XCTAssertEqual(info.latest, "v1.14.2")
        XCTAssertTrue(info.newer)
        XCTAssertEqual(info.outdated, 8)
        XCTAssertEqual(info.oldest, "v1.13.0")
        let list = try TalosJSON.decode(TalosUpdateInfo.self, from: #"{"latest":"v1.14.2","newer":true,"outdated":["a","b"]}"#)
        XCTAssertEqual(list.outdated, 2)
        XCTAssertFalse(try TalosJSON.decode(TalosUpdateInfo.self, from: "{}").newer)
    }

    func testThrottle() {
        let now = Date(timeIntervalSince1970: 100_000)
        XCTAssertTrue(shouldCheckTalosUpdate(lastCheck: nil, now: now))
        XCTAssertFalse(shouldCheckTalosUpdate(lastCheck: now.addingTimeInterval(-3_600), now: now))
        XCTAssertTrue(shouldCheckTalosUpdate(lastCheck: now.addingTimeInterval(-6 * 3_600), now: now))
        XCTAssertTrue(shouldCheckTalosUpdate(lastCheck: now.addingTimeInterval(60), now: now), "clock moved back")
    }

    func testUpgradeChoicesByRole() {
        func node(_ name: String, _ version: String, role: String = "worker", reachable: Bool = true) -> NodeOverview {
            NodeOverview(node: name, hostname: name, reachable: reachable, version: version, role: role)
        }
        let nodes = [
            node("w2", "v1.14.1"), node("w1", "v1.13.5"),
            node("cp2", "v1.14.1", role: "controlplane"), node("cp1", "v1.14.1", role: "controlplane"),
            node("cp3", "v1.14.2", role: "controlplane"), node("cp4", "v1.13.0", role: "controlplane", reachable: false),
            node("x", "v1.14.1", role: ""),
        ]
        let choices = talosUpgradeChoices(nodes, latest: "v1.14.2")
        XCTAssertEqual(choices.controlPlane.map(\.hostname), ["cp1", "cp2"])
        XCTAssertEqual(choices.workers.map(\.hostname), ["w1", "w2", "x"])
        XCTAssertEqual(choices.count, 5)
        XCTAssertTrue(choices.workersWait)

        let workersOnly = talosUpgradeChoices(nodes.filter { $0.role != "controlplane" }, latest: "v1.14.2")
        XCTAssertTrue(workersOnly.controlPlane.isEmpty)
        XCTAssertFalse(workersOnly.workersWait)
    }

    func testOutdatedAndInput() {
        XCTAssertTrue(isOutdatedTalos("v1.13.4", latest: "v1.14.2"))
        XCTAssertFalse(isOutdatedTalos("v1.14.2", latest: "v1.14.2"))
        XCTAssertFalse(isOutdatedTalos("", latest: "v1.14.2"))
        XCTAssertEqual(talosVersionsCSV(["v1.14.2", "", "v1.13.0", "v1.14.2"]), "v1.13.0,v1.14.2,v1.14.2")
        XCTAssertEqual(talosUpdateBannerCount(TalosUpdateInfo(latest: "v1.14.2", newer: true, outdated: 8), localOutdated: 3), 8)
        XCTAssertEqual(talosUpdateBannerCount(TalosUpdateInfo(latest: "v1.14.2", newer: true), localOutdated: 3), 3)
        XCTAssertNil(talosUpdateBannerCount(TalosUpdateInfo(latest: "v1.14.2", newer: false, outdated: 2), localOutdated: 2))
        XCTAssertNil(talosUpdateBannerCount(TalosUpdateInfo(latest: "", newer: true), localOutdated: 2))
    }
}
