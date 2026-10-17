import XCTest
@testable import IchorCore

final class WatchGlanceTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func snap(_ nodes: [(String, NodeHealth, String)], alarms: [String] = [], age: TimeInterval = 0) -> ClusterSnapshot {
        var byAddress: [String: NodeState] = [:]
        for (i, node) in nodes.enumerated() {
            byAddress["10.0.0.\(i + 1)"] = NodeState(hostname: node.0, health: node.1, reason: node.2)
        }
        return ClusterSnapshot(context: "admin@lab", takenAt: now.addingTimeInterval(-age), nodes: byAddress,
                               etcdAlarms: alarms, etcdChecked: true)
    }

    func testRowsPutNodesDownFirstThenByName() {
        let rows = watchRows(from: snap([("cp-2", .ready, ""), ("w-1", .notReady, "Ready: KubeletNotReady"),
                                         ("cp-1", .ready, ""), ("w-2", .unreachable, "timeout")]))
        XCTAssertEqual(rows.map(\.name), ["w-2", "w-1", "cp-1", "cp-2"])
        XCTAssertEqual(rows[1].reason, "Ready: KubeletNotReady")
    }

    func testRowsNeverCarryTheNodeAddress() throws {
        let rows = watchRows(from: snap([("", .unreachable, "dial tcp 10.0.0.1:50000: i/o timeout"),
                                         ("192.168.1.9", .ready, "")]))
        let json = try XCTUnwrap(String(data: JSONEncoder().encode(rows), encoding: .utf8))
        XCTAssertFalse(json.contains("10.0.0"))
        XCTAssertFalse(json.contains("192.168"))
        XCTAssertEqual(rows[0].reason, "dial tcp …: i/o timeout")
        XCTAssertEqual(rows[1].name, "")
    }

    func testScrubbedAddresses() {
        XCTAssertEqual(scrubbedAddresses("get https://10.0.0.1:6443/api: refused"), "get … refused")
        XCTAssertEqual(scrubbedAddresses("dial [fd00::1]:50000 failed"), "dial … failed")
        XCTAssertEqual(scrubbedAddresses("no route to fd00:10::2"), "no route to …")
        XCTAssertEqual(scrubbedAddresses("Ready: KubeletNotReady; v1.31.0"), "Ready: KubeletNotReady; v1.31.0")
        XCTAssertEqual(scrubbedAddresses("[NOSPACE] member"), "[NOSPACE] member")
    }

    func testLongReasonsAreCut() {
        let rows = watchRows(from: snap([("n", .notReady, String(repeating: "x", count: 500))]))
        XCTAssertEqual(rows[0].reason.count, watchReasonLength)
        XCTAssertTrue(rows[0].reason.hasSuffix("…"))
    }

    func testPayloadFollowsTheWidgetListAndMarksTheActiveCluster() {
        let clusters = [WidgetCluster(id: "a", name: "Home lab", link: "ichor://open?x"), WidgetCluster(id: "b", name: "Edge")]
        let payload = watchPayload(clusters: clusters, snapshots: ["b": snap([("n1", .ready, "")], alarms: ["1:NOSPACE"])],
                                   active: "b", masked: false, sentAt: now)
        XCTAssertEqual(payload.clusters.map(\.id), ["0", "1"])
        XCTAssertEqual(payload.clusters.map(\.name), ["Home lab", "Edge"])
        XCTAssertNil(payload.clusters[0].takenAt)
        XCTAssertEqual(payload.activeCluster?.name, "Edge")
        XCTAssertEqual(payload.clusters[1].etcdAlarms, 1)
    }

    func testPayloadCarriesNoKeysLinksOrContexts() throws {
        let clusters = [WidgetCluster(id: "cluster-id-123", name: "Lab", link: "ichor://open?secret")]
        let payload = watchPayload(clusters: clusters, snapshots: ["cluster-id-123": snap([("n1", .ready, "")])],
                                   active: "cluster-id-123", masked: true, sentAt: now)
        let json = try XCTUnwrap(payload.encoded().flatMap { String(data: $0, encoding: .utf8) })
        XCTAssertFalse(json.contains("cluster-id-123"))
        XCTAssertFalse(json.contains("ichor://"))
        XCTAssertFalse(json.contains("admin@lab"))
        XCTAssertTrue(payload.masked)
    }

    func testActiveClusterFallsBackToTheFirst() {
        let payload = WatchPayload(sentAt: now, masked: false,
                                   clusters: [WatchCluster(id: "0", name: "A", active: false), WatchCluster(id: "1", name: "B", active: false)])
        XCTAssertEqual(payload.activeCluster?.name, "A")
        XCTAssertNil(WatchPayload(sentAt: now, masked: false, clusters: []).activeCluster)
    }

    func testClusterReusesTheWidgetGlance() {
        let payload = watchPayload(clusters: [WidgetCluster(id: "a", name: "A")],
                                   snapshots: ["a": snap([("n1", .ready, ""), ("n2", .notReady, "")], alarms: ["1:NOSPACE"])],
                                   active: "a", masked: false, sentAt: now)
        let cluster = payload.clusters[0]
        XCTAssertEqual(cluster.glance, [.notReady(1), .etcdAlarms(1)])
        XCTAssertEqual(cluster.tone, .problem)
        XCTAssertEqual(cluster.downCount, 1)
        XCTAssertEqual(cluster.readyCount, 1)
        XCTAssertFalse(cluster.isStale(now: now))
        XCTAssertTrue(cluster.isStale(now: now.addingTimeInterval(glanceStaleAfter + 1)))
    }

    func testClusterWithoutACheck() {
        let cluster = WatchCluster(id: "0", name: "A", active: true)
        XCTAssertNil(cluster.snapshot)
        XCTAssertEqual(cluster.glance, [])
        XCTAssertNil(cluster.tone)
        XCTAssertTrue(cluster.isStale(now: now))
    }

    func testToneOfAWarningOnly() {
        let cluster = WatchCluster(id: "0", name: "A", active: true, takenAt: now,
                                   nodes: [WatchNode(name: "n", health: .notReady)], etcdChecked: true)
        XCTAssertEqual(cluster.tone, .warning)
    }

    func testRoundTripThroughDefaults() throws {
        let suite = "watch-glance-tests"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suite))
        defer { defaults.removePersistentDomain(forName: suite) }
        let payload = WatchPayload(sentAt: now, masked: false,
                                   clusters: [WatchCluster(id: "0", name: "A", active: true, takenAt: now,
                                                           nodes: [WatchNode(name: "n", health: .unreachable, reason: "r")])])
        defaults.set(payload.encoded(), forKey: WatchPayload.storeKey)
        XCTAssertEqual(WatchPayload.load(from: defaults), payload)
        XCTAssertNil(WatchPayload.decode(Data("nope".utf8)))
        XCTAssertNil(WatchPayload.load(from: nil))
    }

    private func bigPayload(clusters: Int, nodes: Int) -> WatchPayload {
        let reason = String(repeating: "condition failing ", count: 6)
        let rows = (0..<clusters).map { c in
            WatchCluster(id: String(c), name: "cluster-\(c)", active: c == 0, takenAt: now,
                         nodes: (0..<nodes).map { WatchNode(name: "node-\($0)", health: $0 % 3 == 0 ? .notReady : .ready, reason: reason) },
                         etcdChecked: true)
        }
        return WatchPayload(sentAt: now, masked: false, clusters: rows)
    }

    func testSmallPayloadIsUnchanged() {
        let payload = bigPayload(clusters: 2, nodes: 3)
        XCTAssertEqual(trimmedWatchPayload(payload), payload)
    }

    func testTrimmingCutsReasonsFirst() throws {
        let payload = bigPayload(clusters: 4, nodes: 60)
        let size = try XCTUnwrap(payload.encoded()).count
        let trimmed = trimmedWatchPayload(payload, limit: size - 1000)
        XCTAssertLessThanOrEqual(try XCTUnwrap(trimmed.encoded()).count, size - 1000)
        XCTAssertEqual(trimmed.clusters.count, 4)
        XCTAssertEqual(trimmed.clusters[0].nodes.count, 60)
        XCTAssertTrue(trimmed.clusters[0].nodes.allSatisfy { $0.reason.count <= 40 })
    }

    func testTrimmingDropsReadyNodesOfOtherClustersThenClusters() throws {
        let payload = bigPayload(clusters: 30, nodes: 100)
        let trimmed = trimmedWatchPayload(payload)
        XCTAssertLessThanOrEqual(try XCTUnwrap(trimmed.encoded()).count, watchPayloadLimit)
        XCTAssertEqual(trimmed.clusters.first?.name, "cluster-0")
        XCTAssertEqual(trimmed.clusters.first?.nodes.count, 100)
        XCTAssertTrue(trimmed.clusters.dropFirst().allSatisfy { $0.nodes.allSatisfy { $0.health != .ready } })
    }

    func testTrimmingKeepsTheActiveClusterAlone() throws {
        let payload = bigPayload(clusters: 3, nodes: 50)
        let trimmed = trimmedWatchPayload(payload, limit: 900)
        XCTAssertLessThanOrEqual(try XCTUnwrap(trimmed.encoded()).count, 900)
        XCTAssertEqual(trimmed.clusters.map(\.name), ["cluster-0"])
        XCTAssertLessThan(trimmed.clusters[0].nodes.count, 50)
    }
}
