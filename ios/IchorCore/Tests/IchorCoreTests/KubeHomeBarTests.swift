import XCTest
@testable import IchorCore

final class KubeHomeBarTests: XCTestCase {
    func testDefaultKeepsFourIconsWithGitOps() {
        let bar = KubeHomeBar()
        XCTAssertEqual(bar.icons, [.workloads, .resources, .gitOps, .metrics])
        XCTAssertEqual(bar.menu, [.alerts, .helm, .dataServices, .checkup, .apiHealth, .networkPolicies, .settings])
        XCTAssertTrue(bar.isDefault)
        XCTAssertEqual(KubeHomeBar.storageKey, "kubeHome.bar")
    }

    func testEncodeMatchesAndroid() {
        let bar = KubeHomeBar().toBar(.dataServices).toMenu(.resources)
        XCTAssertEqual(bar.encoded, "WORKLOADS,GITOPS,METRICS,DATA_SERVICES|RESOURCES,ALERTS,HELM,CHECKUP,API_HEALTH,NETWORK_POLICIES,SETTINGS")
        XCTAssertEqual(KubeHomeBar.parse(bar.encoded), bar)
        XCTAssertFalse(bar.isDefault)
    }

    func testABarSavedBeforeGitOpsKeepsItsArrangement() {
        let bar = KubeHomeBar.parse("WORKLOADS,RESOURCES,METRICS|HELM,DATA_SERVICES,CHECKUP,API_HEALTH,NETWORK_POLICIES,SETTINGS")
        XCTAssertEqual(bar.icons, [.workloads, .resources, .metrics])
        XCTAssertEqual(bar.menu, [.helm, .dataServices, .checkup, .apiHealth, .networkPolicies, .settings, .gitOps, .alerts])
        XCTAssertFalse(bar.isDefault)
    }

    func testTheOverviewsNamesAreNotTaken() {
        let bar = KubeHomeBar.parse("HEALTH,EVENTS|KUBESPAN")
        XCTAssertTrue(bar.icons.isEmpty)
        XCTAssertEqual(bar.menu, KubeHomeAction.allCases)
    }
}
