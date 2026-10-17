import XCTest
@testable import IchorCore

final class OverviewStreamTests: XCTestCase {
    private let at = Date(timeIntervalSince1970: 1_700_000_000)

    private func up(_ addr: String) -> NodeOverview {
        NodeOverview(node: addr, hostname: "node-\(addr)", reachable: true, version: "v1.14.1", arch: "amd64", platform: "metal",
                     role: "worker", stage: "running", ready: true, cpuCount: 4, memTotal: 8_000, memAvailable: 3_000)
    }

    private func down(_ addr: String) -> NodeOverview {
        NodeOverview(node: addr, hostname: addr, reachable: false, error: "unreachable: i/o timeout", errorKind: "network",
                     role: "unknown", stage: "unknown")
    }

    func testTargetsAreTheNodesElseTheEndpointsEachOnce() {
        XCTAssertEqual(overviewTargets(nodes: ["10.0.0.2", "10.0.0.1", "10.0.0.2"], endpoints: ["lb"], omni: false), ["10.0.0.2", "10.0.0.1"])
        XCTAssertEqual(overviewTargets(nodes: [], endpoints: ["10.0.0.9", "10.0.0.9"], omni: false), ["10.0.0.9"])
        // Omni lists its own nodes: the endpoint is not one.
        XCTAssertEqual(overviewTargets(nodes: [], endpoints: ["omni.example.invalid"], omni: true), [])
    }

    func testPartsAreShownInTheContextsOrderAndMarkedPartial() {
        let assembly = OverviewAssembly(context: "lab", targets: ["10.0.0.1", "10.0.0.2", "10.0.0.3"])
            .adding(up("10.0.0.3"))
            .adding(up("10.0.0.9")) // not in the targets: after them
            .adding(up("10.0.0.1"))
        let overview = assembly.overview
        XCTAssertTrue(overview.partial)
        XCTAssertEqual(overview.context, "lab")
        XCTAssertEqual(overview.nodes.map(\.node), ["10.0.0.1", "10.0.0.3", "10.0.0.9"])
    }

    func testTheSameNodeTwiceIsReplaced() {
        let assembly = OverviewAssembly(context: "lab", targets: ["10.0.0.1"]).adding(down("10.0.0.1")).adding(up("10.0.0.1"))
        XCTAssertEqual(assembly.nodes.count, 1)
        XCTAssertTrue(assembly.nodes[0].reachable)
    }

    func testAPartialOverviewIsNeverAnOutage() {
        let partial = OverviewAssembly(context: "lab", targets: ["10.0.0.1", "10.0.0.2"]).adding(down("10.0.0.1")).overview
        XCTAssertNil(partial.outage)
        XCTAssertFalse(partial.showsLastKnown)
        XCTAssertNotNil(ClusterOverview(context: "lab", nodes: [down("10.0.0.1")]).outage)
    }

    func testPartialIsNotEncoded() throws {
        let partial = ClusterOverview(context: "lab", nodes: [up("10.0.0.1")], partial: true)
        let json = try JSONEncoder().encode(partial)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: json) as? [String: Any])
        XCTAssertNil(object["partial"])
        let decoded = try JSONDecoder().decode(ClusterOverview.self, from: json)
        XCTAssertFalse(decoded.partial)
        XCTAssertEqual(decoded.nodes, partial.nodes)
    }

    func testAnsweredCountsAgainstTheTargets() {
        let partial = ClusterOverview(context: "lab", nodes: [up("10.0.0.1")], partial: true)
        XCTAssertEqual(partial.answered(of: ["10.0.0.1", "10.0.0.2"]), NodesAnswered(answered: 1, total: 2))
        XCTAssertEqual(partial.answered(of: ["10.0.0.1", "10.0.0.2"])?.fraction, 0.5)
        // No targets (Omni): the count alone.
        XCTAssertNil(partial.answered(of: [])?.total)
        XCTAssertNil(partial.answered(of: [])?.fraction)
        // Never fewer than answered.
        XCTAssertEqual(NodesAnswered(answered: 3, total: 2).total, 3)
        XCTAssertNil(ClusterOverview(context: "lab", nodes: [up("10.0.0.1")]).answered(of: ["10.0.0.1"]))
    }

    func testAPartShowsOnlyWhileNothingElseIs() {
        let first = ClusterOverview(context: "lab", nodes: [up("10.0.0.1")], partial: true)
        let second = ClusterOverview(context: "lab", nodes: [up("10.0.0.1"), up("10.0.0.2")], partial: true)
        let loading = LoadState<ClusterOverview>.loading
        let shown = try? XCTUnwrap(loading.showingPartial(first, at: at))
        XCTAssertEqual(shown?.showsPartial, true)
        // A later part replaces an earlier one.
        if case .loaded(let overview, _, _)? = shown?.showingPartial(second, at: at) {
            XCTAssertEqual(overview.nodes.count, 2)
        } else {
            XCTFail("a later part replaces the earlier one")
        }
        // A complete overview on screen (the previous load, the last known one, the seed) stays.
        let complete = LoadState<ClusterOverview>.loaded(ClusterOverview(context: "lab", nodes: [down("10.0.0.1")]), at: at)
        XCTAssertNil(complete.showingPartial(first))
        XCTAssertNil(LoadState<ClusterOverview>.failed("boom").showingPartial(first))
    }

    func testAFailedStreamDropsItsPartsButKeepsCompleteData() {
        let partial = LoadState<ClusterOverview>.loaded(ClusterOverview(context: "lab", nodes: [up("10.0.0.1")], partial: true), at: at)
        if case .failed(let message) = partial.finished(with: .failed("timeout")) {
            XCTAssertEqual(message, "timeout")
        } else {
            XCTFail("a failed stream shows its error, not its parts")
        }
        XCTAssertNil(partial.completeOverview)

        let complete = LoadState<ClusterOverview>.loaded(ClusterOverview(context: "lab", nodes: [up("10.0.0.1")]), at: at)
        if case .loaded(_, _, let error) = complete.finished(with: .failed("timeout")) {
            XCTAssertEqual(error, "timeout")
        } else {
            XCTFail("a failed refresh keeps the data on screen")
        }
        XCTAssertEqual(complete.completeOverview?.at, at)
    }

    func testMergeLastKnownKeepsPartial() {
        let partial = ClusterOverview(context: "lab", nodes: [down("10.0.0.1")], partial: true)
        XCTAssertTrue(mergeLastKnown(current: partial, previous: ClusterOverview(context: "lab", nodes: [up("10.0.0.1")]), previousAt: at).partial)
    }
}
