import XCTest
@testable import IchorCore

final class LastKnownTests: XCTestCase {
    private let at = Date(timeIntervalSince1970: 1_700_000_000)

    private func up(_ addr: String, hostname: String = "cp-1") -> NodeOverview {
        NodeOverview(node: addr, hostname: hostname, reachable: true, version: "v1.14.1", arch: "amd64", platform: "metal",
                     role: "controlplane", stage: "running", ready: true, cpuCount: 4, memTotal: 8_000, memAvailable: 3_000)
    }

    private func down(_ addr: String, lastSeen: Int64? = nil) -> NodeOverview {
        NodeOverview(node: addr, hostname: addr, reachable: false, error: "unreachable: no route to host", errorKind: "network",
                     role: "unknown", stage: "unknown", lastSeen: lastSeen)
    }

    private func overview(_ nodes: [NodeOverview]) -> ClusterOverview { ClusterOverview(context: "lab", nodes: nodes) }

    func testNoPreviousLeavesTheOverviewAsIs() {
        let current = overview([down("10.0.0.1")])
        XCTAssertEqual(mergeLastKnown(current: current, previous: nil, previousAt: at), current)
    }

    func testAnUnreachableNodeKeepsWhatItSaidLast() {
        let merged = mergeLastKnown(current: overview([down("10.0.0.1")]), previous: overview([up("10.0.0.1")]), previousAt: at)
        let node = merged.nodes[0]
        XCTAssertEqual(node.hostname, "cp-1")
        XCTAssertEqual(node.version, "v1.14.1")
        XCTAssertEqual(node.arch, "amd64")
        XCTAssertEqual(node.platform, "metal")
        XCTAssertEqual(node.role, "controlplane")
        XCTAssertEqual(node.cpuCount, 4)
        XCTAssertEqual(node.memTotal, 8_000)
        XCTAssertEqual(node.lastSeen, 1_700_000_000_000)
        // What is true now stays.
        XCTAssertFalse(node.reachable)
        XCTAssertEqual(node.error, "unreachable: no route to host")
        XCTAssertEqual(node.errorKind, "network")
        XCTAssertEqual(node.stage, "unknown")
        XCTAssertFalse(node.ready)
        XCTAssertEqual(node.memAvailable, 0)
        XCTAssertEqual(node.health, .unreachable)
    }

    func testLastSeenIsKeptAcrossFailedRefreshes() {
        let first = mergeLastKnown(current: overview([down("a")]), previous: overview([up("a")]), previousAt: at)
        let second = mergeLastKnown(current: overview([down("a")]), previous: first, previousAt: at.addingTimeInterval(60))
        XCTAssertEqual(second.nodes[0].lastSeen, 1_700_000_000_000)
        XCTAssertEqual(second.nodes[0].hostname, "cp-1")
    }

    func testReachableAndUnknownNodesAreUntouched() {
        let current = overview([up("a", hostname: "new"), down("b"), down("c")])
        let previous = overview([up("a", hostname: "old"), down("b")])
        let merged = mergeLastKnown(current: current, previous: previous, previousAt: at)
        XCTAssertEqual(merged.nodes[0], up("a", hostname: "new"))
        XCTAssertNil(merged.nodes[0].lastSeen)
        // Never seen answering, or not there before: nothing to show.
        XCTAssertEqual(merged.nodes[1], down("b"))
        XCTAssertEqual(merged.nodes[2], down("c"))
    }

    func testLastKnownReplacesTheOutageNoticeOnlyWithSomethingToShow() {
        XCTAssertFalse(overview([down("a")]).showsLastKnown)
        XCTAssertTrue(overview([down("a", lastSeen: 1), down("b")]).showsLastKnown)
        // Not an outage while one node answers.
        XCTAssertFalse(overview([up("a"), down("b", lastSeen: 1)]).showsLastKnown)
    }

    func testOverviewRoundTripsWithLastSeen() throws {
        let merged = mergeLastKnown(current: overview([down("a"), up("b")]), previous: overview([up("a")]), previousAt: at)
        let json = String(decoding: try JSONEncoder().encode(merged), as: UTF8.self)
        XCTAssertEqual(try TalosJSON.decode(ClusterOverview.self, from: json), merged)
    }

    func testGoOverviewWithoutCapacityOrLastSeenDecodes() throws {
        let json = #"{"context":"lab","nodes":[{"node":"a","hostname":"a","reachable":true,"version":"v1","arch":"arm64","platform":"metal","role":"worker","stage":"running","ready":true,"unmetConditions":null}]}"#
        let node = try TalosJSON.decode(ClusterOverview.self, from: json).nodes[0]
        XCTAssertNil(node.lastSeen)
        XCTAssertEqual(node.cpuCount, 0)
        XCTAssertEqual(node.unmetConditions, [])
    }

    func testEntriesExpireAfterADay() {
        let entry = LastKnownEntry(key: "etcd", at: at, json: "{}")
        XCTAssertTrue(entry.isFresh(now: at))
        XCTAssertTrue(entry.isFresh(now: at.addingTimeInterval(lastKnownMaxAge - 1)))
        XCTAssertFalse(entry.isFresh(now: at.addingTimeInterval(lastKnownMaxAge)))
        // The clock moved back: its age is unknown.
        XCTAssertFalse(entry.isFresh(now: at.addingTimeInterval(-3_600)))
        XCTAssertEqual(entry.date, at)
    }

    func testDomainKeysAreDistinctPerNode() {
        let keys: [LastKnownDomain] = [.overview, .etcd, .kubespan, .inventory, .workloads, .pods,
                                       .services(node: "a"), .services(node: "b"), .resources(node: "a"),
                                       .hardware(node: "a"), .network(node: "a"), .images(node: "a")]
        XCTAssertEqual(Set(keys.map(\.key)).count, keys.count)
    }

    func testAFailedRefreshKeepsTheDataWithItsError() {
        let shown: LoadState<Int> = .loaded(1, at: at)
        guard case .loaded(let value, let when, let error) = shown.refreshed(with: .failed("timeout")) else {
            return XCTFail("data dropped")
        }
        XCTAssertEqual(value, 1)
        XCTAssertEqual(when, at)
        XCTAssertEqual(error, "timeout")
        // A success replaces it, error gone.
        guard case .loaded(2, _, nil) = shown.refreshed(with: .loaded(2, at: at)) else { return XCTFail("not replaced") }
        // Nothing shown yet: the failure is shown.
        guard case .failed("timeout") = LoadState<Int>.loading.refreshed(with: .failed("timeout")) else { return XCTFail("no error") }
    }
}
