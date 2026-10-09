import XCTest
@testable import IchorCore

final class MonitorClustersTests: XCTestCase {
    private let contexts = [
        ContextSummary(name: "home-admin", fingerprint: "fa", clusterID: "home"),
        ContextSummary(name: "home-reader", fingerprint: "fb", clusterID: "home"),
        ContextSummary(name: "lab", fingerprint: "fc", clusterID: "lab"),
        ContextSummary(name: "edge", kind: ContextKind.kube, fingerprint: "fd"),
    ]

    func testOneContextPerCluster() {
        XCTAssertEqual(clusterContexts(contexts, active: nil).map(\.name), ["home-admin", "lab", "edge"])
        // The active context stands for its cluster, in the cluster's place.
        XCTAssertEqual(clusterContexts(contexts, active: "home-reader").map(\.name), ["home-reader", "lab", "edge"])
        XCTAssertEqual(clusterContexts(contexts, active: "lab").map(\.name), ["home-admin", "lab", "edge"])
    }

    func testClusterKeys() {
        XCTAssertEqual(contexts.map(monitorClusterKey), ["home", "home", "lab", "fd"])
        XCTAssertEqual(monitorClusterKey(ContextSummary(name: "demo")), "name:demo")
    }

    func testTurnedOffClustersAreSkipped() {
        XCTAssertEqual(monitoredContexts(contexts, active: nil, unwatched: ["fc"]).map(\.name), ["home-admin", "edge"])
        // Off on any context of a cluster turns the cluster off, whichever context checks it.
        XCTAssertEqual(monitoredContexts(contexts, active: "home-admin", unwatched: ["fb"]).map(\.name), ["lab", "edge"])
        XCTAssertFalse(watchedInBackground(contexts[0], contexts: contexts, unwatched: ["fb"]))
        XCTAssertTrue(watchedInBackground(contexts[2], contexts: contexts, unwatched: ["fb"]))
    }

    func testSettingWatched() {
        XCTAssertEqual(settingWatched(false, for: contexts[0], contexts: contexts, unwatched: []), ["fa"])
        // On again: every context of the cluster is cleared.
        XCTAssertEqual(settingWatched(true, for: contexts[0], contexts: contexts, unwatched: ["fb", "fc"]), ["fc"])
        XCTAssertEqual(settingWatched(false, for: ContextSummary(name: "x"), contexts: contexts, unwatched: []), [])
    }

    private func snapshot(_ context: String) -> ClusterSnapshot {
        ClusterSnapshot(context: context, takenAt: Date(timeIntervalSince1970: 1_000),
                        nodes: ["10.0.0.2": NodeState(hostname: "cp-1", health: .ready, reason: "")])
    }

    func testLegacySnapshotGoesToTheActiveCluster() {
        let legacy = snapshot("home-admin")
        XCTAssertEqual(migratedSnapshots([:], legacy: legacy, activeCluster: "home"), ["home": legacy])
        // Already migrated (or checked since): the cluster's own stays.
        let own = snapshot("home-reader")
        XCTAssertEqual(migratedSnapshots(["home": own], legacy: legacy, activeCluster: "home"), ["home": own])
        XCTAssertEqual(migratedSnapshots(["lab": own], legacy: nil, activeCluster: "home"), ["lab": own])
        XCTAssertEqual(migratedSnapshots([:], legacy: legacy, activeCluster: nil), [:])
    }

    func testRemovedClustersDropTheirState() {
        let all = ["home": snapshot("home-admin"), "lab": snapshot("lab")]
        XCTAssertEqual(keepingClusters(all, clusters: contexts.map(monitorClusterKey)).keys.sorted(), ["home", "lab"])
        XCTAssertEqual(keepingClusters(all, clusters: ["lab"]).keys.sorted(), ["lab"])
        let snoozes = AlertSnoozes(until: ["fa": ["node:a": 5], "fz": ["node:a": 5]])
        XCTAssertEqual(snoozes.keeping(clusters: ["fa"]).until.keys.sorted(), ["fa"])
    }

    func testSharedSnapshotPerCluster() throws {
        let defaults = try XCTUnwrap(UserDefaults(suiteName: "monitor-clusters-tests"))
        defer { defaults.removePersistentDomain(forName: "monitor-clusters-tests") }
        let legacy = snapshot("home-admin")
        defaults.set(try JSONEncoder().encode(legacy), forKey: SharedSnapshot.key)
        // Before the updated app said which cluster is active.
        XCTAssertEqual(SharedSnapshot.load(from: defaults, cluster: nil), legacy)
        defaults.set("home", forKey: SharedSnapshot.activeKey)
        // Before the first run: the active cluster shows the old single snapshot.
        XCTAssertEqual(SharedSnapshot.load(from: defaults, cluster: nil), legacy)
        XCTAssertNil(SharedSnapshot.load(from: defaults, cluster: "lab"))
        let lab = snapshot("lab")
        defaults.set(try JSONEncoder().encode(["lab": lab]), forKey: SharedSnapshot.snapshotsKey)
        XCTAssertEqual(SharedSnapshot.load(from: defaults, cluster: "lab"), lab)
        defaults.set("lab", forKey: SharedSnapshot.activeKey)
        XCTAssertEqual(SharedSnapshot.load(from: defaults, cluster: nil), lab)
    }

