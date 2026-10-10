import XCTest
@testable import IchorCore

final class OverviewBarTests: XCTestCase {
    func testDefaultKeepsFourIconsWithGitOpsAndEtcd() {
        let bar = OverviewBar()
        XCTAssertEqual(bar.icons, [.health, .workloads, .gitOps, .etcd])
        XCTAssertEqual(bar.menu, [.events, .metrics, .alerts, .kubespan, .settings])
        XCTAssertTrue(bar.isDefault)
    }

    func testMovesWithinTheBarAndTheMenu() {
        XCTAssertEqual(OverviewBar().down(.health).icons, [.workloads, .health, .gitOps, .etcd])
        XCTAssertEqual(OverviewBar().up(.kubespan).menu, [.events, .metrics, .kubespan, .alerts, .settings])
    }

    func testMovingPastTheLineCrossesIntoTheMenuAndBack() {
        let down = OverviewBar().down(.etcd)
        XCTAssertEqual(down.icons, [.health, .workloads, .gitOps])
        XCTAssertEqual(down.menu, [.etcd, .events, .metrics, .alerts, .kubespan, .settings])
        XCTAssertEqual(OverviewBar().up(.events).icons, [.health, .workloads, .gitOps, .etcd, .events])
        XCTAssertEqual(down.up(.etcd), OverviewBar())
    }

    func testEndsStayPut() {
        XCTAssertEqual(OverviewBar().up(.health), OverviewBar())
        XCTAssertEqual(OverviewBar().down(.settings), OverviewBar())
    }

    func testToMenuAndToBarJumpToTheLine() {
        let bar = OverviewBar().toMenu(.health)
        XCTAssertEqual(bar.icons, [.workloads, .gitOps, .etcd])
        XCTAssertEqual(bar.menu, [.health, .events, .metrics, .alerts, .kubespan, .settings])
        let back = OverviewBar().toBar(.settings)
        XCTAssertEqual(back.icons, [.health, .workloads, .gitOps, .etcd, .settings])
        XCTAssertEqual(back.menu, [.events, .metrics, .alerts, .kubespan])
        XCTAssertEqual(bar.toMenu(.metrics), bar)
        XCTAssertEqual(back.toBar(.settings), back)
    }

    func testEncodeRoundTripsInAndroidForm() {
        let bar = OverviewBar().toBar(.kubespan).toMenu(.health)
        XCTAssertEqual(bar.encoded, "WORKLOADS,GITOPS,ETCD,KUBESPAN|HEALTH,EVENTS,METRICS,ALERTS,SETTINGS")
        XCTAssertEqual(OverviewBar.parse(bar.encoded), bar)
        let empty = OverviewBar().toMenu(.health).toMenu(.workloads).toMenu(.gitOps).toMenu(.etcd)
        XCTAssertEqual(OverviewBar.parse(empty.encoded), empty)
        XCTAssertTrue(empty.icons.isEmpty)
    }

    func testParseFallsBackToDefault() {
        XCTAssertEqual(OverviewBar.parse(nil), OverviewBar())
        XCTAssertEqual(OverviewBar.parse(""), OverviewBar())
        XCTAssertEqual(OverviewBar.parse("garbage"), OverviewBar())
    }

    func testParseSkipsUnknownAndRepeatedAndAppendsMissingToTheMenu() {
        let bar = OverviewBar.parse("SETTINGS,GONE,SETTINGS|ETCD,SETTINGS")
        XCTAssertEqual(bar.icons, [.settings])
        XCTAssertEqual(bar.menu, [.etcd, .health, .workloads, .gitOps, .events, .metrics, .alerts, .kubespan])
        XCTAssertFalse(bar.isDefault)
    }

    func testABarSavedBeforeGitOpsKeepsItsArrangement() {
        let bar = OverviewBar.parse("HEALTH,EVENTS,WORKLOADS|METRICS,KUBESPAN,ETCD,SETTINGS")
        XCTAssertEqual(bar.icons, [.health, .events, .workloads])
        XCTAssertEqual(bar.menu, [.metrics, .kubespan, .etcd, .settings, .gitOps, .alerts])
        XCTAssertFalse(bar.isDefault)
    }

    func testSlotsPutTheLineAfterTheIcons() {
        XCTAssertEqual(OverviewBar().slots, [.action(.health), .action(.workloads), .action(.gitOps), .action(.etcd), .menuLine,
                                             .action(.events), .action(.metrics), .action(.alerts), .action(.kubespan),
                                             .action(.settings)])
    }

    func testMovingAcrossTheLineChangesTheIcons() {
        // Slots: health, workloads, gitOps, etcd, |, events, metrics, alerts, kubespan, settings.
        let down = OverviewBar().moving(fromOffsets: [0], toOffset: 6)
        XCTAssertEqual(down.icons, [.workloads, .gitOps, .etcd])
        XCTAssertEqual(down.menu, [.events, .health, .metrics, .alerts, .kubespan, .settings])
        let up = OverviewBar().moving(fromOffsets: [9], toOffset: 0)
        XCTAssertEqual(up.icons, [.settings, .health, .workloads, .gitOps, .etcd])
        XCTAssertEqual(up.menu, [.events, .metrics, .alerts, .kubespan])
        XCTAssertEqual(OverviewBar().moving(fromOffsets: [3], toOffset: 0).icons, [.etcd, .health, .workloads, .gitOps])
    }

    func testTheMenuLineDoesNotMove() {
        XCTAssertEqual(OverviewBar().moving(fromOffsets: [4], toOffset: 0), OverviewBar())
    }
}
