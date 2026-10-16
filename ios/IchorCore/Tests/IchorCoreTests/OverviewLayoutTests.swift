import XCTest
@testable import IchorCore

final class OverviewLayoutTests: XCTestCase {
    func testDefaultShowsEverySectionInOrder() {
        let layout = OverviewLayout()
        XCTAssertEqual(layout.visible, OverviewCard.allCases)
        XCTAssertTrue(layout.hiddenCards.isEmpty)
        XCTAssertTrue(layout.isDefault)
    }

    func testEncodeRoundTripsInAndroidForm() {
        let layout = OverviewLayout().move(from: 7, to: 0).hiding(.apps)
        XCTAssertEqual(layout.encoded, "NODES,PINNED,TALOS_UPDATE,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX,TIME_DRIFT,ALERTS")
        XCTAssertEqual(OverviewLayout.parse(layout.encoded), layout)
    }

    func testParseFallsBackToDefault() {
        XCTAssertEqual(OverviewLayout.parse(nil), OverviewLayout())
        XCTAssertEqual(OverviewLayout.parse(""), OverviewLayout())
        XCTAssertEqual(OverviewLayout.parse("garbage"), OverviewLayout())
    }

    func testParseSkipsUnknownAndRepeatedAndAppendsMissing() {
        let layout = OverviewLayout.parse("TIME_DRIFT, -NODES,GONE,TIME_DRIFT,-NODES")
        XCTAssertEqual(layout.order, [.pinned, .talosUpdate, .timeDrift, .nodes, .summary, .apps, .dataServices, .argoCD, .flux, .alerts])
        XCTAssertEqual(layout.hidden, [.nodes])
        XCTAssertEqual(layout.visible, [.pinned, .talosUpdate, .timeDrift, .summary, .apps, .dataServices, .argoCD, .flux, .alerts])
    }

    func testALayoutSavedBeforeTheTalosUpdateCardKeepsItOnTop() {
        // It was pinned above the sections then; other new ones still come last.
        let saved = OverviewLayout.parse("NODES,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX")
        XCTAssertEqual(saved.visible, [.pinned, .talosUpdate, .nodes, .summary, .dataServices, .argoCD, .flux, .timeDrift, .alerts])
        XCTAssertEqual(OverviewCard.allCases.filter(\.leadsWhenNew), [.pinned, .talosUpdate])
        // Once saved with it, it stays where it was put.
        let placed = OverviewLayout.parse("SUMMARY,-TALOS_UPDATE,NODES")
        XCTAssertEqual(Array(placed.order.prefix(4)), [.pinned, .summary, .talosUpdate, .nodes])
        XCTAssertEqual(placed.hidden, [.talosUpdate])
    }

    func testALayoutSavedBeforePinsPutsThePinnedCardFirst() {
        let saved = OverviewLayout.parse("TALOS_UPDATE,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX,NODES,TIME_DRIFT,ALERTS")
        XCTAssertEqual(saved.visible.first, .pinned)
        XCTAssertEqual(saved.hidden, [.apps])
        // The saved form stays the same names, one more.
        XCTAssertEqual(saved.encoded, "PINNED,TALOS_UPDATE,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX,NODES,TIME_DRIFT,ALERTS")
    }

    func testMoveWorksOnShownSections() {
        let moved = OverviewLayout().hiding(.apps).move(from: 6, to: 3)
        XCTAssertEqual(moved.visible, [.pinned, .talosUpdate, .summary, .nodes, .dataServices, .argoCD, .flux, .timeDrift, .alerts])
        XCTAssertEqual(moved.hiddenCards, [.apps])
    }

    func testMoveOutOfRangeIsIgnored() {
        let layout = OverviewLayout()
        XCTAssertEqual(layout.move(from: -1, to: 2), layout)
        XCTAssertEqual(layout.move(from: 0, to: 10), layout)
        XCTAssertEqual(layout.move(from: 2, to: 2), layout)
    }

    func testMovingLikeSwiftUIOnMove() {
        let layout = OverviewLayout().hiding(.apps)
        // Shown: pinned, talosUpdate, summary, dataServices, argoCD, flux, nodes, timeDrift, alerts. Down: the offset is past the target.
        XCTAssertEqual(layout.moving(fromOffsets: [2], toOffset: 4).visible, [.pinned, .talosUpdate, .dataServices, .summary, .argoCD, .flux, .nodes, .timeDrift, .alerts])
        XCTAssertEqual(layout.moving(fromOffsets: [6], toOffset: 0).visible, [.nodes, .pinned, .talosUpdate, .summary, .dataServices, .argoCD, .flux, .timeDrift, .alerts])
        XCTAssertEqual(layout.moving(fromOffsets: [1], toOffset: 9).visible.last, .talosUpdate)
        XCTAssertEqual(layout.moving(fromOffsets: [1], toOffset: 9).hiddenCards, [.apps])
        XCTAssertEqual(layout.moving(fromOffsets: [10], toOffset: 0), layout)
    }

    func testAbsentSectionsAreLeftOutAndKeepTheirPlace() {
        let absent: Set<OverviewCard> = [.dataServices, .flux, .alerts]
        let layout = OverviewLayout().hiding(.argoCD).hiding(.flux)
        XCTAssertEqual(layout.visible(absent: absent), [.pinned, .talosUpdate, .summary, .apps, .nodes, .timeDrift])
        XCTAssertEqual(layout.hiddenCards(absent: absent), [.argoCD])
        // Apps (offset 3 without data services) dropped after nodes: data services stays fifth.
        let moved = layout.moving(fromOffsets: [3], toOffset: 5, absent: absent)
        XCTAssertEqual(moved.visible, [.pinned, .talosUpdate, .summary, .nodes, .dataServices, .apps, .timeDrift, .alerts])
        XCTAssertEqual(moved.hidden, [.argoCD, .flux])
    }

    func testShowPutsTheSectionLast() {
        let shown = OverviewLayout().hiding(.summary).hiding(.nodes).showing(.summary)
        XCTAssertEqual(shown.visible, [.pinned, .talosUpdate, .apps, .dataServices, .argoCD, .flux, .timeDrift, .alerts, .summary])
        XCTAssertEqual(shown.hiddenCards, [.nodes])
        XCTAssertEqual(shown.showing(.apps), shown)
    }

    func testHideAndShowBackIsNotDefaultUnlessSameOrder() {
        XCTAssertFalse(OverviewLayout().hiding(.summary).showing(.summary).isDefault)
        XCTAssertTrue(OverviewLayout().hiding(.alerts).showing(.alerts).isDefault)
    }

    func testAllHidden() {
        let layout = OverviewCard.allCases.reduce(OverviewLayout()) { $0.hiding($1) }
        XCTAssertTrue(layout.visible.isEmpty)
        XCTAssertEqual(OverviewLayout.parse(layout.encoded), layout)
    }

    func testDetectedSections() {
        XCTAssertEqual(Set(OverviewCard.allCases.filter(\.whenDetected)), [.pinned, .dataServices, .argoCD, .flux, .alerts])
    }
}
