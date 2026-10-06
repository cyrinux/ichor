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
        let layout = OverviewLayout().move(from: 6, to: 0).hiding(.apps)
        XCTAssertEqual(layout.encoded, "NODES,TALOS_UPDATE,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX,TIME_DRIFT")
        XCTAssertEqual(OverviewLayout.parse(layout.encoded), layout)
    }

    func testParseFallsBackToDefault() {
        XCTAssertEqual(OverviewLayout.parse(nil), OverviewLayout())
        XCTAssertEqual(OverviewLayout.parse(""), OverviewLayout())
        XCTAssertEqual(OverviewLayout.parse("garbage"), OverviewLayout())
    }

    func testParseSkipsUnknownAndRepeatedAndAppendsMissing() {
        let layout = OverviewLayout.parse("TIME_DRIFT, -NODES,GONE,TIME_DRIFT,-NODES")
        XCTAssertEqual(layout.order, [.talosUpdate, .timeDrift, .nodes, .summary, .apps, .dataServices, .argoCD, .flux])
        XCTAssertEqual(layout.hidden, [.nodes])
        XCTAssertEqual(layout.visible, [.talosUpdate, .timeDrift, .summary, .apps, .dataServices, .argoCD, .flux])
    }

    func testALayoutSavedBeforeTheTalosUpdateCardKeepsItOnTop() {
        // It was pinned above the sections then; other new ones still come last.
        let saved = OverviewLayout.parse("NODES,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX")
        XCTAssertEqual(saved.visible, [.talosUpdate, .nodes, .summary, .dataServices, .argoCD, .flux, .timeDrift])
        XCTAssertEqual(OverviewCard.allCases.filter(\.leadsWhenNew), [.talosUpdate])
        // Once saved with it, it stays where it was put.
        let placed = OverviewLayout.parse("SUMMARY,-TALOS_UPDATE,NODES")
        XCTAssertEqual(Array(placed.order.prefix(3)), [.summary, .talosUpdate, .nodes])
        XCTAssertEqual(placed.hidden, [.talosUpdate])
    }

    func testMoveWorksOnShownSections() {
        let moved = OverviewLayout().hiding(.apps).move(from: 5, to: 2)
        XCTAssertEqual(moved.visible, [.talosUpdate, .summary, .nodes, .dataServices, .argoCD, .flux, .timeDrift])
        XCTAssertEqual(moved.hiddenCards, [.apps])
    }

    func testMoveOutOfRangeIsIgnored() {
        let layout = OverviewLayout()
        XCTAssertEqual(layout.move(from: -1, to: 2), layout)
        XCTAssertEqual(layout.move(from: 0, to: 8), layout)
        XCTAssertEqual(layout.move(from: 2, to: 2), layout)
    }

    func testMovingLikeSwiftUIOnMove() {
        let layout = OverviewLayout().hiding(.apps)
        // Shown: talosUpdate, summary, dataServices, argoCD, flux, nodes, timeDrift. Down: the offset is past the target.
        XCTAssertEqual(layout.moving(fromOffsets: [1], toOffset: 3).visible, [.talosUpdate, .dataServices, .summary, .argoCD, .flux, .nodes, .timeDrift])
        XCTAssertEqual(layout.moving(fromOffsets: [5], toOffset: 0).visible, [.nodes, .talosUpdate, .summary, .dataServices, .argoCD, .flux, .timeDrift])
        XCTAssertEqual(layout.moving(fromOffsets: [0], toOffset: 7).visible.last, .talosUpdate)
        XCTAssertEqual(layout.moving(fromOffsets: [0], toOffset: 7).hiddenCards, [.apps])
        XCTAssertEqual(layout.moving(fromOffsets: [9], toOffset: 0), layout)
    }

    func testAbsentSectionsAreLeftOutAndKeepTheirPlace() {
        let absent: Set<OverviewCard> = [.dataServices, .flux]
        let layout = OverviewLayout().hiding(.argoCD).hiding(.flux)
        XCTAssertEqual(layout.visible(absent: absent), [.talosUpdate, .summary, .apps, .nodes, .timeDrift])
        XCTAssertEqual(layout.hiddenCards(absent: absent), [.argoCD])
        // Apps (offset 2 without data services) dropped after nodes: data services stays fourth.
        let moved = layout.moving(fromOffsets: [2], toOffset: 4, absent: absent)
        XCTAssertEqual(moved.visible, [.talosUpdate, .summary, .nodes, .dataServices, .apps, .timeDrift])
        XCTAssertEqual(moved.hidden, [.argoCD, .flux])
    }

    func testShowPutsTheSectionLast() {
        let shown = OverviewLayout().hiding(.summary).hiding(.nodes).showing(.summary)
        XCTAssertEqual(shown.visible, [.talosUpdate, .apps, .dataServices, .argoCD, .flux, .timeDrift, .summary])
        XCTAssertEqual(shown.hiddenCards, [.nodes])
        XCTAssertEqual(shown.showing(.apps), shown)
    }

    func testHideAndShowBackIsNotDefaultUnlessSameOrder() {
        XCTAssertFalse(OverviewLayout().hiding(.summary).showing(.summary).isDefault)
        XCTAssertTrue(OverviewLayout().hiding(.timeDrift).showing(.timeDrift).isDefault)
    }

    func testAllHidden() {
        let layout = OverviewCard.allCases.reduce(OverviewLayout()) { $0.hiding($1) }
        XCTAssertTrue(layout.visible.isEmpty)
        XCTAssertEqual(OverviewLayout.parse(layout.encoded), layout)
    }

    func testDetectedSections() {
        XCTAssertEqual(Set(OverviewCard.allCases.filter(\.whenDetected)), [.dataServices, .argoCD, .flux])
    }
}
