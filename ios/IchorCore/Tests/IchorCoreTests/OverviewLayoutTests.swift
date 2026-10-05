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
        let layout = OverviewLayout().move(from: 5, to: 0).hiding(.apps)
        XCTAssertEqual(layout.encoded, "NODES,SUMMARY,-APPS,DATA_SERVICES,ARGO_CD,FLUX,TIME_DRIFT")
        XCTAssertEqual(OverviewLayout.parse(layout.encoded), layout)
    }

    func testParseFallsBackToDefault() {
        XCTAssertEqual(OverviewLayout.parse(nil), OverviewLayout())
        XCTAssertEqual(OverviewLayout.parse(""), OverviewLayout())
        XCTAssertEqual(OverviewLayout.parse("garbage"), OverviewLayout())
    }

    func testParseSkipsUnknownAndRepeatedAndAppendsMissing() {
        let layout = OverviewLayout.parse("TIME_DRIFT, -NODES,GONE,TIME_DRIFT,-NODES")
        XCTAssertEqual(layout.order, [.timeDrift, .nodes, .summary, .apps, .dataServices, .argoCD, .flux])
        XCTAssertEqual(layout.hidden, [.nodes])
        XCTAssertEqual(layout.visible, [.timeDrift, .summary, .apps, .dataServices, .argoCD, .flux])
    }

    func testMoveWorksOnShownSections() {
        let moved = OverviewLayout().hiding(.apps).move(from: 4, to: 1)
        XCTAssertEqual(moved.visible, [.summary, .nodes, .dataServices, .argoCD, .flux, .timeDrift])
        XCTAssertEqual(moved.hiddenCards, [.apps])
    }

    func testMoveOutOfRangeIsIgnored() {
        let layout = OverviewLayout()
        XCTAssertEqual(layout.move(from: -1, to: 2), layout)
        XCTAssertEqual(layout.move(from: 0, to: 7), layout)
        XCTAssertEqual(layout.move(from: 2, to: 2), layout)
    }

    func testMovingLikeSwiftUIOnMove() {
        let layout = OverviewLayout().hiding(.apps)
        // Shown: summary, dataServices, argoCD, flux, nodes, timeDrift. Down: the offset is past the target.
        XCTAssertEqual(layout.moving(fromOffsets: [0], toOffset: 2).visible, [.dataServices, .summary, .argoCD, .flux, .nodes, .timeDrift])
        XCTAssertEqual(layout.moving(fromOffsets: [4], toOffset: 0).visible, [.nodes, .summary, .dataServices, .argoCD, .flux, .timeDrift])
        XCTAssertEqual(layout.moving(fromOffsets: [0], toOffset: 6).visible.last, .summary)
        XCTAssertEqual(layout.moving(fromOffsets: [0], toOffset: 6).hiddenCards, [.apps])
        XCTAssertEqual(layout.moving(fromOffsets: [9], toOffset: 0), layout)
    }

    func testShowPutsTheSectionLast() {
        let shown = OverviewLayout().hiding(.summary).hiding(.nodes).showing(.summary)
        XCTAssertEqual(shown.visible, [.apps, .dataServices, .argoCD, .flux, .timeDrift, .summary])
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
