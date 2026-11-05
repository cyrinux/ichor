import XCTest

@testable import IchorCore

final class SearchBlankStateTests: XCTestCase {
    private func entry(_ id: String) -> ScreenEntry { ScreenEntry(id: id, title: id, section: "Overview") }

    /// As ScreenCatalog.swift builds it for a cluster added from a kubeconfig with a role that reads the API.
    private var kubeCatalog: [ScreenEntry] {
        [
            "workloads", "alerts", "metrics", "log-search", "argocd", "flux", "data-services", "checkup",
            "activity", "updates", "runbooks", "settings/appearance", "settings/kubernetes",
        ].map(entry)
    }

    /// As ScreenCatalog.swift builds it for a Talos os:reader with one node known.
    private var talosReaderCatalog: [ScreenEntry] {
        [
            "nodes", "health", "etcd", "events", "kubespan", "activity", "join-node", "updates", "runbooks",
            "settings/appearance", "node/10.0.0.1/kernel_log", "node/10.0.0.1/events", "node/10.0.0.1/hardware",
        ].map(entry)
    }

    func testNothingInAnyCacheAndNoRecentsSaysNothingIsKnown() {
        XCTAssertEqual(searchBlankBody(snapshotEmpty: true, recentQueries: 0, openedTargets: 0), .nothingKnown)
    }

    func testAKnownClusterWithNoRecentsGetsTheFirstUseState() {
        XCTAssertEqual(searchBlankBody(snapshotEmpty: false, recentQueries: 0, openedTargets: 0), .firstUse)
    }

    func testRecentSearchesOrOpenedObjectsKeepTheirLists() {
        XCTAssertEqual(searchBlankBody(snapshotEmpty: false, recentQueries: 1, openedTargets: 0), .recents)
        XCTAssertEqual(searchBlankBody(snapshotEmpty: false, recentQueries: 0, openedTargets: 1), .recents)
        // A recently opened object is still useful with an empty snapshot.
        XCTAssertEqual(searchBlankBody(snapshotEmpty: true, recentQueries: 0, openedTargets: 1), .recents)
    }

    func testAKubeconfigClusterIsOfferedItsMainScreensInOrder() {
        XCTAssertEqual(firstUseShortcuts(kubeCatalog).map(\.id), ["workloads", "alerts", "argocd", "flux"])
    }

    func testATalosReaderGetsNoNodeOrSettingsScreenAndAtMostFour() {
        let ids = firstUseShortcuts(talosReaderCatalog).map(\.id)
        XCTAssertLessThanOrEqual(ids.count, 4)
        XCTAssertTrue(ids.allSatisfy { !$0.hasPrefix("node/") && !$0.hasPrefix("settings/") })
        XCTAssertEqual(ids, ["nodes", "health", "etcd", "events"])
    }

    func testNoCatalogNoShortcut() {
        XCTAssertTrue(firstUseShortcuts([]).isEmpty)
    }
}