    func testWidgetClusters() {
        let labels = ClusterLabels(names: ["fc": "Lab"])
        XCTAssertEqual(widgetClusters(contexts, active: "home-reader", labels: labels),
                       [WidgetCluster(id: "home", name: "home-reader"), WidgetCluster(id: "lab", name: "Lab"),
                        WidgetCluster(id: "fd", name: "edge")])
        let linked = widgetClusters(contexts, active: nil, labels: labels) { $0.clusterID.isEmpty ? nil : "link-\($0.clusterID)" }
        XCTAssertEqual(linked.map(\.link), ["link-home", "link-lab", nil])
    }

    func testAppShareLink() {
        XCTAssertEqual(appShareLink(web: "https://example.org/ichor/open/#v=1&c=abc&t=cluster"), "ichor://open?v=1&c=abc&t=cluster")
        XCTAssertNil(appShareLink(web: "https://example.org/ichor/open/"))
    }

    func testUnreachableAlertsAtNOnce() {
        var state: Reachability?
        var alerts: [Alert] = []
        for _ in 0..<5 {
            let result = evaluateReachability(previous: state, reachable: false, runs: 3, enabled: true)
            state = result.next
            if let alert = result.alert { alerts.append(alert) }
        }
        XCTAssertEqual(alerts.map(\.key), ["unreachable"])
        XCTAssertTrue(alerts[0].problem)
        XCTAssertEqual(state, Reachability(misses: 5, alerted: true))
        // Recovery: "reachable again" once, then the count starts over.
        let back = evaluateReachability(previous: state, reachable: true, runs: 3, enabled: true)
        XCTAssertEqual(back.alert?.problem, false)
        XCTAssertEqual(back.next, Reachability())
        XCTAssertNil(evaluateReachability(previous: back.next, reachable: true, runs: 3, enabled: true).alert)
    }

    func testUnreachableCountResetsOnAnswer() {
        var state = evaluateReachability(previous: nil, reachable: false, runs: 2, enabled: true).next
        state = evaluateReachability(previous: state, reachable: true, runs: 2, enabled: true).next
        let once = evaluateReachability(previous: state, reachable: false, runs: 2, enabled: true)
        XCTAssertNil(once.alert)
        XCTAssertNotNil(evaluateReachability(previous: once.next, reachable: false, runs: 2, enabled: true).alert)
    }

    func testUnreachableOffCountsSilently() {
        var state: Reachability?
        for _ in 0..<4 {
            let result = evaluateReachability(previous: state, reachable: false, runs: 2, enabled: false)
            XCTAssertNil(result.alert)
            state = result.next
        }
        XCTAssertEqual(state, Reachability(misses: 4, alerted: false))
        XCTAssertNil(evaluateReachability(previous: state, reachable: true, runs: 2, enabled: false).alert)
        // Turned on meanwhile: the next miss alerts.
        XCTAssertNotNil(evaluateReachability(previous: state, reachable: false, runs: 2, enabled: true).alert)
    }

    func testUnreachableRunsAreClamped() {
        let first = evaluateReachability(previous: nil, reachable: false, runs: 0, enabled: true)
        XCTAssertNil(first.alert)
        XCTAssertNotNil(evaluateReachability(previous: first.next, reachable: false, runs: 0, enabled: true).alert)
        XCTAssertNil(evaluateReachability(previous: Reachability(misses: 8), reachable: false, runs: 99, enabled: true).alert)
    }

    func testUnreachableAlertCategoryAndActions() {
        XCTAssertEqual(alertKind(key: "unreachable"), "cluster")
        XCTAssertEqual(alertKind(key: "am:f00d"), "alertmanager")
        XCTAssertEqual(alertKind(key: "node:10.0.0.2"), "node")
        XCTAssertEqual(alertActions(key: "unreachable", problem: true, canWake: true), [.snooze])
        XCTAssertTrue(alertActionSets(kind: "cluster").contains([.snooze]))
        XCTAssertEqual(alertCategory(kind: alertKind(key: "unreachable"), actions: [.snooze], hideDetails: false), "alert.cluster.snooze")
    }

    func testNotificationIDsDifferPerCluster() {
        let home = alertNotificationID(cluster: "home", alertKey: "node:10.0.0.2")
        let lab = alertNotificationID(cluster: "lab", alertKey: "node:10.0.0.2")
        XCTAssertNotEqual(home, lab)
        XCTAssertEqual(home, "home|node:10.0.0.2")
        XCTAssertEqual(alertNotificationID(cluster: "", alertKey: "cert"), "cert")
    }

    func testTimeouts() {
        XCTAssertEqual(monitorClusterTimeout(clusters: 1), 20)
        XCTAssertEqual(monitorClusterTimeout(clusters: 4), 20)
        XCTAssertEqual(monitorClusterTimeout(clusters: 8), 12.5)
        XCTAssertEqual(monitorClusterTimeout(clusters: 40), 8)
        XCTAssertEqual(monitorClusterTimeout(clusters: 0), 20)
    }

    func testDeadlineReturnsTheResultInTime() {
        let done = expectation(description: "result")
        Task {
            let value = await withDeadline(5) { () async -> Int? in 7 }
            XCTAssertEqual(value, 7)
            done.fulfill()
        }
        wait(for: [done], timeout: 2)
    }

    func testDeadlineDoesNotWaitForSlowWork() {
        let done = expectation(description: "deadline")
        Task {
            let value = await withDeadline(0.1) { () async -> Int? in
                // Ignores cancellation, like a blocking Go call.
                usleep(2_000_000)
                return 7
            }
            XCTAssertNil(value)
            done.fulfill()
        }
        wait(for: [done], timeout: 1)
    }
}
