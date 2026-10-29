import XCTest
@testable import IchorCore

final class SettingsPageTests: XCTestCase {
    func testPagesKeepTheSettingsOrder() {
        XCTAssertEqual(
            SettingsPage.allCases.map(\.rawValue),
            [
                "appearance", "security", "privacy", "alerts", "ai", "gitHosts", "plugins", "kubernetes", "clusters",
                "support", "about",
            ]
        )
    }

    func testKubernetesNeedsTheKubeconfigFeature() {
        XCTAssertEqual(SettingsPage.visible(kubeconfig: true), SettingsPage.allCases)
        let without = SettingsPage.visible(kubeconfig: false)
        XCTAssertEqual(without.count, 10)
        XCTAssertFalse(without.contains(.kubernetes))
    }

    func testEveryPageHasItsOwnIcon() {
        XCTAssertEqual(Set(SettingsPage.allCases.map(\.systemImage)).count, SettingsPage.allCases.count)
    }
}
