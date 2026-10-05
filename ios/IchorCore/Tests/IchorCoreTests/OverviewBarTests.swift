import XCTest
@testable import IchorCore

final class OverviewBarTests: XCTestCase {
    func testDefaultKeepsThreeIconsAndTheRestInTheMenu() {
        let bar = OverviewBar()
        XCTAssertEqual(bar.icons, [.health, .events, .workloads])
        XCTAssertEqual(bar.menu, [.metrics, .kubespan, .etcd, .settings])
        XCTAssertTrue(bar.isDefault)
    }

    func testMovesWithinTheBarAndTheMenu() {
        XCTAssertEqual(OverviewBar().down(.health).icons, [.events, .health, .workloads])
        XCTAssertEqual(OverviewBar().up(.etcd).menu, [.metrics, .etcd, .kubespan, .settings])
    }

    func testMovingPastTheLineCrossesIntoTheMenuAndBack() {
        let down = OverviewBar().down(.workloads)
        XCTAssertEqual(down.icons, [.health, .events])
        XCTAssertEqual(down.menu, [.workloads, .metrics, .kubespan, .etcd, .settings])
        XCTAssertEqual(OverviewBar().up(.metrics).icons, [.health, .events, .workloads, .metrics])
        XCTAssertEqual(down.up(.workloads), OverviewBar())
    }

    func testEndsStayPut() {
        XCTAssertEqual(OverviewBar().up(.health), OverviewBar())
        XCTAssertEqual(OverviewBar().down(.settings), OverviewBar())
    }

    func testToMenuAndToBarJumpToTheLine() {
        let bar = OverviewBar().toMenu(.health)
        XCTAssertEqual(bar.icons, [.events, .workloads])
        XCTAssertEqual(bar.menu, [.health, .metrics, .kubespan, .etcd, .settings])
        let back = OverviewBar().toBar(.settings)
        XCTAssertEqual(back.icons, [.health, .events, .workloads, .settings])
        XCTAssertEqual(back.menu, [.metrics, .kubespan, .etcd])
        XCTAssertEqual(bar.toMenu(.metrics), bar)
        XCTAssertEqual(back.toBar(.settings), back)
    }

    func testEncodeRoundTripsInAndroidForm() {
        let bar = OverviewBar().toBar(.etcd).toMenu(.health)
        XCTAssertEqual(bar.encoded, "EVENTS,WORKLOADS,ETCD|HEALTH,METRICS,KUBESPAN,SETTINGS")
        XCTAssertEqual(OverviewBar.parse(bar.encoded), bar)
        let empty = OverviewBar().toMenu(.health).toMenu(.events).toMenu(.workloads)
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
        XCTAssertEqual(bar.menu, [.etcd, .health, .events, .workloads, .metrics, .kubespan])
        XCTAssertFalse(bar.isDefault)
    }

    func testSlotsPutTheLineAfterTheIcons() {
        XCTAssertEqual(OverviewBar().slots, [.action(.health), .action(.events), .action(.workloads), .menuLine,
                                             .action(.metrics), .action(.kubespan), .action(.etcd), .action(.settings)])
    }

    func testMovingAcrossTheLineChangesTheIcons() {
        // Slots: health, events, workloads, |, metrics, kubespan, etcd, settings.
        let down = OverviewBar().moving(fromOffsets: [0], toOffset: 5)
        XCTAssertEqual(down.icons, [.events, .workloads])
        XCTAssertEqual(down.menu, [.metrics, .health, .kubespan, .etcd, .settings])
        let up = OverviewBar().moving(fromOffsets: [7], toOffset: 0)
        XCTAssertEqual(up.icons, [.settings, .health, .events, .workloads])
        XCTAssertEqual(up.menu, [.metrics, .kubespan, .etcd])
        XCTAssertEqual(OverviewBar().moving(fromOffsets: [2], toOffset: 0).icons, [.workloads, .health, .events])
    }

    func testTheMenuLineDoesNotMove() {
        XCTAssertEqual(OverviewBar().moving(fromOffsets: [3], toOffset: 0), OverviewBar())
    }
}
