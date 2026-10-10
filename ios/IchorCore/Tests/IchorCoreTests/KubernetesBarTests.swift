import XCTest
@testable import IchorCore

final class KubernetesBarTests: XCTestCase {
    func testDefaultKeepsThreeIconsSoTheTitleFits() {
        let bar = KubernetesBar()
        XCTAssertEqual(bar.icons, [.checkup, .networkPolicies, .share])
        XCTAssertEqual(bar.menu, [.apiHealth, .flows, .resources, .helm, .events, .apiAddress])
        XCTAssertTrue(bar.isDefault)
        XCTAssertEqual(KubernetesBar.storageKey, "kubernetes.bar")
    }

    func testEncodeMatchesAndroid() {
        let bar = KubernetesBar().toBar(.flows).toMenu(.checkup)
        XCTAssertEqual(bar.encoded, "NETWORK_POLICIES,SHARE,FLOWS|CHECKUP,API_HEALTH,RESOURCES,HELM,EVENTS,API_ADDRESS")
        XCTAssertEqual(KubernetesBar.parse(bar.encoded), bar)
        XCTAssertFalse(bar.isDefault)
    }

    func testAnotherScreensNamesAreNotTaken() {
        let bar = KubernetesBar.parse("HEALTH,ETCD|METRICS")
        XCTAssertTrue(bar.icons.isEmpty)
        XCTAssertEqual(bar.menu, KubernetesAction.allCases)
    }
}
