import XCTest
@testable import IchorCore

final class KubeHomeLayoutTests: XCTestCase {
    func testDefaultShowsEverySectionInOrder() {
        let layout = KubeHomeLayout()
        XCTAssertEqual(layout.visible, [.summary, .nodes, .tools, .dataServices, .argoCD, .flux, .alerts])
        XCTAssertTrue(layout.hiddenCards.isEmpty)
        XCTAssertTrue(layout.isDefault)
        XCTAssertEqual(KubeHomeLayout.storageKey, "kubeHome.layout")
        XCTAssertNotEqual(KubeHomeLayout.storageKey, OverviewLayout.storageKey)
    }

    func testEncodeRoundTripsInAndroidForm() {
        let layout = KubeHomeLayout().move(from: 2, to: 0).hiding(.nodes)
        XCTAssertEqual(layout.encoded, "TOOLS,SUMMARY,-NODES,DATA_SERVICES,ARGO_CD,FLUX,ALERTS")
        XCTAssertEqual(KubeHomeLayout.parse(layout.encoded), layout)
        XCTAssertFalse(layout.isDefault)
    }

    func testParseFallsBackToDefaultAndSkipsTheOverviewsSections() {
        XCTAssertEqual(KubeHomeLayout.parse(nil), KubeHomeLayout())
        XCTAssertEqual(KubeHomeLayout.parse(""), KubeHomeLayout())
        // A layout saved by the Talos overview: its own sections are unknown here, the shared names kept.
        let layout = KubeHomeLayout.parse("TALOS_UPDATE,-APPS,NODES,-DATA_SERVICES,TIME_DRIFT")
        XCTAssertEqual(layout.order, [.nodes, .dataServices, .summary, .tools, .argoCD, .flux, .alerts])
        XCTAssertEqual(layout.hidden, [.dataServices])
    }

    func testAbsentSectionsAreLeftOutAndKeepTheirPlace() {
        let absent: Set<KubeHomeCard> = [.argoCD, .flux, .alerts]
        let layout = KubeHomeLayout().hiding(.dataServices)
        XCTAssertEqual(layout.visible(absent: absent), [.summary, .nodes, .tools])
        XCTAssertEqual(layout.hiddenCards(absent: absent), [.dataServices])
        XCTAssertEqual(layout.moving(fromOffsets: [2], toOffset: 0, absent: absent).visible, [.tools, .summary, .nodes, .argoCD, .flux, .alerts])
    }

    func testShowingPutsTheSectionLast() {
        let shown = KubeHomeLayout().hiding(.summary).hiding(.tools).showing(.summary)
        XCTAssertEqual(shown.visible, [.nodes, .dataServices, .argoCD, .flux, .alerts, .summary])
        XCTAssertEqual(shown.hiddenCards, [.tools])
    }

    func testDetectedSectionsAndNoLeadingOne() {
        XCTAssertEqual(Set(KubeHomeCard.allCases.filter(\.whenDetected)), [.dataServices, .argoCD, .flux, .alerts])
        XCTAssertTrue(KubeHomeCard.allCases.allSatisfy { !$0.leadsWhenNew })
    }
}
