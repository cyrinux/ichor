import XCTest
@testable import IchorCore

final class KubeHomeBarTests: XCTestCase {
    func testDefaultKeepsThreeIconsSoTheClusterNameFits() {
        let bar = KubeHomeBar()
        XCTAssertEqual(bar.icons, [.workloads, .resources, .metrics])
        XCTAssertEqual(bar.menu, [.helm, .dataServices, .checkup, .apiHealth, .networkPolicies, .events, .settings])
        XCTAssertTrue(bar.isDefault)
        XCTAssertEqual(KubeHomeBar.storageKey, "kubeHome.bar")
    }

    func testEncodeMatchesAndroid() {
        let bar = KubeHomeBar().toBar(.dataServices).toMenu(.resources)
        XCTAssertEqual(bar.encoded, "WORKLOADS,METRICS,DATA_SERVICES|RESOURCES,HELM,CHECKUP,API_HEALTH,NETWORK_POLICIES,EVENTS,SETTINGS")
        XCTAssertEqual(KubeHomeBar.parse(bar.encoded), bar)
        XCTAssertFalse(bar.isDefault)
    }

    func testTheSharedScreensShareTheirDestination() {
        XCTAssertEqual(KubeHomeAction.events.destination, OverviewAction.events.destination)
        XCTAssertEqual(KubeHomeAction.checkup.destination, KubernetesAction.checkup.destination)
        XCTAssertEqual(KubeHomeAction.workloads.destination, OverviewAction.workloads.destination)
        XCTAssertNotEqual(KubeHomeAction.resources.destination, OverviewAction.settings.destination)
    }

    func testTheOverviewsNamesAreNotTaken() {
        let bar = KubeHomeBar.parse("HEALTH,ETCD|KUBESPAN")
        XCTAssertTrue(bar.icons.isEmpty)
        XCTAssertEqual(bar.menu, KubeHomeAction.allCases)
    }
}
